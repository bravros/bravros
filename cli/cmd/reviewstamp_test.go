package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bravros/bravros/cli/internal/token"
)

// ---------------------------------------------------------------------------
// Test helpers — token fixtures
// ---------------------------------------------------------------------------

// withReviewStampHome redirects the gate's token path into a temp HOME, so these
// tests can never read, mint, or (worse) DELETE the operator's real
// ~/.claude/state/review-stamp-token. token.Gate.Path() resolves via os.UserHomeDir(),
// which reads $HOME.
func withReviewStampHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := reviewStampGate.Path(); got != filepath.Join(home, ".claude", "state", "review-stamp-token") {
		t.Fatalf("gate path not redirected into temp HOME: %s", got)
	}
	return home
}

// withNoReviewStampToken guarantees the gate reads as EMPTY for a test that must
// exercise the no-token path.
func withNoReviewStampToken(t *testing.T) {
	t.Helper()
	withReviewStampHome(t)
}

// mintTestReviewStampToken writes a valid token directly (bypassing the unlock
// command's inside-Claude refusal, which is exactly what a real operator's separate
// terminal does).
func mintTestReviewStampToken(t *testing.T, ttl time.Duration) {
	t.Helper()
	if _, err := reviewStampGate.Mint(int(ttl.Minutes()), "/dev/ttys999"); err != nil {
		t.Fatalf("mint test token: %v", err)
	}
	if !reviewStampGate.Valid() {
		t.Fatal("freshly minted token should be valid")
	}
}

// ---------------------------------------------------------------------------
// THE HEADLINE TEST — the catastrophic path is closed even though the
// classifier is still wrong.
// ---------------------------------------------------------------------------

// TestTier2ConditionalApprovals_NeverStamp is the whole point of the gate.
//
// These are the CONDITIONAL sign-offs from the last adversarial panel — routine
// review traffic that the prose classifier STILL misreads as an approval, and which
// it structurally CANNOT fix: every negation guard scans LEFT of the matched phrase,
// while the condition attaches to its RIGHT ("LGTM **assuming you fix the
// double-billing**"). Widening the scan just invites a longer clause.
//
// So this test does NOT assert the classifier gets them right. It asserts something
// stronger and permanent: WHATEVER verdict these produce, no stamp is ever written
// for them. The classifier is allowed to stay wrong; the merge gate stays shut.
//
// Each case pins BOTH halves of that guarantee:
//   - Confident is FALSE (so writeStampFromVerdict's approved+Confident arm is unreachable)
//   - stampAuthorityFor(...) == stampNone with no token (so NOTHING is written)
func TestTier2ConditionalApprovals_NeverStamp(t *testing.T) {
	// The 9 routine-traffic false-approves. Comments record what the classifier
	// currently THINKS, to make it explicit that we are shipping a known-wrong
	// classifier behind a sound gate.
	bodies := []struct {
		name string
		body string
	}{
		{"LGTM assuming you fix the double-billing",
			"LGTM assuming you fix the double-billing in the retry path."},
		{"Once that is corrected this will be ready to merge",
			"The charge() helper bills twice on a transient failure. Once that is corrected this will be ready to merge."},
		{"Ready to merge? Not yet.",
			"### Ready to merge?\n\nNot yet."},
		{"LGTM but only after you fix the null deref",
			"LGTM, but only after you fix the null deref on line 42."},
		{"provided you address the migration this is safe to merge",
			"Provided you address the destructive migration, this is safe to merge."},
		{"approved for merge subject to the auth fix",
			"Approved for merge subject to the auth check being restored."},
		{"clear to merge as soon as CI is green",
			"Clear to merge as soon as CI is green — it is currently failing."},
		{"LGTM modulo the hardcoded credential",
			"LGTM modulo the hardcoded credential in config.go."},
		{"ready to merge with the caveat that the index is missing",
			"Ready to merge, with the caveat that the composite index is still missing."},
	}

	for _, tc := range bodies {
		t.Run(tc.name, func(t *testing.T) {
			withNoReviewStampToken(t)

			vr := parseVerdict(tc.body)

			// (1) The AUTHORIZATION bit must be false. This is what makes
			// writeStampFromVerdict's `approved && Confident` arm unreachable for
			// these bodies — regardless of what Verdict says.
			if vr.Confident {
				t.Fatalf("CATASTROPHIC: conditional sign-off is Confident (verdict=%q, tier=%q, matched=%q) — it could self-authorize a merge",
					vr.Verdict, vr.Tier, vr.MatchedPhrase)
			}

			// (2) And the stamp-writer must agree: nothing is written, with no token.
			if got := stampAuthorityFor(vr, false); got != stampNone {
				t.Fatalf("CATASTROPHIC: stampAuthorityFor = %q; want %q — a conditional sign-off must never stamp",
					got, stampNone)
			}

			// (3) End-to-end: writeStampFromVerdict actually writes no file and
			// exits 0 (safe no-op, not an error).
			dir := t.TempDir()
			planningDir := filepath.Join(dir, ".planning")
			os.MkdirAll(planningDir, 0o755)
			orig, _ := os.Getwd()
			os.Chdir(dir)
			defer os.Chdir(orig)

			if code := writeStampFromVerdict("4242", tc.body); code != 0 {
				t.Errorf("expected exit 0 (safe no-op), got %d", code)
			}
			if _, err := os.Stat(filepath.Join(planningDir, ".review-stamp-4242.json")); err == nil {
				t.Fatal("CATASTROPHIC: a stamp was written for a conditional sign-off")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The tier contract
// ---------------------------------------------------------------------------

// TestVerdictTiers pins the two-tier contract at the parseVerdict level:
//
//	tier-1 marker approved     → Confident TRUE  (unchanged; the sound path)
//	tier-2 prose approval      → Verdict "approved", Confident FALSE (reported, not authorized)
//	tier-2 prose rejection     → changes-requested, Confident TRUE  (decisive; fail-closed intact)
//	tier-1 marker rejection    → changes-requested, Confident TRUE
func TestVerdictTiers(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantV    string
		wantTier string
		wantConf bool
	}{
		{
			name:     "TIER 1 — marker approved stays authoritative and confident",
			body:     "Some prose.\n\n<!-- bravros-verdict: approved -->",
			wantV:    "approved",
			wantTier: verdictTierMarker,
			wantConf: true,
		},
		{
			name:     "TIER 1 — marker changes-requested stays confident",
			body:     "Some prose.\n\n<!-- bravros-verdict: changes-requested -->",
			wantV:    "changes-requested",
			wantTier: verdictTierMarker,
			wantConf: true,
		},
		{
			name:     "TIER 2 — the real @claude sign-off shape reports approved but is NOT confident",
			body:     "### Verdict\n\n**Mergeable.** The fix is correct and all targeted tests pass.",
			wantV:    "approved",
			wantTier: verdictTierProse,
			wantConf: false,
		},
		{
			name:     "TIER 2 — LGTM overall reports approved but is NOT confident",
			body:     "LGTM overall, but fix the typo in the comment.",
			wantV:    "approved",
			wantTier: verdictTierProse,
			wantConf: false,
		},
		{
			name:     "TIER 2 — the VERBATIM pr1343 final review reports approved but is NOT confident",
			body:     pr1343FinalReviewBody,
			wantV:    "approved",
			wantTier: verdictTierProse,
			wantConf: false,
		},
		{
			name:     "TIER 2 — the VERBATIM pr1343 first review reports approved but is NOT confident",
			body:     pr1343FirstReviewBody,
			wantV:    "approved",
			wantTier: verdictTierProse,
			wantConf: false,
		},
		{
			name:     "TIER 2 — prose rejection stays DECISIVE and confident",
			body:     "**Blocking.** The nil deref on line 42 crashes production.",
			wantV:    "changes-requested",
			wantTier: verdictTierProse,
			wantConf: true,
		},
		{
			name:     "TIER 2 — changes-requested prose stays decisive and confident",
			body:     "Changes requested: please fix the error handler.",
			wantV:    "changes-requested",
			wantTier: verdictTierProse,
			wantConf: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseVerdict(tc.body)
			if got.Verdict != tc.wantV {
				t.Errorf("verdict = %q; want %q", got.Verdict, tc.wantV)
			}
			if got.Tier != tc.wantTier {
				t.Errorf("tier = %q; want %q", got.Tier, tc.wantTier)
			}
			if got.Confident != tc.wantConf {
				t.Errorf("confident = %v; want %v", got.Confident, tc.wantConf)
			}
		})
	}
}

// TestTier2ApprovalNeverSelfAuthorizes is the INVARIANT, stated once and for all:
// there is no body anywhere in the corpus for which a tier-2 approval can stamp
// without a token. Guards against a future edit re-flipping Confident on the prose
// approval path.
func TestTier2ApprovalNeverSelfAuthorizes(t *testing.T) {
	corpus := []string{
		"### Verdict\n\n**Mergeable.** All good.",
		"LGTM overall.",
		"Ready to merge.",
		"Safe to merge — no findings.",
		"Verdict: approved.",
		"**Merge-ready.**",
		"**Good to merge.**",
		"**Clear to ship.**",
		"**Merge when ready.**",
		"**Approved to merge.**",
		"no-new-blockers",
		pr1343FinalReviewBody,
		pr1343FirstReviewBody,
	}
	for _, body := range corpus {
		vr := parseVerdict(body)
		if vr.Verdict != "approved" {
			continue // not an approval; not what this invariant is about
		}
		if vr.Tier != verdictTierProse {
			t.Errorf("body %q: expected tier-2 prose, got %q", body, vr.Tier)
		}
		if vr.Confident {
			t.Errorf("INVARIANT VIOLATED — tier-2 approval is Confident: %q", body)
		}
		if got := stampAuthorityFor(vr, false); got != stampNone {
			t.Errorf("INVARIANT VIOLATED — tier-2 approval stamps without a token: %q → %q", body, got)
		}
	}
}

// TestStampAuthorityFor pins the pure decision table (P-0183 G1 contract: the tier-1
// marker is the ONLY verdict input; the token rescues any MARKER-LESS review; a tier-1
// veto is never token-overridable).
func TestStampAuthorityFor(t *testing.T) {
	marker := parseVerdictResult{Verdict: "approved", Tier: verdictTierMarker, Confident: true}
	markerVeto := parseVerdictResult{Verdict: "changes-requested", Tier: verdictTierMarker, Confident: true}
	prose := parseVerdictResult{Verdict: "approved", Tier: verdictTierProse, Confident: false}
	reject := parseVerdictResult{Verdict: "changes-requested", Tier: verdictTierProse, Confident: true}
	unclear := parseVerdictResult{Verdict: "unclear"}

	cases := []struct {
		name  string
		vr    parseVerdictResult
		token bool
		want  stampAuthority
	}{
		{"tier-1 marker approval self-authorizes", marker, false, stampByMarker},
		{"tier-1 marker approval ignores a token (does not need one)", marker, true, stampByMarker},
		{"tier-1 marker VETO stands — token cannot override it", markerVeto, true, stampNone},
		{"tier-1 marker veto without token writes nothing", markerVeto, false, stampNone},
		{"tier-2 prose approval WITHOUT token writes nothing", prose, false, stampNone},
		{"tier-2 prose approval WITH token writes on operator authority", prose, true, stampByToken},
		{"tier-2 prose rejection WITHOUT token writes nothing", reject, false, stampNone},
		{"tier-2 prose rejection is ADVISORY — token still rescues (false-veto fix)", reject, true, stampByToken},
		{"unclear WITHOUT token writes nothing", unclear, false, stampNone},
		{"unclear WITH token writes on operator authority", unclear, true, stampByToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stampAuthorityFor(tc.vr, tc.token); got != tc.want {
				t.Errorf("stampAuthorityFor = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestFalseVeto_PR294_TokenRescues is the P-0183 G1 regression fixture: PR #294's
// exact body shape false-matches bare "blocking" in the prose classifier — and used
// to return stampNone BEFORE consulting the token, destroying the operator's only
// escape hatch for a marker-less review.
func TestFalseVeto_PR294_TokenRescues(t *testing.T) {
	body := "### Previously blocking - cursor pagination tiebreak: fixed\nAll previously-blocking issues are resolved.\n"
	vr := parseVerdict(body)
	// The DISPLAYED verdict is fixed too (resolution exemptions): this body is now an
	// approval-shaped report, not a veto. Either way, stamp authority must not depend
	// on the prose guess:
	if vr.Tier == verdictTierMarker {
		t.Fatalf("fixture must be marker-less; got tier=%q", vr.Tier)
	}
	if got := stampAuthorityFor(vr, true); got != stampByToken {
		t.Errorf("stampAuthorityFor(PR294 body, token) = %q; want %q — the token must rescue a marker-less review", got, stampByToken)
	}
	if got := stampAuthorityFor(vr, false); got == stampByMarker {
		t.Errorf("marker-less body must never yield stampByMarker")
	}
}

// TestResolutionVocabulary_DisplayVerdict pins the advisory-display fix: strictly
// past-tense/resolution phrasing no longer reads as changes-requested, while a body
// carrying BOTH a resolved old blocker and a genuine new one still reports
// changes-requested (an exemption suppresses only the occurrence it spans).
func TestResolutionVocabulary_DisplayVerdict(t *testing.T) {
	resolved := "### Previously blocking - cursor pagination tiebreak: fixed\nAll previously-blocking issues are resolved.\n\n**Mergeable.**"
	vr := parseVerdict(resolved)
	if vr.Verdict != "approved" {
		t.Errorf("resolved-blockers body: displayed verdict = %q (matched=%q); want approved", vr.Verdict, vr.MatchedPhrase)
	}

	mixed := "Previously blocking — pagination: fixed.\n\nNew blocking issue: nil deref in auth middleware.\n"
	vr = parseVerdict(mixed)
	if vr.Verdict != "changes-requested" {
		t.Errorf("mixed body with a GENUINE new blocker: displayed verdict = %q; want changes-requested", vr.Verdict)
	}

	// Present-tense forms must still veto (no fail-open creep).
	present := "**Blocking.** The nil deref needs to be fixed."
	vr = parseVerdict(present)
	if vr.Verdict != "changes-requested" {
		t.Errorf("present-tense blocker: displayed verdict = %q; want changes-requested", vr.Verdict)
	}
}

// TestMarkerMutation_FlippedValueNeverStamps is the mutation test the G1 risk section
// demands: flip the marker value and confirm no stamp authority without a token, and
// no marker authority ever.
func TestMarkerMutation_FlippedValueNeverStamps(t *testing.T) {
	approved := "Looks solid overall.\n\nBRAVROS-VERDICT: approved\n"
	flipped := "Looks solid overall.\n\nBRAVROS-VERDICT: changes-requested\n"

	vrA := parseVerdict(approved)
	if got := stampAuthorityFor(vrA, false); got != stampByMarker {
		t.Fatalf("control: marker approval must stamp; got %q", got)
	}

	vrF := parseVerdict(flipped)
	if vrF.Tier != verdictTierMarker || vrF.Verdict != "changes-requested" {
		t.Fatalf("mutation fixture must parse as tier-1 changes-requested; got tier=%q verdict=%q", vrF.Tier, vrF.Verdict)
	}
	if got := stampAuthorityFor(vrF, false); got != stampNone {
		t.Errorf("MUTATION SURVIVED: flipped marker yields %q; want %q", got, stampNone)
	}
	if got := stampAuthorityFor(vrF, true); got != stampNone {
		t.Errorf("MUTATION SURVIVED: flipped marker + token yields %q; want %q — a tier-1 veto is not token-overridable", got, stampNone)
	}
}

// ---------------------------------------------------------------------------
// The token gate
// ---------------------------------------------------------------------------

// TestReviewStampGate_MintRefusedInsideClaude is the gate's reason for existing: the
// session that wants the merge must not be able to authorize it. We assert the
// refusal PREDICATE that mintGateToken branches on (isInsideClaudeEnv) — the command
// itself calls os.Exit(1), which a unit test cannot survive.
func TestReviewStampGate_MintRefusedInsideClaude(t *testing.T) {
	withReviewStampHome(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "test-session-abc123")

	if !isInsideClaudeEnv() {
		t.Fatal("CLAUDE_CODE_SESSION_ID must mark the process as inside Claude Code — the mint refusal depends on it")
	}

	// The refusal messaging must name this gate's verb, not promote's.
	if reviewStampGate.RefuseMsg == "" {
		t.Error("gate must carry a RefuseMsg")
	}
	if reviewStampGate.UnlockHelp == "" {
		t.Error("gate must carry UnlockHelp explaining the separate-terminal mint")
	}

	// And nothing may have been minted as a side effect.
	if reviewStampGate.Valid() {
		t.Fatal("no token may exist inside a Claude Code session unless the operator minted it out-of-band")
	}
}

// TestReviewStampGate_LegacySessionVarAlsoRefuses pins the legacy fallback env var.
func TestReviewStampGate_LegacySessionVarAlsoRefuses(t *testing.T) {
	withReviewStampHome(t)
	t.Setenv("CLAUDE_SESSION_ID", "legacy-session")
	if !isInsideClaudeEnv() {
		t.Fatal("legacy CLAUDE_SESSION_ID must also mark the process as inside Claude Code")
	}
}

// TestReviewStampGate_StatusReportsPresence covers `bravros pr-review status`
// (--field present), which is how a skill asks "did the operator authorize this?".
func TestReviewStampGate_StatusReportsPresence(t *testing.T) {
	withReviewStampHome(t)

	if reviewStampGate.Present() {
		t.Error("no token minted yet — Present() must be false")
	}

	mintTestReviewStampToken(t, 5*time.Minute)

	if !reviewStampGate.Present() {
		t.Error("after mint — Present() must be true")
	}
	tok := reviewStampGate.Read()
	if tok == nil {
		t.Fatal("token must be readable after mint")
	}
	if !tok.SingleUse {
		t.Error("the review-stamp token must be marked single-use")
	}
	if tok.TTLMinutes != 5 {
		t.Errorf("ttl_minutes = %d; want 5 (short TTL)", tok.TTLMinutes)
	}
	if tok.Expired() {
		t.Error("a freshly minted 5-minute token must not be expired")
	}
}

// TestReviewStampGate_ExpiredTokenDoesNotAuthorize pins the TTL.
func TestReviewStampGate_ExpiredTokenDoesNotAuthorize(t *testing.T) {
	home := withReviewStampHome(t)

	// Write an already-expired token by hand.
	stateDir := filepath.Join(home, ".claude", "state")
	os.MkdirAll(stateDir, 0o700)
	past := time.Now().UTC().Add(-10 * time.Minute)
	expired := token.Token{
		CreatedAt:  past,
		ExpiresAt:  past.Add(5 * time.Minute), // expired 5 minutes ago
		TTLMinutes: 5,
		SingleUse:  true,
	}
	data := `{"created_at":"` + expired.CreatedAt.Format(time.RFC3339) +
		`","expires_at":"` + expired.ExpiresAt.Format(time.RFC3339) +
		`","ttl_minutes":5,"tty":"","single_use":true}`
	if err := os.WriteFile(reviewStampGate.Path(), []byte(data), 0o600); err != nil {
		t.Fatalf("write expired token: %v", err)
	}

	if reviewStampGate.Valid() {
		t.Error("an expired token must not be valid")
	}
	if consumeReviewStampToken() {
		t.Fatal("CATASTROPHIC: an expired token authorized a stamp")
	}
}

// TestConsumeReviewStampToken_SingleUse pins the single-use invariant at the
// consume-helper level: the first consume succeeds and DELETES the token; the second
// finds nothing.
func TestConsumeReviewStampToken_SingleUse(t *testing.T) {
	withReviewStampHome(t)
	mintTestReviewStampToken(t, 5*time.Minute)

	if !consumeReviewStampToken() {
		t.Fatal("first consume of a valid token must succeed")
	}
	if _, err := os.Stat(reviewStampGate.Path()); err == nil {
		t.Fatal("token file must be DELETED by the consume — single-use is not optional")
	}
	if consumeReviewStampToken() {
		t.Fatal("CATASTROPHIC: a consumed token authorized a second stamp")
	}
}

// ---------------------------------------------------------------------------
// End-to-end: the escape hatch, and its single-use bound
// ---------------------------------------------------------------------------

// TestWriteStampFromVerdict_Tier2ProseNeverStampsWithoutToken is the safe no-op: a
// prose approval, no token → nothing written, exit 0.
func TestWriteStampFromVerdict_Tier2ProseNeverStampsWithoutToken(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)
	os.Chdir(dir)
	defer os.Chdir(orig)
	withNoReviewStampToken(t)

	// The real @claude sign-off shape — a GENUINE approval that we still refuse to
	// act on, because we cannot tell it apart from the conditional ones.
	body := "### Verdict\n\n**Mergeable.** The fix is correct and all targeted tests pass."

	if code := writeStampFromVerdict("777", body); code != 0 {
		t.Errorf("expected exit 0 (safe no-op), got %d", code)
	}
	if _, err := os.Stat(filepath.Join(planningDir, ".review-stamp-777.json")); err == nil {
		t.Fatal("a tier-2 prose approval must NOT write a stamp without an operator token")
	}
}

// TestWriteStampFromVerdict_Tier2PlusTokenStampsAndConsumes is the escape hatch,
// end-to-end — and its bound. The operator minted a token out-of-band, so the SAME
// prose body that wrote nothing above now stamps; and the token is consumed, so a
// SECOND PR cannot ride the same authorization.
func TestWriteStampFromVerdict_Tier2PlusTokenStampsAndConsumes(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)
	os.Chdir(dir)
	defer os.Chdir(orig)

	withReviewStampHome(t)
	mintTestReviewStampToken(t, 5*time.Minute)

	body := "### Verdict\n\n**Mergeable.** The fix is correct and all targeted tests pass."

	// (1) With a valid token, the tier-2 approval DOES stamp.
	if code := writeStampFromVerdict("555", body); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(planningDir, ".review-stamp-555.json")); err != nil {
		t.Fatalf("expected a stamp written on the operator's token authority: %v", err)
	}

	// (2) The token was CONSUMED by that stamp.
	if _, err := os.Stat(reviewStampGate.Path()); err == nil {
		t.Fatal("the token must be consumed by the stamp it authorized")
	}
	if reviewStampGate.Valid() {
		t.Fatal("consumed token still reads as valid")
	}

	// (3) A SECOND stamp attempt — a different PR, same session — writes NOTHING.
	// This is the bound that makes the escape hatch safe inside an autonomous batch:
	// one out-of-band approval buys exactly one merge.
	if code := writeStampFromVerdict("556", body); code != 0 {
		t.Errorf("second attempt: expected exit 0 (safe no-op), got %d", code)
	}
	if _, err := os.Stat(filepath.Join(planningDir, ".review-stamp-556.json")); err == nil {
		t.Fatal("CATASTROPHIC: a consumed token authorized a second, unreviewed stamp")
	}
}

// TestWriteStampFromVerdict_TokenRescuesMarkerlessRejection pins the P-0183 G1
// inversion of the old "token does not rescue a rejection" contract: for a
// MARKER-LESS review the prose verdict is advisory only, so the operator's
// out-of-band token authorizes the stamp regardless of what the classifier guessed
// (that is the whole false-veto fix — a prose misread must not strip the escape
// hatch). The token IS spent: it authorized this stamp.
func TestWriteStampFromVerdict_TokenRescuesMarkerlessRejection(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)
	os.Chdir(dir)
	defer os.Chdir(orig)

	withReviewStampHome(t)
	mintTestReviewStampToken(t, 5*time.Minute)

	body := "**Blocking.** The nil deref on line 42 crashes production."

	if code := writeStampFromVerdict("666", body); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(planningDir, ".review-stamp-666.json")); err != nil {
		t.Fatal("token must rescue a MARKER-LESS review on operator authority — no stamp written")
	}
	if reviewStampGate.Valid() {
		t.Error("the token must be consumed by the stamp it authorized")
	}
}

// TestWriteStampFromVerdict_TokenCannotOverrideTier1Veto is the boundary of the
// escape hatch: an EXPLICIT tier-1 BRAVROS-VERDICT: changes-requested stands, token
// or not. The token survives — it was never spent.
func TestWriteStampFromVerdict_TokenCannotOverrideTier1Veto(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)
	os.Chdir(dir)
	defer os.Chdir(orig)

	withReviewStampHome(t)
	mintTestReviewStampToken(t, 5*time.Minute)

	body := "The nil deref on line 42 crashes production.\n\nBRAVROS-VERDICT: changes-requested\n"

	if code := writeStampFromVerdict("667", body); code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(planningDir, ".review-stamp-667.json")); err == nil {
		t.Fatal("CATASTROPHIC: a token overrode an explicit tier-1 veto")
	}
	if !reviewStampGate.Valid() {
		t.Error("a token must not be consumed by a stamp that was never written")
	}
}

// TestPrReviewSubcommandsRegistered pins the three verbs onto `bravros pr-review`.
func TestPrReviewSubcommandsRegistered(t *testing.T) {
	want := map[string]bool{"unlock": false, "status": false, "revoke": false}
	for _, c := range prReviewCmd.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("bravros pr-review %s is not registered", name)
		}
	}
	// --field must exist on status (skills call `--field present`).
	if f := prReviewStatusCmd.Flags().Lookup("field"); f == nil {
		t.Error("pr-review status must support --field (skills call --field present)")
	}
	// --ttl on unlock, mirroring promote / verify-suite.
	if f := prReviewUnlockCmd.Flags().Lookup("ttl"); f == nil {
		t.Error("pr-review unlock must support --ttl")
	} else if f.DefValue != "5" {
		t.Errorf("default TTL = %s; want 5 (short TTL)", f.DefValue)
	}
}

// TestReviewStampGate_PathContract pins the on-disk path. Skills and future audit
// rules may read this file directly, exactly as Rules 17b/47 read the promote and
// verify-suite tokens — so the path is a contract, not an implementation detail.
func TestReviewStampGate_PathContract(t *testing.T) {
	home := withReviewStampHome(t)
	want := filepath.Join(home, ".claude", "state", "review-stamp-token")
	if got := reviewStampGate.Path(); got != want {
		t.Errorf("token path = %q; want %q", got, want)
	}
	if reviewStampGate.Name != "review-stamp" {
		t.Errorf("gate name = %q; want review-stamp", reviewStampGate.Name)
	}
}
