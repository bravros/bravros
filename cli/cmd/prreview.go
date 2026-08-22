// Package cmd — prreview.go
//
// Implements `--wait` mode for `bravros pr-review`: blocks until a bot posts a
// review OR an issue comment on the specified PR (or the timeout expires). This
// gives autonomous workers (e.g. /auto-pr) a concrete blocking command to run
// after /pr instead of an inline bash poll loop.
//
// The @claude GitHub Action posts review replies as issue comments (author login
// "claude"), not as formal reviews[]. A comment is admitted as a verdict
// candidate when it is bot/Action-authored AND carries a line-anchored
// BRAVROS-VERDICT sentinel (see isVerdictCandidateComment) — B-0023: the
// Action's presentation copy ("**Claude finished ...") is not part of the wire
// contract and changes without notice; the sentinel is. fetchLatestBotReviewOrComment
// queries both sources and picks the latest entry after sinceTime.
//
// Exit codes (matches GNU timeout convention):
//
//	0   — review or comment found
//	124 — timeout elapsed without a review/comment
//	1   — error (invalid flags, gh CLI failure, etc.)
//
// See cli-pending/pr-review-wait-flag.md for the full spec.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/github"
)

// prReviewWriteStamp is the --write-stamp flag, registered in init() of pr.go.
var prReviewWriteStamp bool

// prReviewLatest and prReviewJSON back the --latest / --json flags,
// registered in init() of pr.go. --latest restores ONE minimal read verb
// after P-0187 retired every other pr-review read path: fetch the latest bot
// review/comment and report its parsed verdict. It is READ-ONLY — it never
// writes the review stamp (that stays --write-stamp's exclusive job) and
// never touches the review-stamp token.
var (
	prReviewLatest bool
	prReviewJSON   bool
)

// Verdict TIERS. The tier is the SOURCE of the verdict, and it is the whole
// safety story of this file — it says whether the bot ASSERTED a verdict or
// whether we GUESSED one from its prose.
//
//	verdictTierMarker — TIER 1. The bot emitted the canonical verdict marker our
//	  review-request skills instruct it to emit — the plain-text sentinel line
//	  "BRAVROS-VERDICT: …" (canonical since B-0342) or the legacy HTML comment
//	  "<!-- bravros-verdict: … -->". This is an explicit, machine-readable,
//	  structured assertion. It is SOUND, and it alone can self-authorize an
//	  autonomous merge.
//
//	verdictTierProse  — TIER 2. No marker; the verdict was INFERRED from free-form
//	  English by the heuristics below. Six adversarial panels have now shown this
//	  inference is UNSOUND IN THE APPROVAL DIRECTION and cannot be made sound: each
//	  round closed a class of false-approve and the next round found another. The
//	  final, structural proof is the CONDITIONAL sign-off —
//
//	      "LGTM assuming you fix the double-billing."
//	      "Once that is corrected this will be ready to merge."
//
//	  — where the condition attaches to the RIGHT of the phrase. Every guard here
//	  scans LEFT for a negator, so it cannot, even in principle, see it. Widening the
//	  scan rightward just moves the goalposts: the next panel writes a longer clause.
//
// THE RESOLUTION (operator-decided, and it is not a better classifier): a TIER-2
// APPROVAL IS NO LONGER AN AUTHORIZATION. parseVerdict still REPORTS what it thinks
// the bot said (Verdict stays "approved", so `--field verdict` / `--terse` / logs
// still show the human the bot's apparent position), but it returns Confident:FALSE,
// and writeStampFromVerdict requires approved+Confident to write the stamp that
// unlocks an autonomous merge. So the classifier may still be WRONG about the prose
// and it no longer MATTERS — a wrong guess now costs a human one deliberate stamp
// (see the review-stamp token, reviewstamp.go) instead of merging a rejected PR.
//
// TIER-2 REJECTIONS KEEP Confident:TRUE. The asymmetry is the point, and it is the
// same asymmetry the rejection scan has always had:
//
//	false VETO    → no stamp → a human reviews and stamps by hand. Annoying. SAFE.
//	false APPROVE → stamp written → audit Rules 28/31 accept it → an autonomous
//	                pipeline MERGES A REJECTED PR. Catastrophic.
//
// Demoting rejections too would weaken the fail-CLOSED direction for no gain. Only
// the approval direction — the one that can authorize an irreversible action — is
// demoted.
const (
	verdictTierMarker = "marker" // TIER 1 — the bot's own BRAVROS-VERDICT sentinel / <!-- bravros-verdict: … --> marker
	verdictTierProse  = "prose"  // TIER 2 — inferred from free-form prose (advisory only, for approvals)
)

// parseVerdictResult holds the output of parseVerdict.
type parseVerdictResult struct {
	// Verdict is "approved", "changes-requested", or "unclear". It reports what the
	// review APPEARS to say, at BOTH tiers — it is not, on its own, an authorization.
	Verdict string
	// Tier is the SOURCE of the verdict: verdictTierMarker (tier 1) or
	// verdictTierProse (tier 2). Empty when there is no verdict ("unclear").
	//
	// It exists so the stamp-writer's decision is READABLE rather than implied by a
	// bool, and so logs/callers can tell an asserted verdict from a guessed one.
	Tier string
	// Confident is the AUTHORIZATION bit, not a confidence score. It is true for
	// tier-1 marker verdicts (both directions) and for tier-2 REJECTIONS (which stay
	// decisive) — and FALSE for every tier-2 APPROVAL, so a prose guess can never
	// self-authorize a merge. See the tier constants above.
	Confident bool
	// MatchedPhrase is the phrase or marker that triggered the verdict (for logging).
	MatchedPhrase string
}

// Phrase tables for parseVerdict. Update these when the @claude verdict template
// drifts — this is the single place to add/remove approval or rejection phrases.
//
// These are PLAIN-PROSE phrases: a match anywhere in the body (word-bounded, not
// negated) approves. Only add a phrase here when its mere presence in prose is an
// unambiguous sign-off. A bare adjective like "mergeable" is NOT — it reads the
// same in "the branch is mergeable, but the logic is wrong" and in GitHub's own
// conflict-state vocabulary ("mergeable: false"). Bare tokens get a dedicated
// high-precision matcher instead: see lgtmApproves (for "lgtm") and
// verdictTokenApproves (for "mergeable" / "approved to merge" / "merge when ready").
//
// EVICTED (each was a PROVEN false-approve — the phrase read as an unambiguous
// sign-off only to the table's author, never to the reviewer writing the prose):
//
//   - "no new issues" — a SCOPE statement about the delta, not a verdict. PROVEN:
//     "No new issues in the diff itself. … The charge() helper double-bills on
//     retry … I'd rather not land this yet. Please revisit." → approved. There is
//     no structural rescue for it either (a bolded "**No new issues.**" still says
//     nothing about merging), so it is deleted outright, not relocated.
//   - "merge when ready" / "merge when you are ready" — a CONDITIONAL handoff, and
//     the condition routinely IS the fix. PROVEN: "The nil deref on line 42 will
//     crash production. Please fix it, then merge when ready." → approved. Unlike
//     "no new issues" this one HAS a genuine sign-off shape ("**Merge when ready.**"
//     alone on its line — the PR #322 fixture), so it is relocated to verdictTokens
//     where the line-start structural anchor separates the sign-off from the prose.
//   - "merge-ready" / "clear to ship" / "good to merge" — the same CONDITIONAL/HEDGED
//     shape, and all three PROVEN false-approvers as bare prose:
//     "Restore the auth check and this becomes merge-ready." → approved;
//     "This is far from clear to ship." → approved;
//     "I can't say this is good to merge." → approved.
//     Each has a genuine sign-off shape ("**Merge-ready.**", "**Good to merge.**"),
//     so — like "merge when ready" — they are RELOCATED to verdictTokens rather than
//     deleted. A conditional mention in prose can no longer approve; a standalone,
//     line-start-anchored sign-off still can.
//   - "merge readiness: ✅ ready" / "status: ✅" — structured status lines, not prose,
//     and DELETED OUTRIGHT (they used to live in emojiStatusMarkers). A build status is
//     not a merge verdict: a per-CHECK CI line legitimately begins its own line, so even
//     the line-start anchor could not rescue them —
//     "Status: ✅ CI green (42/42 tests pass)\n\nThe migration drops the users table —
//     I'd rather not land this yet." → approved. The tier-1 bravros-verdict marker
//     supersedes them, and a structured template that means to sign off can emit a real
//     verdict token.
//
// KEPT, and why each still clears the bar ("mere presence in prose is an unambiguous
// sign-off"):
//
//   - "ready to merge" / "safe to merge" / "clear to merge" — a completed judgement about
//     THIS branch, with no forward condition baked into the words. The hedged shapes
//     ("not ready to merge", "I would not call this safe to merge") are exactly what the
//     word-window negation guard below catches, and the negator can no longer be pushed
//     out of reach by a soft wrap.
//   - "approved for merge" / "verdict: approved" — explicit sign-off vocabulary; the word
//     "approved" applied to THIS pr is not a hedge.
//   - "lgtm" — the classic sign-off, but the broadest token here, so it keeps its own
//     dedicated matcher (lgtmApproves) with a forward-condition guard on top of the shared
//     negation window.
//   - "no-new-blockers" — a structured verdict TOKEN, not prose: it is one of the two
//     values audit Rules 28/31 accept in a review stamp
//     (reviewer_verdict ∈ {approved, no-new-blockers}), so a bot echoing it is making an
//     explicit machine-readable claim, not a passing remark. Pinned by the
//     "no-new-blockers token" case in TestParseVerdict_TableDriven.
var positivePhrases = []string{
	"ready to merge", "safe to merge", "approved for merge",
	"clear to merge", "verdict: approved", "lgtm",
	"no-new-blockers",
}

// negativePhrases is the "changes requested" family — the ONLY rejection signals
// scanned against the CODE-STRIPPED body rather than the raw one. Illustrative
// test-case names in this repo are literally these strings inside backticks (the
// PR #322 false-negative), so a raw scan for them would veto a genuine approval that
// merely quotes a test name. Every other rejection signal — the blocking-severity
// family — is scanned RAW; see blockingRejectionSignals.
//
// The old "not ready" / "not safe to merge" / "do not merge" entries moved to
// blockingRejectionSignals (raw, un-hideable). They are not duplicated here.
var negativePhrases = []string{
	"changes requested", "requesting changes", "request changes",
}

// -----------------------------------------------------------------------------
// THE FLATTENED VIEW — one mechanism, applied uniformly (whitespace invariance).
// -----------------------------------------------------------------------------
//
// Every guard on the approval side reasons about what sits NEXT TO a phrase: is the
// phrase negated? is it carrying a forward condition? Two assumptions used to be baked
// into those guards, and BOTH are false of real review prose:
//
//	(a) the negator is CONTIGUOUS with the phrase — negated() matched exactly four
//	    prefixes ("not <p>", "n't <p>", "not yet <p>", "isn't <p>");
//	(b) the surrounding context can be read as a RAW BYTE WINDOW of the body.
//
// Reviewers hard-wrap at ~80 columns and hedge in ordinary English, so a SINGLE
// WHITESPACE BYTE flipped the verdict. Proven, with differential runs whose only
// difference is one byte:
//
//	"LGTM once the nil deref is handled."      → unclear    (guard fires)
//	"LGTM\nonce the nil deref is handled."     → APPROVED   (\n defeats the guard)
//	"…this is not ready to merge."             → rejected   (guard fires)
//	"…this is not\nready to merge."            → APPROVED   (\n breaks contiguity)
//	"I would not call this safe to merge."     → APPROVED   (one interposed word)
//	"This is far from clear to ship."          → APPROVED   (no negator shape at all)
//
// flatten builds the view all contiguity-based matching now runs against: EVERY run of
// whitespace — spaces, tabs, CR, newlines — collapses to a SINGLE space. A line break
// therefore carries no meaning and cannot change a verdict, ever.
//
// IT IS NOT THE ONLY VIEW. The STRUCTURAL matchers (the line-start anchor in
// lineVerdictCandidates, the standalone-line token, the table-pipe rejection,
// emphasisSpans, stripIndentedCode) are inherently LINE-ORIENTED and MUST keep reading
// the LINE-STRUCTURED text — flattening out from under them would splice every line's
// prose onto the next and forge the anchor. parseVerdict keeps both views and names them
// explicitly (`lined` vs `flat`); do not cross the wires.
func flatten(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// verdictNegatorWindowWords is how much left context is scanned for a negator before a
// phrase or token may approve, measured in WORDS of the FLATTENED text.
//
// WORDS, not bytes, on purpose. The previous window was 120 RAW bytes, and a longer
// clause simply walked the negator out of it:
//
//	"I do not consider this branch to be\nmergeable."                        → unclear
//	"I do not consider the current implementation of the payment retry path,
//	 which double-bills the customer on every transient failure, to be\nmergeable."
//	                                                                         → APPROVED
//
// Widening the byte count is more whack-a-mole: the next clause is longer still. A word
// count is invariant to clause length and punctuation, which is exactly the axis the byte
// window was defeated on.
//
// The window is a BACKSTOP, not the primary bound. The primary bound is the SENTENCE:
// negatorWindow keeps only the text after the last sentence terminator, because a negation
// inside a CLOSED sentence is separate context ("I did not find any regressions.\n\n
// **Mergeable.**" must still approve). 25 words comfortably spans a soft-wrapped clause
// while a run-on longer than that is bounded rather than unbounded.
const verdictNegatorWindowWords = 25

// negatorWindow returns the left-context window scanned for negation: the tail of `before`
// after the last sentence terminator, capped at verdictNegatorWindowWords words.
//
// `before` MUST already be flattened — the sentence clip and the word split both assume a
// single-space-normalized string.
func negatorWindow(before string) string {
	if k := strings.LastIndexAny(before, ".!?:;"); k >= 0 {
		before = before[k+1:]
	}
	w := strings.Fields(before)
	if len(w) > verdictNegatorWindowWords {
		w = w[len(w)-verdictNegatorWindowWords:]
	}
	return strings.Join(w, " ")
}

// verdictNegators is the negation vocabulary scanned in the left window. It replaces
// negated()'s four contiguous prefixes: a negator ANYWHERE in the window disqualifies the
// phrase, so an interposed word ("would not CALL THIS safe to merge"), a soft wrap
// ("not\nready to merge"), or a non-"not" negation ("far from clear to ship", "I can't say
// this is good to merge") are all caught by the same mechanism.
//
// "n't" is handled separately (as a substring) because it can only ever occur inside a
// contraction — can't, won't, isn't, don't, ain't, wouldn't.
var verdictNegators = []string{
	"not", "no", "never", "no longer", "cannot", "can't", "won't",
	"hardly", "barely", "far from", "unable", "rather not",
	"refuse", "refuses", "refusing", "withhold", "withholding",
	"would not", "doubt",
}

// hasNegator reports whether the (already flattened, sentence-clipped) window carries a
// negation cue. Word-bounded, so "note" / "another" / "known" do not trip "not" / "no".
func hasNegator(window string) bool {
	lower := strings.ToLower(window)
	if strings.Contains(lower, "n't") {
		return true
	}
	for _, neg := range verdictNegators {
		if containsWordBounded(lower, neg) {
			return true
		}
	}
	return false
}

// negatorBefore reports whether the flattened text preceding byte offset `at` negates the
// phrase that starts there. This is THE shared guard: positive phrases (phraseApproves),
// "lgtm" (lgtmHedged), and the structural verdict tokens (verdictNegated) all route
// through it, so there is exactly one negation mechanism to reason about.
func negatorBefore(flat string, at int) bool {
	return hasNegator(negatorWindow(flat[:at]))
}

// phraseApproves reports whether phrase occurs in the FLATTENED body as an un-negated
// approval.
//
// Each occurrence must be word-bounded on the left (a non-letter, or start of string,
// immediately before it) so "unclear to merge" cannot satisfy "clear to merge", and must
// carry no negator in its left window. A negated occurrence does not veto the phrase
// outright — scanning continues, so "This was not ready to merge. Now it is ready to
// merge." still approves on the second, un-negated occurrence.
func phraseApproves(flat, phrase string) bool {
	for i := 0; ; {
		j := strings.Index(flat[i:], phrase)
		if j < 0 {
			return false
		}
		k := i + j
		if (k == 0 || !isASCIILetter(flat[k-1])) && !negatorBefore(flat, k) {
			return true
		}
		i = k + 1
	}
}

// lgtmApproves reports whether a standalone "lgtm" token in the FLATTENED body reads as a
// genuine approval rather than a hedged or conditional mention. "lgtm" is the broadest
// positive phrase and gates an autonomous merge (Rules 28/31), so beyond the shared
// negation window it also carries a FORWARD-CONDITION guard: the token must be
// word-bounded (not a substring like "lgtmworthy"), must not sit in a hedging context
// ("not sure this is LGTM", "might LGTM"), and must not be immediately qualified by a
// condition ("LGTM after fixes", "LGTM once CI passes", "is LGTM yet").
//
// A trailing nit like "LGTM overall, but fix the typo" still approves — that is the
// reviewer accepting the change with a follow-up note. (PR #314 review.)
func lgtmApproves(flat string) bool {
	const tok = "lgtm"
	for from := 0; ; {
		i := strings.Index(flat[from:], tok)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(tok)
		bounded := (start == 0 || !isASCIILetter(flat[start-1])) &&
			(end == len(flat) || !isASCIILetter(flat[end]))
		if bounded && !lgtmHedged(flat, start, end) {
			return true
		}
		from = end
	}
}

// lgtmAfterWindowWords is how much of the FLATTENED text following the "lgtm" token is
// scanned for a forward condition, in WORDS.
//
// The old after-window was 16 raw bytes fed through `TrimLeft(…, " ,.!:;")` — a cutset
// that omits "\n" and "\t". So "LGTM\nonce the nil deref is handled." never
// HasPrefix-matched "once" and APPROVED, while the identical sentence with a space did
// not. The window is now word-based over flattened text, so no whitespace byte can defeat
// it.
const lgtmAfterWindowWords = 4

// lgtmHedges are non-negating hedges: they express uncertainty rather than negation, so
// they are not in verdictNegators but still disqualify an "lgtm".
var lgtmHedges = []string{"not sure", "might", "maybe", "almost", "nearly", "not quite", "could be", "would be"}

// lgtmForwardConditions are the qualifiers that turn "LGTM" into "LGTM, *once you…*" — a
// promise of a future approval, not an approval.
var lgtmForwardConditions = []string{"after", "once", "when", "if", "pending", "unless", "with fixes", "with the fix"}

// lgtmHedged reports whether the standalone "lgtm" token at [start,end) of the FLATTENED
// body sits in a hedging, negating, or conditional context that disqualifies it.
func lgtmHedged(flat string, start, end int) bool {
	before := negatorWindow(flat[:start])
	if hasNegator(before) {
		return true
	}
	for _, h := range lgtmHedges {
		if strings.Contains(before, h) {
			return true
		}
	}
	after := afterWords(flat[end:], lgtmAfterWindowWords)
	for _, c := range lgtmForwardConditions {
		if after == c || strings.HasPrefix(after, c+" ") {
			return true
		}
	}
	// "yet" shortly after is a not-done signal ("is LGTM yet").
	return containsWordBounded(after, "yet")
}

// afterWords returns the first n words of s (already FLATTENED), with leading punctuation
// dropped so ", safe to merge" and " — once CI passes" both present their first real word
// at offset 0.
func afterWords(s string, n int) string {
	s = strings.TrimLeft(s, " ,.!:;—-")
	w := strings.Fields(s)
	if len(w) > n {
		w = w[:n]
	}
	return strings.ToLower(strings.Join(w, " "))
}

// isASCIILetter reports whether b is an a-z byte. lower is already ToLower'd, so
// uppercase and non-ASCII bytes count as token boundaries — which is correct for
// the word-boundary check around "lgtm".
func isASCIILetter(b byte) bool {
	return b >= 'a' && b <= 'z'
}

// -----------------------------------------------------------------------------
// Verdict tokens (PR #1343) — bare tokens matched ONLY as a standalone verdict.
// -----------------------------------------------------------------------------
//
// @claude signs off with a bolded verdict token under a "### Verdict" heading:
//
//	### Verdict
//
//	**Mergeable.** The fix is correct, ... and all targeted tests + Pint pass.
//
// parseVerdict scored that "unclear", so --write-stamp wrote no stamp and /finish
// re-triggered a redundant review pass. The naive fix — dropping "mergeable" into
// positivePhrases — is UNSAFE: "mergeable" is a bare adjective AND GitHub's own
// conflict-state term, so a plain-prose match flips this gate (which authorizes
// autonomous merges via audit Rules 28/31) from fail-closed to fail-open on bodies
// that are clearly rejections:
//
//	"Mergeable: ❌ No — CI is red."                        → would approve
//	"The branch is mergeable, but the logic is wrong."     → would approve
//	"GitHub reports mergeable: false — it conflicts."      → would approve
//	"Non-mergeable until conflicts are resolved."          → would approve (the hyphen
//	                                                         defeats phraseWordBounded,
//	                                                         which only rejects a LETTER prefix)
//
// A vocabulary guard alone cannot rescue a bare token — the old negated() matched only
// four CONTIGUOUS prefixes ("not <p>", "n't <p>", "not yet <p>", "isn't <p>"), so a single
// interposed word ("will not BE mergeable") defeated it. (The negation guard is now a
// word-window scan over flattened text, which does catch that shape — but it is
// defense-in-depth here, not the primary discriminator.)
//
// The discriminator is STRUCTURE, not vocabulary: the bot's sign-off is a bolded /
// standalone verdict token; every false-approve above is an ordinary prose mention.
// So verdictTokenApproves accepts a token only when it stands alone — as the entire
// content of a bold-emphasis span (**Mergeable.**, __Mergeable.__) or as the entire
// content of its own line ("Mergeable."). Because the content must EQUAL the token,
// "**Non-mergeable.**" (content "non-mergeable") and "**Not mergeable.**" (content
// "not mergeable") are rejected structurally — no negation table required.
//
// This runs in parseVerdict AFTER the decisive negatives, so a body carrying
// "do not merge" can never be rescued by a bolded "**Mergeable.**".
//
// "merge when ready" / "merge when you are ready" / "merge-ready" / "good to merge" /
// "clear to ship" live here for the SAME reason, having all been evicted from
// positivePhrases (see there). As prose each is a conditional or hedged mention whose
// condition is routinely the fix itself — "Please fix it, then merge when ready.",
// "Restore the auth check and this becomes merge-ready.", "I can't say this is good to
// merge.", "This is far from clear to ship." all falsely approved. As a line-start-anchored
// standalone/bolded token ("**Merge when ready.**" — the PR #322 sign-off shape —
// "**Merge-ready.**", "**Good to merge.**") they are a verdict. Structure, again, is the
// discriminator.
var verdictTokens = []string{
	"mergeable", "approved to merge",
	"merge when ready", "merge when you are ready",
	"merge-ready", "good to merge", "clear to ship",
}

// -----------------------------------------------------------------------------
// The rejection side: BROAD, RAW, NIT-AWARE (the FINAL policy).
// -----------------------------------------------------------------------------
//
// Operator decision: "the bot's own verdict wins; block only on BLOCKING findings."
// A finding the bot itself marks non-blocking does NOT veto; a BLOCKING-severity
// finding vetoes decisively, regardless of any approval token and regardless of
// document order.
//
// Approval and rejection are NOT symmetric, because their failure modes are not:
//
//	false VETO    → no stamp → a human reviews and stamps by hand. Annoying. SAFE.
//	false APPROVE → writes .planning/.review-stamp-<PR>.json → audit Rules 28/31
//	                accept it → an autonomous pipeline MERGES A REJECTED PR.
//	                Catastrophic.
//
// So: be STRICT about what counts as approval, PARANOID about what counts as
// rejection. Three adversarial panels each broke a STRUCTURAL rejection matcher
// (a bolded / standalone "**Blocking.**" token) — one word outside its token list
// ("**Rejected.**", "**Needs work.**", "**Do not land this.**") or one nested
// emphasis wrapper ("**_Blocking._**") and the veto silently failed to fire, while
// the fail-OPEN approval token kept working. Structure is the wrong discriminator
// on this side. A substring scan over a rich vocabulary is the right one.
//
// blockingRejectionSignals is that vocabulary. Any NON-EXEMPT occurrence anywhere
// in the body is a decisive changes-requested. Deliberately over-broad: a false
// veto costs one manual stamp, a missed veto merges a rejected PR.
var blockingRejectionSignals = []string{
	"blocking", "blocker", "blocked",
	"do not merge", "don't merge", "do not land", "don't land",
	"not safe to merge", "not mergeable", "non-mergeable",
	"needs work", "needs rework", "needs changes",
	"rejected", "rejecting", "not approved",
	"must fix", "must be fixed",
	"not ready",
	"🔴", "❌", "🚫", "⛔",
}

// rejectionExemptions are the explicitly-NON-BLOCKING constructions. Without them
// the scan above would veto the very bodies it exists to let through — the real
// @claude sign-off routinely says "I found no blocking issues" or "Minor nit
// (non-blocking): …" INSIDE an approval. An occurrence of a rejection signal is
// EXEMPT when an exemption phrase literally CONTAINS it (see rejectionExempt): the
// exemption must span the whole hit, not merely sit nearby, so "please address the
// blockers" (no exemption covers "blocker") still vetoes.
// The "not a blocker" family is here on the strength of REAL PRODUCTION DATA: the
// FIRST @claude review on paylog/ev PR #1343 is an approval that signs off with
// "**Mergeable.** … The description-string inconsistency above is a polish
// suggestion, not a blocker." An exemption list without it vetoes a genuine
// approval. Both @claude bodies on that PR are pinned as must-approve fixtures.
var rejectionExemptions = []string{
	"non-blocking", "non blocking", "no blocking", "not blocking",
	"nothing blocking", "zero blocking", "0 blocking", "never blocking",
	"not a blocking", "isn't blocking", "aren't blocking",
	"non-blocker", "no-blocker", "no blocker", "no blockers",
	"no-new-blockers", "no new blockers", "zero blockers",
	"not a blocker", "isn't a blocker", "is not a blocker",
	"no longer a blocker", "not blockers",
	"unblocked", "not blocked", "no blocked", "nothing blocked",
	"no longer blocked", "never blocked",
	"was not ready", "been not ready",
	"not rejected", "nothing rejected",
	// Resolution / past-tense family (P-0183 G1). Strictly past-tense or
	// resolution-scoped — no present-tense forms. These fix the DISPLAYED verdict for
	// review bodies like "### Previously blocking - cursor pagination tiebreak: fixed"
	// (PR #294), which misled human readers. They carry no stamp authority either way:
	// since P-0183 G1 the prose verdict is report-only and never feeds
	// stampAuthorityFor.
	//
	// Why this cannot create a false approve: an exemption suppresses ONLY the one
	// occurrence it spans — blockingRejection keeps iterating (from = at + 1) across
	// every other signal, so a body saying "Previously blocking — pagination: fixed"
	// AND "New blocking issue: nil deref" still hits the non-exempt "blocking" and
	// still reports changes-requested.
	"previously blocking", "previously-blocking", "previously blocked",
	"previously a blocker", "previous blocker", "previous blockers",
	"prior blocking", "prior blocker", "prior blockers",
	"earlier blocking", "formerly blocking", "no longer blocking",
	"was blocking", "were blocking", "was a blocker",
	"had been blocking", "used to be blocking",
}

// blockingRejection scans body for a non-exempt rejection signal and returns it.
//
// It runs over the RAW body — BEFORE stripCodeMarkup / HTML-comment / indented-code
// stripping — on purpose. A stray unbalanced backtick makes the code stripper swallow
// the rest of the line (or the rest of the body, for an unterminated fence), which can
// destroy the fail-CLOSED signal while leaving the fail-OPEN approval token intact:
//
//	**Mergeable.** Overall solid.
//
//	But the helper ` returns nil here.
//
//	**Blocking.** The nil deref crashes prod at `req.User.ID`.
//
// Here the two stray backticks pair up around "**Blocking.** The nil deref crashes
// prod at " and the veto vanishes. Rejection must be un-hideable, so it never reads a
// filtered view of the document.
//
// ACCEPTED COST: a fenced code EXAMPLE that merely contains the word "blocking" now
// vetoes. That is a false VETO — the safe direction — costing one manual stamp.
//
// The "changes requested" / "request changes" / "requesting changes" family is
// deliberately NOT in this list: it stays on the code-STRIPPED scan (negativePhrases)
// because illustrative test-case names in this very repo are literally those strings
// inside backticks (PR #322 false-negative). A genuine prose "changes requested" still
// vetoes there.
func blockingRejection(body string) (string, bool) {
	lower := strings.ToLower(body)
	for _, sig := range blockingRejectionSignals {
		for from := 0; ; {
			i := strings.Index(lower[from:], sig)
			if i < 0 {
				break
			}
			at := from + i
			if !rejectionExempt(lower, at, len(sig)) {
				return sig, true
			}
			from = at + 1
		}
	}
	return "", false
}

// rejectionExempt reports whether the signal occurrence at [at, at+n) in lower is
// wholly contained inside an exemption phrase — i.e. the phrase starts at or before
// the hit and ends at or after it. Containment (not proximity) is the test, so
// "no blockers found" exempts its "blocker" while "address the blockers" does not.
func rejectionExempt(lower string, at, n int) bool {
	for _, ex := range rejectionExemptions {
		for from := 0; ; {
			i := strings.Index(lower[from:], ex)
			if i < 0 {
				break
			}
			j := from + i
			if j <= at && j+len(ex) >= at+n {
				return true
			}
			from = j + 1
		}
	}
	return false
}

// verdictTokenApproves reports whether body carries a standalone verdict token
// ("Mergeable" / "Approved to merge"), returning the token that matched so the
// stamp log stays informative. It must be called on the stripCodeMarkup'd body, so
// a backtick-quoted "`Mergeable.`" is already gone and cannot approve.
//
// The token must be LINE-START ANCHORED (see lineVerdictCandidates): a bold span at an
// arbitrary column is NOT a verdict. Without the anchor, "standalone" was a lie — a
// blockquote marker ("> **Mergeable.** — that was my earlier take"), a table pipe
// ("| **Mergeable** | ❌ No |"), a list marker ("3. **Mergeable** — no."), or a plain
// attribution ("The author claims: **Mergeable.**") all preceded the span and still
// approved.
func verdictTokenApproves(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		for _, cand := range lineVerdictCandidates(line) {
			tok, ok := matchToken(cand, verdictTokens)
			if !ok {
				continue
			}
			if verdictNegated(lines[:i]) {
				continue
			}
			return tok, true
		}
	}
	return "", false
}

// lineVerdictCandidates returns the strings a line can offer as a verdict token —
// but ONLY when the line is structurally capable of carrying a sign-off.
//
// A line qualifies when, after leading whitespace ONLY, its content EITHER is the
// token outright (emphasis normalized: "Mergeable.", "**Mergeable.**") OR OPENS with
// a bold span ("**Mergeable.** The fix is correct, …" — the real @claude shape).
// Prose trailing the span on the same line stays allowed; the span may not be
// preceded by anything.
//
// Anything else before the span disqualifies the line: a blockquote (">"), a list
// marker ("-", "*", "+", "3."), or ordinary prose ("The author claims: "). A line
// containing a "|" is rejected outright — a table row is a status matrix, never a
// sign-off.
//
// NO HEADING ALLOWANCE. An earlier revision let an optional "#"-run precede the
// token ("## **Mergeable.**"). That re-opened the LABEL hole this matcher exists to
// close: "## Mergeable" as a SECTION TITLE with "No." on the next line approved. The
// real bot body puts "### Verdict" on its own line and "**Mergeable.**" on the NEXT
// line, so heading-stripping was never needed for the happy path — deleting it costs
// nothing and a heading is a label, never a verdict.
func lineVerdictCandidates(line string) []string {
	if strings.Contains(line, "|") {
		return nil // table row — a status matrix, not a verdict
	}
	s := strings.TrimLeft(line, " \t")
	if s == "" || s[0] == '#' {
		return nil // heading = section LABEL, not a sign-off
	}
	var out []string
	// (b) the whole line is the token, emphasis normalized.
	out = append(out, s)
	// (a) the line OPENS with a bold span; prose may trail it on the same line.
	if spans := emphasisSpans(s); len(spans) > 0 && spans[0].start == 0 {
		out = append(out, spans[0].content)
	}
	return out
}

// matchToken normalizes a candidate (a bold span's content, or a whole line) and
// reports which token in tokens it equals.
//
// Trailing "." and "!" are sentence punctuation and get trimmed. A trailing ":" is
// deliberately NOT trimmed: a colon means "a value follows", which is a LABEL, not a
// verdict — trimming it would approve the extremely common status-table shapes
// "**Mergeable:** ❌ No" and "Mergeable:\n❌ No". Rejecting colon-terminated
// candidates costs nothing (the bot's real sign-off is "**Mergeable.**") and closes
// that hole by construction.
func matchToken(candidate string, tokens []string) (string, bool) {
	norm, ok := normalizeEmphasis(candidate)
	if !ok {
		return "", false
	}
	norm = strings.TrimRight(norm, ".!")
	norm = strings.ToLower(strings.TrimSpace(norm))
	for _, t := range tokens {
		if norm == t {
			return t, true
		}
	}
	return "", false
}

// normalizeEmphasis strips EVERY markdown emphasis character ("*" and "_") from a
// candidate, so nested and mixed wrappers all collapse onto the bare token:
// "**Mergeable.**", "__Mergeable.__", "***Mergeable.***", "**_Mergeable._**" →
// "Mergeable.". A balanced-run stripper missed the mixed forms, and asymmetric
// normalization is exactly how "**_Blocking._**" once slipped past the veto.
//
// It returns ok=false — the candidate is not a token at all — when the leading run
// of emphasis characters is followed by whitespace. That is a LIST BULLET
// ("* Mergeable" in a checklist), not emphasis: per CommonMark an emphasis opener is
// never followed by whitespace. Without this guard a blind strip-all would turn the
// bullet into the bare token and approve a checklist item.
func normalizeEmphasis(candidate string) (string, bool) {
	s := strings.TrimSpace(candidate)
	i := 0
	for i < len(s) && (s[i] == '*' || s[i] == '_') {
		i++
	}
	if i > 0 && (i == len(s) || s[i] == ' ' || s[i] == '\t') {
		return "", false // list bullet (or emphasis chars only) — never a verdict
	}
	var b strings.Builder
	for j := 0; j < len(s); j++ {
		if s[j] == '*' || s[j] == '_' {
			continue
		}
		b.WriteByte(s[j])
	}
	return strings.TrimSpace(b.String()), true
}

// emphasisSpan is one markdown bold span found on a line.
type emphasisSpan struct {
	start   int    // byte index of the opening delimiter run within the line
	content string // text between the delimiters
}

// emphasisSpans returns the bold-emphasis spans on line. Only runs of 2-3 "*" or
// "_" count (bold / bold-italic); a single-char run is italic and is intentionally
// NOT accepted — "the branch is *mergeable*, but the logic is wrong" must not be
// mistaken for a verdict. A closing run must match the opener in both char and
// length; an unterminated run is skipped and scanning continues.
func emphasisSpans(line string) []emphasisSpan {
	var out []emphasisSpan
	n := len(line)
	for i := 0; i < n; {
		c := line[i]
		if c != '*' && c != '_' {
			i++
			continue
		}
		runStart := i
		for i < n && line[i] == c {
			i++
		}
		runLen := i - runStart
		if runLen < 2 || runLen > 3 {
			continue // not a bold opener — resume scanning after the run
		}
		closeStart, closeEnd := findEmphasisClose(line, i, c, runLen)
		if closeStart < 0 {
			continue // unterminated — resume scanning after the run
		}
		out = append(out, emphasisSpan{start: runStart, content: line[i:closeStart]})
		i = closeEnd
	}
	return out
}

// findEmphasisClose returns the [start,end) byte range of the next run of exactly
// `want` copies of c at or after `from` in line, or (-1,-1) if there is none.
func findEmphasisClose(line string, from int, c byte, want int) (int, int) {
	n := len(line)
	for i := from; i < n; {
		if line[i] != c {
			i++
			continue
		}
		start := i
		for i < n && line[i] == c {
			i++
		}
		if i-start == want {
			return start, i
		}
	}
	return -1, -1
}

// verdictNegated reports whether the text preceding a line-start-anchored verdict
// token negates it. Same-line left context needs no check: the anchor in
// lineVerdictCandidates already guarantees nothing but whitespace precedes the token
// on its own line, so the only negator that can reach it is a soft-wrapped
// continuation from ABOVE ("This change is not\nready in my judgement to be
// considered\nmergeable.").
//
// prevLines is every line above the token's line, in order.
func verdictNegated(prevLines []string) bool {
	return hasNegator(negatorWindow(negationContext(prevLines)))
}

// negationContext FLATTENS the sentence fragment that a soft wrap could have carried
// into the verdict token, so a line break can never hide a negation from the scan.
//
// It builds the fragment by walking back from the token and stopping at the BLANK LINE
// (paragraph break) above it — a construct a soft-wrapped sentence can never contain.
// Everything from there to the token is joined and whitespace-normalized, which is what
// makes the scan blind to line breaks.
//
// The SENTENCE clip and the word-count cap are applied downstream by negatorWindow, the
// same helper the prose phrases and "lgtm" use — one negation mechanism, three callers.
func negationContext(prevLines []string) string {
	start := len(prevLines)
	for start > 0 && strings.TrimSpace(prevLines[start-1]) != "" {
		start--
	}
	return flatten(strings.Join(prevLines[start:], " "))
}

// containsWordBounded reports whether word occurs in lower delimited by non-letters
// on both sides.
func containsWordBounded(lower, word string) bool {
	for from := 0; ; {
		i := strings.Index(lower[from:], word)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(word)
		if (start == 0 || !isASCIILetter(lower[start-1])) &&
			(end == len(lower) || !isASCIILetter(lower[end])) {
			return true
		}
		from = start + 1
	}
}

// codeElision is what a stripped region leaves behind in the text the approval scan
// reads. It is deliberately NOT a space.
//
// A space was a CONFIRMED false-approve: whitespace is exactly what the line-start
// anchor trims, so eliding an inline code span to " " PROMOTED a mid-line bold span
// to column 0 and forged the anchor —
//
//	"`The author claims: `**Mergeable.** I disagree; the auth gate is gone."
//
// became " **Mergeable.** I disagree; …", whose first non-blank content is the bold
// span, so an attributed quote inside a rejection approved. The elision must keep the
// column OCCUPIED. NUL does: it is not whitespace (TrimLeft leaves it in place, so the
// anchor still sees prose before the span) and it is not an ASCII letter (so
// phraseWordBounded / containsWordBounded / normalizeEmphasis keep treating it as the
// token delimiter the space used to be).
const codeElision = "\x00"

// elide is the replacement text for a removed region: one codeElision byte followed by
// one newline per newline the region contained.
//
// Preserving the LINE COUNT matters as much as preserving the column: the line-start
// anchor, negationContext's paragraph-break walk, and stripIndentedCode all read the
// document's line geometry, and collapsing a fenced block onto one line would splice
// the prose above it onto the prose below.
func elide(region string) string {
	return codeElision + strings.Repeat("\n", strings.Count(region, "\n"))
}

// stripHiddenRegions removes document regions a human reader does not see, BEFORE
// the APPROVAL scan. Content invisible on the rendered page must never drive the
// verdict — both shapes below were CONFIRMED false-approves:
//
//   - HTML comments, INCLUDING multi-line ones. A "**Mergeable.**" parked inside
//     "<!-- … -->" approved a body whose visible text rejects.
//   - <details> blocks. GitHub renders them COLLAPSED, so a stale
//     "<details><summary>Old verdict</summary> **Mergeable.** </details>" above a
//     live rejection approved on text the reader never expanded.
//
// Both are dropped delimiters-and-all and elided (see elide / codeElision).
//
// It returns malformed=true when an UNTERMINATED opener forced it to drop the rest of
// the body. See stripCodeMarkup's malformed contract — the caller must not infer an
// approval from a document whose tail it could not read.
//
// This runs on the APPROVAL path only. blockingRejection deliberately reads the RAW
// body, so a rejection can never be hidden by wrapping it in a comment either.
func stripHiddenRegions(body string) (string, bool) {
	body, m1 := stripRegion(body, "<!--", "-->")
	body, m2 := stripRegion(body, "<details", "</details>")
	return body, m1 || m2
}

// stripRegion removes every open…close region (case-insensitive on the delimiters,
// which are ASCII) from body, eliding each. An unterminated open delimiter drops the
// rest of the body and reports malformed=true.
func stripRegion(body, open, close string) (string, bool) {
	lower := strings.ToLower(body)
	if !strings.Contains(lower, open) {
		return body, false
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(lower[i:], open)
		if j < 0 {
			b.WriteString(body[i:])
			return b.String(), false
		}
		start := i + j
		b.WriteString(body[i:start])
		k := strings.Index(lower[start+len(open):], close)
		if k < 0 {
			// Unterminated opener — drop the remainder AND flag the body malformed.
			b.WriteString(elide(body[start:]))
			return b.String(), true
		}
		end := start + len(open) + k + len(close)
		b.WriteString(elide(body[start:end]))
		i = end
	}
}

// stripIndentedCode drops CommonMark indented-code lines (4+ leading spaces) before
// the APPROVAL scan. An illustrative "    **Mergeable.**" — a reviewer showing the
// shape they do NOT want — was a CONFIRMED false-approve. Runs AFTER stripCodeMarkup,
// so fenced content is already gone and cannot be double-counted.
func stripIndentedCode(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.HasPrefix(ln, "    ") || strings.HasPrefix(ln, "\t") {
			out = append(out, "")
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// stripCodeMarkup removes markdown code regions from body before verdict
// phrase-matching, so a verdict phrase quoted inside a backtick span or fenced
// block (e.g. an illustrative test-case name like `"changes requested beats no
// new issues"`) can never be mistaken for the review's actual verdict. The
// genuine @claude verdict lives in prose; illustrative phrases get code-fenced —
// stripping the markup is the discriminator (PR #322 false-negative).
//
// Removal rules:
//   - Fenced block: a run of >=3 backticks opens it; the next run of >=3
//     backticks closes it. The whole region (delimiters included) is dropped.
//     An unterminated fence drops the remainder of the body AND reports
//     malformed=true (see below).
//   - Inline span: a run of 1-2 backticks opens it; the next run of EQUAL length
//     closes it. The whole region (delimiters included) is dropped. An
//     unterminated inline run keeps its literal backtick(s) and resumes scanning
//     after them — matching CommonMark, where a lone backtick is just text. It is
//     therefore NOT malformed: nothing was dropped.
//
// Each removed region is elided (see elide / codeElision), which preserves both the
// column it occupied and its line count. Bodies with no backticks pass through
// byte-identical (the negative-phrase plain-prose cases are unaffected).
//
// THE malformed RETURN (fail closed). An unterminated fence means we silently threw
// away the tail of the document — and the tail is where a reviewer's final word lives.
// PROVEN false-approve:
//
//	"No new issues in the touched files.\n\n```diff\n- old\n+ new\n\nVerdict: changes
//	 requested — the diff above removes the auth check."
//
// The fence never closes, so everything from "```diff" onward — INCLUDING the
// "changes requested" verdict — was dropped, while the positive phrase ABOVE the fence
// survived and approved. An approval must never be inferred from a document we could
// not fully read, so parseVerdict downgrades every prose-path approval to "unclear"
// when malformed is set. Rejections are unaffected (blockingRejection reads the RAW
// body), and so is the marker path (parseVerdict reads it from the RAW body at step 0,
// before any stripping) — a legitimate marker still wins inside a malformed body.
func stripCodeMarkup(body string) (string, bool) {
	var b strings.Builder
	i := 0
	n := len(body)
	for i < n {
		if body[i] != '`' {
			b.WriteByte(body[i])
			i++
			continue
		}
		// Count the length of this opening backtick run.
		runStart := i
		for i < n && body[i] == '`' {
			i++
		}
		runLen := i - runStart
		if runLen >= 3 {
			// Fenced block: close on the next run of >=3 backticks.
			closeAt := findBacktickRun(body, i, 3, false)
			if closeAt < 0 {
				// Unterminated fence — drop the remainder and flag the body malformed.
				b.WriteString(elide(body[runStart:]))
				return b.String(), true
			}
			// Skip past the closing fence run.
			j := closeAt
			for j < n && body[j] == '`' {
				j++
			}
			b.WriteString(elide(body[runStart:j]))
			i = j
			continue
		}
		// Inline span (runLen 1 or 2): close on the next run of EQUAL length.
		closeAt := findBacktickRun(body, i, runLen, true)
		if closeAt < 0 {
			// Unterminated inline run — keep the literal backtick(s) as text.
			b.WriteString(body[runStart:i])
			continue
		}
		b.WriteString(elide(body[runStart : closeAt+runLen]))
		i = closeAt + runLen
	}
	return b.String(), false
}

// findBacktickRun returns the start index of the next backtick run in body at or
// after `from` that satisfies the length predicate, or -1 if none. When exact is
// true the run length must equal want; otherwise it must be >= want. Runs longer
// than a wanted exact length are skipped (they are not valid inline closers).
func findBacktickRun(body string, from, want int, exact bool) int {
	i := from
	n := len(body)
	for i < n {
		if body[i] != '`' {
			i++
			continue
		}
		runStart := i
		for i < n && body[i] == '`' {
			i++
		}
		runLen := i - runStart
		if (exact && runLen == want) || (!exact && runLen >= want) {
			return runStart
		}
		// Not a match (e.g. a longer run than an exact-length inline closer) —
		// keep scanning past it.
	}
	return -1
}

// EMOJI STATUS MARKERS — DELETED (do not reinstate).
//
// "Status: ✅" and "Merge Readiness: ✅ READY" were legacy structured-@claude-template
// fallbacks, matched first as bare substrings and later as LINE-START-anchored markers.
// Both are gone: A BUILD STATUS IS NOT A MERGE VERDICT, and even the line-start anchor
// could not save them, because a per-CHECK CI status line legitimately begins its own
// line. PROVEN false-approve against the anchored version:
//
//	"Status: ✅ CI green (42/42 tests pass)\n\nThe migration drops the users table — I'd
//	 rather not land this yet."                                             → APPROVED
//
// The tier-1 bravros-verdict marker supersedes them for any bot we drive, and a
// structured template that genuinely means to sign off can emit a real verdict token
// ("**Mergeable.**") which the structural matcher already accepts. There is nothing left
// for an emoji status line to buy — only a fail-OPEN hole on a gate that authorizes
// autonomous merges (audit Rules 28/31).
//
// The canonical machine-readable verdict marker has TWO accepted forms.
//
// CANONICAL (since B-0342) — a plain-text sentinel line, alone on its line:
//
//	BRAVROS-VERDICT: approved
//	BRAVROS-VERDICT: changes-requested
//
// LEGACY — the original HTML-comment form:
//
//	<!-- bravros-verdict: approved -->
//	<!-- bravros-verdict: changes-requested -->
//
// WHY TWO FORMS (B-0342): the @claude GitHub Action strips HTML comments from the
// model's output before posting, so the legacy marker NEVER survived an Action-posted
// review — tier-1 stamping was unreachable in any repo reviewed by the Action (proven
// on paylog/afterpay PRs #288-#294: 8 requests, 0 markers ever posted; reproduced on
// this repo's PR #382). The sentinel is ordinary visible text, which the Action
// preserves. The legacy form stays accepted for local reviewers and old comments.
//
// Liberal in what we accept (case-insensitive, tolerant of internal whitespace, and —
// for the sentinel — of markdown emphasis wrappers and a trailing period, since models
// love to bold a final verdict), strict in what we emit (the skills post the exact
// byte-for-byte forms above). Only the two sanctioned values match — anything else
// (e.g. "maybe") is NOT a marker and falls through to the prose heuristics.
//
// The sentinel is LINE-ANCHORED on both sides on purpose: a quoted echo
// ("> BRAVROS-VERDICT: approved"), a list item ("- BRAVROS-VERDICT: approved"), or a
// prose mention ("the BRAVROS-VERDICT: approved line must be last") does NOT match.
var (
	verdictMarkerRe = regexp.MustCompile(`(?is)<!--\s*bravros-verdict\s*:\s*(approved|changes-requested)\s*-->`)
	// The emphasis wrappers must sit ADJACENT to the token (no space): that is what
	// separates a bolded sign-off ("**BRAVROS-VERDICT: approved**") from a markdown
	// list-item echo ("* BRAVROS-VERDICT: approved"), which must NOT match.
	verdictSentinelRe = regexp.MustCompile(`(?im)^[ \t]*[*_~` + "`" + `]{0,3}bravros-verdict[ \t]*:[ \t]*(approved|changes-requested)[.!]?[*_~` + "`" + `]{0,3}[ \t\r]*$`)
)

// findVerdictMarker returns the value of the LAST verdict marker in body (either
// form, ordered by position), lowercased, or ("", false) when the body carries no
// well-formed marker.
//
// LAST wins, deliberately:
//   - a bot that revises its verdict mid-review emits a new marker; the newest
//     statement is the one it stands behind;
//   - it makes an earlier quoted/echoed marker (e.g. the bot repeating our own
//     instruction block back at us) non-authoritative — and because the request
//     block lists "changes-requested" second, a verbatim echo of BOTH lines with
//     no real verdict after them resolves changes-requested, i.e. fail closed.
//
// The body handed here MUST be the RAW comment body — see parseVerdict step (0).
func findVerdictMarker(body string) (string, bool) {
	last, verdict := -1, ""
	for _, re := range []*regexp.Regexp{verdictMarkerRe, verdictSentinelRe} {
		for _, m := range re.FindAllStringSubmatchIndex(body, -1) {
			if m[0] > last {
				last, verdict = m[0], strings.ToLower(body[m[2]:m[3]])
			}
		}
	}
	if last < 0 {
		return "", false
	}
	return verdict, true
}

// parseVerdict scans a bot review body for verdict markers.
//
// TIER 1 — the bravros-verdict marker (AUTHORITATIVE). See step (0) below.
// TIER 2 — the prose heuristics (best-effort FALLBACK, biased to fail closed).
//
// REPORT-ONLY (P-0183 G1, operator-decided 2026-07-13): the TIER-2 prose verdict is
// ADVISORY OUTPUT for a human reader (--field verdict, --terse, logs) and never feeds
// stampAuthorityFor in either direction. Six adversarial panels concluded prose
// classification is unwinnable on the approval side (the conditional sign-off is
// structural), and the false-veto class (PR #294's "Previously blocking …: fixed")
// showed the rejection side misfires too — and a false veto used to destroy the
// operator's token escape hatch. Tier-1 markers (B-0342) survive the Action, so the
// classifier is no longer needed as an authority. Do not re-wire it into the gate.
//
// FINAL POLICY (operator-decided) for the TIER-2 fallback: "the bot's own verdict
// wins; block only on BLOCKING findings."
//   - The bot's structural "**Mergeable.**" is authoritative for APPROVAL.
//   - A finding the bot itself marks NON-BLOCKING ("Minor nit (non-blocking): …",
//     "I found no blocking issues") does NOT veto.
//   - A BLOCKING-severity finding vetoes DECISIVELY — regardless of any approval
//     token and regardless of document order.
//
// Algorithm (marker first; then rejection; the two prose sides are deliberately
// asymmetric):
//
//  0. findVerdictMarker over the RAW body — if the bot emitted the canonical
//     BRAVROS-VERDICT sentinel (or the legacy <!-- bravros-verdict: … --> marker),
//     that IS the verdict. Return immediately.
//     This runs before EVERYTHING (before the rejection scan, before any stripping)
//     for two reasons:
//     a) the legacy marker form is an HTML comment, and stripHiddenRegions deletes
//     HTML comments — reading it from a stripped body would always find nothing;
//     b) the broad prose rejection scan must NOT be able to veto an "approved"
//     marker. That scan's false-veto surface (a bare ❌ inside a CI summary
//     table, the word "blocking" in an unrelated sentence) is EXACTLY what the
//     marker exists to eliminate. The marker is the bot's own explicit,
//     structured verdict; our prose guess does not get to overrule it.
//     Steps 1-7 remain as the fallback for legacy comments, human reviews, and any
//     repo not yet posting its review request through our skills.
//  1. blockingRejection over the RAW body — a broad, nit-aware substring scan. Any
//     non-exempt hit is decisive changes-requested. Runs before ANY stripping so a
//     stray backtick / HTML comment cannot hide the veto.
//  2. Strip, for the approval path only: hidden regions (HTML comments, collapsed
//     <details>), then code markup (spans + fences), then indented code blocks.
//     Everything a human reader cannot see is now gone. If either stripper had to
//     drop the tail of the body because of an UNTERMINATED opener (a `<!--`, a
//     `<details`, a ``` fence), it reports malformed — and a malformed body can
//     never yield an approval (step 5a). We may have thrown away the reviewer's
//     final word; guessing "approved" from the surviving prefix is the one mistake
//     this gate cannot afford.
//     2a. TWO VIEWS are built from the stripped body and they are NOT interchangeable:
//     `lined` keeps the document's LINE GEOMETRY (what the structural matchers —
//     line-start anchor, standalone-line token, table-pipe rejection, emphasis
//     spans — must read), while `flat` collapses every whitespace run to one space
//     (what every CONTIGUITY-based matcher — the phrase scan, the negation window,
//     the lgtm hedge/condition windows — must read, so that a soft wrap can never
//     change a verdict). See flatten().
//  3. negativePhrases (the "changes requested" family) over the FLAT stripped body —
//     decisive. Stripped, not raw, because illustrative test names quote them; flat,
//     so a hard wrap ("changes\nrequested") cannot hide the veto either.
//  4. positivePhrases over the FLAT body — each counts only when word-bounded and
//     carrying no negator in its left word-window (phraseApproves / negatorBefore).
//     "lgtm" additionally routes through lgtmApproves for its forward-condition guard.
//  5. verdictTokenApproves over the LINED body — the STRICT structural token: bolded or
//     alone on its line, anchored at line start, never behind a heading marker. Bare
//     tokens and every conditional/hedge-prone phrase ("merge when ready", "merge-ready",
//     "good to merge", "clear to ship") are excluded from the prose table in step 4
//     because a prose mention ("the branch is mergeable, but the logic is wrong",
//     "restore the auth check and this becomes merge-ready") must not approve.
//     5a. Every approval in steps 4-5 funnels through approveUnlessMalformed.
//  6. Fall back to ("unclear", false).
//
// There is deliberately NO emoji-status step any more: "Status: ✅" / "Merge Readiness:
// ✅ READY" were deleted outright (a build status is not a merge verdict — see the block
// above verdictMarkerRe).
func parseVerdict(body string) parseVerdictResult {
	// (0) MARKER FIRST, on the RAW body — authoritative, short-circuits everything.
	// Must precede stripHiddenRegions (which deletes HTML comments, i.e. the legacy
	// marker form) AND blockingRejection (whose broad prose scan would otherwise
	// false-veto an explicit "approved" marker over a ❌ in a CI summary table).
	if v, ok := findVerdictMarker(body); ok {
		return parseVerdictResult{
			Verdict:       v,
			Tier:          verdictTierMarker,
			Confident:     true,
			MatchedPhrase: "bravros-verdict marker",
		}
	}

	// (1) FAIL CLOSED FIRST, on the RAW body. A rejection must be un-hideable, so
	// this scan never reads a stripped/filtered view of the document.
	// TIER 2, REJECTION SIDE — stays DECISIVE (Confident: true). Only the approval
	// direction was demoted; weakening the fail-closed direction would buy nothing.
	if sig, ok := blockingRejection(body); ok {
		return parseVerdictResult{
			Verdict:       "changes-requested",
			Tier:          verdictTierProse,
			Confident:     true,
			MatchedPhrase: sig,
		}
	}

	// (2) Approval path only: drop everything the reader cannot see — HTML comments
	// and collapsed <details>, then code spans / fenced blocks, then indented code.
	// `malformed` is set when an unterminated opener forced a stripper to discard the
	// tail of the body; it fails every approval below closed (step 6a).
	visible, hiddenMalformed := stripHiddenRegions(body)
	visible, codeMalformed := stripCodeMarkup(visible)
	malformed := hiddenMalformed || codeMalformed

	// (2a) TWO VIEWS, and they are not interchangeable.
	//
	// `lined` — the LINE-STRUCTURED view. The structural matchers (line-start anchor,
	// standalone-line token, table-pipe rejection, emphasis spans) read the document's
	// line geometry and would be destroyed by flattening.
	//
	// `flat` — the WHITESPACE-NORMALIZED view (every run of spaces/tabs/CR/newlines
	// collapsed to a single space, lowercased). Every CONTIGUITY-based matcher reads
	// THIS: the positive-phrase scan, the negation window, the lgtm hedge/condition
	// windows. A line break must never be able to change a verdict.
	lined := stripIndentedCode(visible)
	flat := strings.ToLower(flatten(lined))

	// (3) The "changes requested" family — decisive, on the FLAT stripped body.
	for _, n := range negativePhrases {
		if strings.Contains(flat, n) {
			return parseVerdictResult{
				Verdict:       "changes-requested",
				Tier:          verdictTierProse,
				Confident:     true,
				MatchedPhrase: n,
			}
		}
	}

	// (4) Positives count only when word-bounded AND un-negated: every match's left
	// word-window (flattened, sentence-clipped) is scanned for ANY negator, so an
	// interposed word or a soft wrap cannot smuggle an approval through. "lgtm" is the
	// broadest phrase, so it additionally routes through lgtmApproves for its
	// forward-condition guard ("LGTM once the nil deref is handled").
	for _, p := range positivePhrases {
		if p == "lgtm" {
			if lgtmApproves(flat) {
				return approveUnlessMalformed("lgtm", malformed)
			}
			continue
		}
		if phraseApproves(flat, p) {
			return approveUnlessMalformed(p, malformed)
		}
	}

	// (5) Bare / conditional verdict tokens ("Mergeable", "Approved to merge", "Merge
	// when ready", "Merge-ready", "Good to merge", "Clear to ship") are too broad for the
	// plain-prose table above, so they get a structural matcher: they approve only when
	// bolded or alone on their own line, anchored at line start. See verdictTokenApproves.
	if tok, ok := verdictTokenApproves(lined); ok {
		return approveUnlessMalformed(tok, malformed)
	}

	return parseVerdictResult{Verdict: "unclear", Confident: false}
}

// approveUnlessMalformed is the single exit through which every PROSE-PATH (TIER-2)
// approval passes (steps 4-5 of parseVerdict). Two things happen here, and only one of
// them is old:
//
// (1) MALFORMED → "unclear" (unchanged). An unterminated `<!--`, `<details` or ``` fence
// forced a stripper to discard the tail of the document — exactly where a reviewer's
// final "changes requested" tends to live. We do not guess "approved" from a document we
// could not fully read.
//
// (2) EVERY SURVIVING APPROVAL IS Confident:FALSE (the change). A tier-2 approval is a
// GUESS about English, and the guess is provably not sound — see the tier constants at
// the top of this file. It is still REPORTED (Verdict stays "approved", MatchedPhrase
// names the phrase we matched, so `--field verdict`, `--terse` and the logs still show
// the operator what the bot appears to have said), but Confident:false means
// writeStampFromVerdict will NOT write the merge-authorizing stamp on its own. Only the
// operator's out-of-band review-stamp token can promote this into a stamp — see
// stampAuthorityFor.
//
// The TIER-1 marker path never reaches here: it returns from the RAW body at step 0, so
// a well-formed marker still wins inside a malformed body AND keeps Confident:true.
func approveUnlessMalformed(matched string, malformed bool) parseVerdictResult {
	if malformed {
		return parseVerdictResult{Verdict: "unclear", Confident: false}
	}
	return parseVerdictResult{
		Verdict:       "approved",
		Tier:          verdictTierProse,
		Confident:     false, // a prose guess never self-authorizes a merge
		MatchedPhrase: matched,
	}
}

// reviewEntry is the subset of a GitHub review returned by `gh pr view --json reviews`.
type reviewEntry struct {
	State       string `json:"state"`
	SubmittedAt string `json:"submittedAt"`
	Body        string `json:"body"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
}

// botCandidate is the unified shape for comparing a review or comment by timestamp.
// Kind is "review" or "comment".
type botCandidate struct {
	Kind        string // "review" or "comment"
	Login       string
	Body        string
	State       string
	SubmittedAt string // ISO 8601 timestamp (submittedAt for reviews, created_at for comments)
}

// prReviewResultJSON is the structured output emitted on a successful --wait match.
type prReviewResultJSON struct {
	PR          string `json:"pr"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	Author      string `json:"author"`
	SubmittedAt string `json:"submitted_at"`
	BodyPreview string `json:"body_preview"`
}

// stampAuthority names WHOSE authority a review stamp would be written on. It exists so
// the decision below is a value you can read and test, not a boolean expression you have
// to re-derive.
type stampAuthority string

const (
	// stampNone — nobody authorized this. Write nothing.
	stampNone stampAuthority = "none"
	// stampByMarker — TIER 1. The bot asserted "BRAVROS-VERDICT: approved" (or the
	// legacy HTML-comment form). Sound, self-authorizing, and unchanged from the
	// original behavior.
	stampByMarker stampAuthority = "marker"
	// stampByToken — TIER 2 prose approval, promoted by the operator's out-of-band
	// review-stamp token. The HUMAN authorized this merge, not the classifier. The
	// token is consumed when it is spent.
	stampByToken stampAuthority = "token"
)

// stampAuthorityFor is the whole merge gate, in seven lines and with no I/O.
//
// It is deliberately PURE (verdict + "is a valid token present" → decision) so the
// catastrophic path can be pinned by unit tests without a filesystem, a git repo, or a
// GitHub client: for every known false-approve fixture — including the conditional
// sign-offs the prose classifier still gets WRONG ("LGTM assuming you fix the
// double-billing") — the tests assert this returns stampNone. That is the guarantee that
// survives the classifier being wrong.
//
// P-0183 G1 contract — the TIER-1 MARKER is the ONLY verdict input to stamp authority:
//
//	(a) tier-1 marker approved (+ Confident) → stampByMarker. The sound path.
//	(b) tier-1 marker changes-requested → stampNone, and the token CANNOT override it —
//	    an explicit bot veto stands.
//	(c) NO marker (tier-2 prose, unclear, empty) + a valid review-stamp token →
//	    stampByToken, REGARDLESS of what the prose classifier guessed. The operator read
//	    the review and said yes, out-of-band, from another terminal; a prose guess must
//	    not be able to take that escape hatch away. (Before this change, a prose false
//	    veto — e.g. "Previously blocking … : fixed" matching bare "blocking" — returned
//	    stampNone before the token was ever consulted, destroying the operator's only
//	    recovery path for a marker-less review.)
//	(d) anything else → stampNone. In particular a tier-2 approval with NO token writes
//	    NOTHING (and exits 0 — a safe no-op, not an error).
//
// The prose verdict is still computed and REPORTED (--field verdict, --terse, logs) —
// it is advisory output for a human reader, never a gate, in either direction. Six
// adversarial panels concluded prose classification is unwinnable on the approval side,
// and the false-veto class (this fix) showed the rejection side misfires too; tier-1
// markers (B-0342) make the classifier unnecessary as an authority.
//
// Note there is no path to a stamp that bypasses both the marker and the operator token.
func stampAuthorityFor(vr parseVerdictResult, tokenValid bool) stampAuthority {
	// Tier-1 marker: authoritative in BOTH directions, never token-overridable.
	if vr.Tier == verdictTierMarker {
		if vr.Verdict == "approved" && vr.Confident {
			return stampByMarker
		}
		return stampNone
	}
	// No marker: tier-2 prose has NO stamp authority in either direction. The
	// out-of-band token is the operator's escape hatch for every marker-less review.
	if tokenValid {
		return stampByToken
	}
	return stampNone
}

// writeStampFromVerdict parses the bot review body and writes the review stamp only when
// someone with the authority to do so said yes — the bot via a tier-1 marker, or the
// operator via the out-of-band review-stamp token. Returns the exit code to use
// (0 = stamp written OR safe no-op, 1 = stamp write error). Logs all outcomes to stderr.
//
// Shared by runWaitMode (--wait) and the standalone --write-stamp path (no --wait).
func writeStampFromVerdict(prNumber string, body string) int {
	vr := parseVerdict(body)

	switch stampAuthorityFor(vr, reviewStampGate.Valid()) {
	case stampByMarker:
		return emitStamp(prNumber, vr, "")

	case stampByToken:
		// CONSUME BEFORE WRITE — there is no stamp-without-consume path (the same
		// invariant Rule 47 holds for the verify-suite token). The token is single-use:
		// it authorizes exactly ONE stamp, so a stale token can never silently
		// authorize a second, unreviewed PR later in the same pipeline.
		//
		// Consuming first means a failed stamp write burns the token and the operator
		// must re-mint. That is the correct direction to fail: the alternative
		// (write, then consume) leaves the token on disk whenever the consume races or
		// errors, which is an unauthorized second merge waiting to happen.
		if !consumeReviewStampToken() {
			// Token vanished or expired between Valid() and the consume. Treat exactly
			// like "no token": write nothing, guide the operator.
			adviseTier2NoToken(prNumber, vr)
			return 0
		}
		return emitStamp(prNumber, vr, "operator review-stamp token (out-of-band, now CONSUMED)")

	default: // stampNone
		switch {
		case vr.Tier == verdictTierMarker && vr.Verdict != "approved":
			// TIER-1 VETO. The bot said changes-requested explicitly; the token cannot
			// override an explicit marker verdict.
			fmt.Fprintf(os.Stderr, "verdict: changes-requested (tier-1 marker) — no stamp written; the bot's explicit veto stands and is NOT token-overridable. Address feedback then re-request review.\n")
		case vr.Verdict == "approved":
			// TIER-2 PROSE APPROVAL, NO TOKEN. This is the new safe no-op, and it is
			// NOT a warning — nothing went wrong. The bot seems to approve; we simply
			// do not accept prose as authorization. Tell the operator how to say yes.
			adviseTier2NoToken(prNumber, vr)
		case vr.Verdict == "changes-requested" && vr.Confident:
			// TIER-2 PROSE REJECTION, NO TOKEN. Advisory only (P-0183 G1): it is
			// reported so a human can read it, but it is a guess, and the same escape
			// hatch that authorizes a marker-less approval can override a marker-less
			// misread (e.g. "Previously blocking … : fixed" matching bare "blocking").
			fmt.Fprintf(os.Stderr, "verdict: changes-requested (tier=%s, matched=%q, advisory) — no stamp written. Address feedback then re-request review;\n", vr.Tier, vr.MatchedPhrase)
			fmt.Fprintf(os.Stderr, "   if you have READ the review and this is a prose misread, the out-of-band escape hatch applies: bravros pr-review unlock (separate terminal), then re-run --write-stamp.\n")
		default:
			fmt.Fprintf(os.Stderr, "⚠️  verdict unclear — no stamp written. Read the bot reply yourself; if you agree, see `bravros pr-review unlock`.\n")
		}
		return 0
	}
}

// emitStamp writes the stamp and logs the outcome, naming the AUTHORITY it was written
// on. authority is "" for the tier-1 marker path (the bot's own assertion) and a
// human-readable phrase for the token path, so the stderr record always answers "who
// authorized this merge?".
func emitStamp(prNumber string, vr parseVerdictResult, authority string) int {
	sr, err := github.WriteReviewStamp(prNumber, "approved", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  stamp write failed: %v\n", err)
		return 1
	}
	if sr.Skipped {
		fmt.Fprintf(os.Stderr, "stamp already present at %s — skipping write\n", sr.Path)
		return 0
	}
	if authority == "" {
		fmt.Fprintf(os.Stderr, "✅ review stamp written: %s (verdict=approved, tier=%s, matched=%q)\n",
			sr.Path, vr.Tier, vr.MatchedPhrase)
		return 0
	}
	// Name the tier-2 verdict + phrase we matched, so the record shows exactly what
	// prose the human was trusting (or overriding) when they minted the token.
	fmt.Fprintf(os.Stderr, "✅ review stamp written: %s (stamp=approved, prose-guess=%s, tier=%s, matched=%q)\n",
		sr.Path, vr.Verdict, vr.Tier, vr.MatchedPhrase)
	fmt.Fprintf(os.Stderr, "   authority: %s — the review carried NO BRAVROS-VERDICT marker; this stamp\n", authority)
	fmt.Fprintf(os.Stderr, "   rests on the operator's out-of-band approval, not on the prose classifier.\n")
	return 0
}

// adviseTier2NoToken prints the ACTIONABLE guidance for a tier-2 approval with no token.
// Deliberately not a scary warning: the safe thing just happened. The operator needs to
// know (a) what the bot appears to have said, (b) that prose is not an authorization,
// and (c) exactly how to authorize it deliberately if they agree.
func adviseTier2NoToken(prNumber string, vr parseVerdictResult) {
	fmt.Fprintf(os.Stderr, "ℹ️  the bot appears to APPROVE (tier=prose, matched=%q), but its review carries NO\n", vr.MatchedPhrase)
	fmt.Fprintf(os.Stderr, "   BRAVROS-VERDICT: approved marker — so this is our GUESS about its prose,\n")
	fmt.Fprintf(os.Stderr, "   not its verdict. A guess cannot authorize a merge: no stamp written.\n\n")
	fmt.Fprintf(os.Stderr, "   If you have READ the review and you agree with it, stamp it deliberately:\n")
	fmt.Fprintf(os.Stderr, "     1. Open a separate terminal (outside Claude Code, same machine)\n")
	fmt.Fprintf(os.Stderr, "     2. Run: bravros pr-review unlock\n")
	fmt.Fprintf(os.Stderr, "     3. Return here and re-run: bravros pr-review %s --write-stamp\n\n", prNumber)
	fmt.Fprintf(os.Stderr, "   The token is single-use (5-min TTL) and is consumed by the stamp it authorizes.\n")
	fmt.Fprintf(os.Stderr, "   Claude Code CANNOT mint this token — that is intentional.\n")
}

// prReviewLatestJSON is the stable JSON shape emitted by
// `bravros pr-review <PR> --latest --json`. This field set IS the read-only
// contract — do not rename or remove a field without updating docs/CLI.md and
// example-bravros-cli.md in the same PR (see cli/CLAUDE.md § Docs-sync
// requirement).
type prReviewLatestJSON struct {
	Author   string `json:"author"`    // GitHub login that posted the candidate
	Kind     string `json:"kind"`      // "review" or "comment" — which source it came from
	State    string `json:"state"`     // GitHub review state (e.g. "APPROVED"), or "posted" for a comment
	PostedAt string `json:"posted_at"` // ISO 8601 timestamp
	Body     string `json:"body"`      // raw review/comment body
	Verdict  struct {
		Tier      string `json:"tier"`      // "marker", "prose", or "" when unclear
		Value     string `json:"value"`     // "approved", "changes-requested", or "unclear"
		Confident bool   `json:"confident"` // whether Value is stamp-grade (see parseVerdictResult)
	} `json:"verdict"`
}

// runPRReviewLatest implements `bravros pr-review <PR> --latest [--json]`: a
// READ-ONLY report of the latest bot review/comment and its parsed verdict.
// It fetches via the same fetchLatestBotReviewOrComment used by
// --write-stamp, but NEVER writes the review stamp and NEVER consumes the
// review-stamp token — those stay --write-stamp's exclusive job. Returns the
// process exit code.
func runPRReviewLatest(prNumber string, asJSON bool) int {
	botLogin := os.Getenv("REVIEW_BOT_LOGIN")
	if botLogin == "" {
		botLogin = "claude[bot]"
	}

	candidate, found, err := fetchLatestBotReviewOrComment(prNumber, botLogin, time.Time{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ could not fetch bot review: %v\n", err)
		return 1
	}
	if !found {
		fmt.Fprintf(os.Stderr, "❌ no bot review or comment found for PR #%s\n", prNumber)
		return 1
	}

	vr := parseVerdict(candidate.Body)
	if asJSON {
		return printLatestJSON(candidate, vr)
	}
	printLatestHuman(candidate, vr)
	return 0
}

// printLatestHuman renders the human-readable --latest output: author,
// posted_at, the parsed verdict (tier + value + confidence), and the body.
// Extracted from runPRReviewLatest so it is unit-testable against a stubbed
// *botCandidate + parseVerdictResult without shelling out to gh.
func printLatestHuman(candidate *botCandidate, vr parseVerdictResult) {
	tier := vr.Tier
	if tier == "" {
		tier = "none"
	}
	fmt.Printf("author:     %s\n", candidate.Login)
	fmt.Printf("posted_at:  %s\n", candidate.SubmittedAt)
	fmt.Printf("verdict:    %s (tier=%s, confident=%t)\n", vr.Verdict, tier, vr.Confident)
	fmt.Println("---")
	fmt.Println(candidate.Body)
}

// printLatestJSON renders the --latest --json output: exactly one
// prReviewLatestJSON object on stdout. Extracted from runPRReviewLatest so
// the field set is unit-testable against a stubbed *botCandidate +
// parseVerdictResult without shelling out to gh. Returns the process exit
// code (1 on a marshal failure, which should not happen for this shape).
func printLatestJSON(candidate *botCandidate, vr parseVerdictResult) int {
	out := prReviewLatestJSON{
		Author:   candidate.Login,
		Kind:     candidate.Kind,
		State:    candidate.State,
		PostedAt: candidate.SubmittedAt,
		Body:     candidate.Body,
	}
	out.Verdict.Tier = vr.Tier
	out.Verdict.Value = vr.Verdict
	out.Verdict.Confident = vr.Confident

	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ could not marshal result: %v\n", err)
		return 1
	}
	fmt.Println(string(enc))
	return 0
}

// fetchLatestBotReviewOrComment queries both `gh pr view --json reviews` and
// the issue-comments endpoint (via github.FetchLatestBotCommentJSON), then
// returns the most recent matching entry after sinceTime (if any).
//
// The @claude GitHub Action posts review replies as issue comments (author login
// "claude"), not as formal reviews[]. A comment only counts as a candidate when
// it is bot/Action-authored AND carries a line-anchored BRAVROS-VERDICT
// sentinel — see isVerdictCandidateComment and B-0023. Formal reviews keep
// their existing author-only admission (no sentinel required). This function
// covers both sources so --wait reliably detects a bot reply regardless of
// which posting mechanism was used.
//
// Design note: isBot is called with the bare login from each source. The
// default --bot flag is "claude[bot]" but we also accept the bare login
// "claude" (the Action's actual login) via isBotAction. This avoids changing
// the flag default while still matching Action-posted comments.
//
// Returns (nil, false, nil) when no matching entry is found.
func fetchLatestBotReviewOrComment(prNumber string, botLogin string, sinceTime time.Time) (*botCandidate, bool, error) {
	var candidates []botCandidate

	// --- Source 1: formal reviews ---
	out, _, err := github.Run("gh", "pr", "view", prNumber, "--json", "reviews")
	if err != nil {
		return nil, false, fmt.Errorf("gh pr view: %w", err)
	}
	var reviewData struct {
		Reviews []reviewEntry `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(out), &reviewData); err != nil {
		return nil, false, fmt.Errorf("parse reviews JSON: %w", err)
	}
	for _, r := range reviewData.Reviews {
		if !isBotOrAction(r.Author.Login, botLogin) {
			continue
		}
		if !sinceTime.IsZero() {
			t, err := time.Parse(time.RFC3339, r.SubmittedAt)
			if err == nil && t.Before(sinceTime) {
				continue
			}
		}
		candidates = append(candidates, botCandidate{
			Kind:        "review",
			Login:       r.Author.Login,
			Body:        r.Body,
			State:       r.State,
			SubmittedAt: r.SubmittedAt,
		})
	}

	// --- Source 2: issue comments ---
	repo, repoErr := github.GetRepo()
	if repoErr == nil {
		comment, commentErr := github.FetchLatestBotCommentJSON(repo, prNumber, botLogin)
		if commentErr == nil && comment != nil {
			// A comment counts as a candidate when it is bot/Action-authored
			// AND carries a line-anchored BRAVROS-VERDICT sentinel (B-0023).
			// The @claude Action's presentation copy has already changed once
			// (it no longer opens with the old "**Claude finished ..." prefix —
			// it now posts prose like "### Review complete" followed by a
			// checklist and the sentinel line), so matching on presentation
			// text is not durable; the sentinel is the actual wire contract.
			//
			// isVerdictCandidateComment is what keeps the operator's own
			// "@claude review ..." request comment from being misread as a
			// verdict: that comment literally quotes the sentinel lines
			// (BRAVROS-VERDICT: approved / BRAVROS-VERDICT: changes-requested)
			// as instructions, but it is authored by the operator's login, not
			// the bot/Action login — isBotOrAction is what excludes it.
			if isVerdictCandidateComment(comment.Author, comment.Body, botLogin) {
				if !sinceTime.IsZero() {
					t, err := time.Parse(time.RFC3339, comment.PostedAt)
					if err == nil && t.Before(sinceTime) {
						// skip — predates PR creation
					} else {
						candidates = append(candidates, botCandidate{
							Kind:        "comment",
							Login:       comment.Author,
							Body:        comment.Body,
							State:       "posted",
							SubmittedAt: comment.PostedAt,
						})
					}
				} else {
					candidates = append(candidates, botCandidate{
						Kind:        "comment",
						Login:       comment.Author,
						Body:        comment.Body,
						State:       "posted",
						SubmittedAt: comment.PostedAt,
					})
				}
			}
		}
	}

	// Pick the latest candidate across both sources.
	best := pickLatestCandidate(candidates)

	if best == nil {
		return nil, false, nil
	}
	return best, true, nil
}

// pickLatestCandidate returns the candidate with the most recent SubmittedAt
// timestamp, or nil for an empty pool. Extracted from
// fetchLatestBotReviewOrComment so the latest-bot-entry selection that every
// verdict-consuming path relies on is unit-testable without a live gh client.
func pickLatestCandidate(candidates []botCandidate) *botCandidate {
	var best *botCandidate
	for i := range candidates {
		c := &candidates[i]
		if best == nil {
			best = c
			continue
		}
		tBest, _ := time.Parse(time.RFC3339, best.SubmittedAt)
		tNew, _ := time.Parse(time.RFC3339, c.SubmittedAt)
		if tNew.After(tBest) {
			best = c
		}
	}
	return best
}

// isVerdictCandidateComment reports whether an issue comment is eligible to be
// treated as a verdict candidate by fetchLatestBotReviewOrComment (B-0023):
// bot/Action authorship (isBotOrAction) AND a line-anchored BRAVROS-VERDICT
// sentinel somewhere in the RAW body (findVerdictMarker). Both conditions are
// required — authorship alone would let the operator's own "@claude review ..."
// request comment through (it quotes the sentinel lines as instructions), and
// a sentinel alone would let anyone's echo of the wire contract through.
//
// Extracted as its own pure function so the acceptance rule is unit-testable
// without shelling out to gh — see prreview_test.go's
// TestIsVerdictCandidateComment* cases.
func isVerdictCandidateComment(login, body, botLogin string) bool {
	if !isBotOrAction(login, botLogin) {
		return false
	}
	_, ok := findVerdictMarker(body)
	return ok
}

// isBotOrAction returns true when the login matches the configured botLogin
// (via isBot) OR is the bare "claude" login used by the @claude GitHub Action.
// The Action posts comments as user login "claude" (not "claude[bot]"), so the
// default --bot flag value "claude[bot]" would not match without this widening.
func isBotOrAction(login, botLogin string) bool {
	if isBot(login, botLogin) {
		return true
	}
	// Strip [bot] suffix from botLogin to get the bare Action login.
	// e.g. "claude[bot]" → accept bare "claude" as well.
	if strings.HasSuffix(botLogin, "[bot]") {
		bare := strings.TrimSuffix(botLogin, "[bot]")
		if login == bare {
			return true
		}
	}
	return false
}

// isBot returns true when login matches the expected bot login exactly,
// or when login ends with "[bot]" (GitHub Apps convention).
func isBot(login, botLogin string) bool {
	if login == botLogin {
		return true
	}
	// Allow any [bot] suffix when the botLogin itself ends with [bot]
	if strings.HasSuffix(botLogin, "[bot]") && strings.HasSuffix(login, "[bot]") {
		// Match prefix before [bot]
		prefix := strings.TrimSuffix(botLogin, "[bot]")
		return strings.HasPrefix(login, prefix)
	}
	return false
}
