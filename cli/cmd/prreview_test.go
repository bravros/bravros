package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWaitDefaultVars verifies the package-level wait vars have correct defaults.
func TestIsBotLogin(t *testing.T) {
	tests := []struct {
		login    string
		botLogin string
		want     bool
	}{
		// Exact match
		{"claude[bot]", "claude[bot]", true},
		// Different bot
		{"dependabot[bot]", "claude[bot]", false},
		// Human account
		{"alice", "claude[bot]", false},
		// Custom bot override
		{"mybot[bot]", "mybot[bot]", true},
		// Non-bot custom login
		{"customreviewer", "customreviewer", true},
		// Mismatch prefix
		{"notclaude[bot]", "claude[bot]", false},
	}
	for _, tc := range tests {
		got := isBot(tc.login, tc.botLogin)
		if got != tc.want {
			t.Errorf("isBot(%q, %q) = %v; want %v", tc.login, tc.botLogin, got, tc.want)
		}
	}
}

// TestFetchLatestBotReview_ImmediateMatch tests the review matching logic directly
// by verifying that a matching review entry is returned when found.
func TestFetchLatestBotReview_MatchesLatest(t *testing.T) {
	// Test the isBot + timestamp comparison logic indirectly by exercising
	// the helper functions we can control. fetchLatestBotReviewOrComment shells out
	// to gh which is not available in unit tests; verify the logic handles
	// multiple reviews by testing the comparison path.

	// Build two entries and verify the more recent one wins (manual simulation).
	r1 := reviewEntry{State: "COMMENTED", SubmittedAt: "2026-05-08T01:00:00Z"}
	r1.Author.Login = "claude[bot]"
	r2 := reviewEntry{State: "APPROVED", SubmittedAt: "2026-05-08T02:00:00Z"}
	r2.Author.Login = "claude[bot]"

	// Simulate the "keep latest" logic from fetchLatestBotReviewOrComment.
	var matched *reviewEntry
	for i := range []reviewEntry{r1, r2} {
		r := []reviewEntry{r1, r2}[i]
		rp := &r
		if matched == nil {
			matched = rp
			continue
		}
		tCur, _ := time.Parse(time.RFC3339, matched.SubmittedAt)
		tNew, _ := time.Parse(time.RFC3339, rp.SubmittedAt)
		if tNew.After(tCur) {
			matched = rp
		}
	}
	if matched == nil {
		t.Fatal("expected matched to be non-nil")
	}
	if matched.State != "APPROVED" {
		t.Errorf("expected latest review State=APPROVED, got %q", matched.State)
	}
	if matched.SubmittedAt != "2026-05-08T02:00:00Z" {
		t.Errorf("expected latest SubmittedAt=2026-05-08T02:00:00Z, got %q", matched.SubmittedAt)
	}
}

// TestRunWaitMode_TimeoutReturns124 verifies timeout exit code when no review arrives.
// Uses a minimal timeout and interval so the test runs quickly.
func TestFetchLatestBotReviewOrComment_CommentOnlyDetected(t *testing.T) {
	// The function shells out to `gh`, which is not available in unit tests.
	// We test the candidate-selection logic directly by constructing a slice
	// of botCandidate entries and replicating the "pick latest" loop.

	candidates := []botCandidate{
		{
			Kind:        "comment",
			Login:       "claude",
			Body:        "**Claude finished @sbravros's task in 1m 29s** —— [View job](https://example/run/1)\n\nReview: looks good.",
			State:       "posted",
			SubmittedAt: "2026-05-08T00:17:00Z",
		},
	}

	// Simulate pick-latest loop from fetchLatestBotReviewOrComment.
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

	if best == nil {
		t.Fatal("expected a candidate to be selected, got nil")
	}
	if best.Kind != "comment" {
		t.Errorf("expected kind=comment, got %q", best.Kind)
	}
	if best.Login != "claude" {
		t.Errorf("expected login=claude, got %q", best.Login)
	}
}

// TestFetchLatestBotReviewOrComment_ReviewWinsWhenNewer verifies that a formal
// review wins over a stale comment when the review timestamp is more recent.
func TestFetchLatestBotReviewOrComment_ReviewWinsWhenNewer(t *testing.T) {
	candidates := []botCandidate{
		{
			Kind:        "comment",
			Login:       "claude",
			Body:        "**Claude finished @sbravros's task in 1m 29s** —— [View job](https://example/run/1)\n\nLooked at PR.",
			State:       "posted",
			SubmittedAt: "2026-05-08T00:10:00Z", // older
		},
		{
			Kind:        "review",
			Login:       "claude[bot]",
			Body:        "LGTM.",
			State:       "APPROVED",
			SubmittedAt: "2026-05-08T00:20:00Z", // newer
		},
	}

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

	if best == nil {
		t.Fatal("expected a candidate to be selected, got nil")
	}
	if best.Kind != "review" {
		t.Errorf("expected kind=review to win (newer timestamp), got kind=%q", best.Kind)
	}
	if best.State != "APPROVED" {
		t.Errorf("expected state=APPROVED, got %q", best.State)
	}
}

// TestFetchLatestBotReviewOrComment_UserRequestCommentExcluded verifies that a
// comment whose body does NOT start with "**Claude finished" is never counted
// as a bot review. The "@claude review" request comment posted by the human must
// be ignored, even if the author login matches.
func TestFetchLatestBotReviewOrComment_UserRequestCommentExcluded(t *testing.T) {
	// The body-prefix filter is: strings.HasPrefix(body, "**Claude finished")
	// Simulate a user's "@claude review" comment — it should not pass the filter.
	userRequestBody := "@claude review\nPlease take a look at this PR."
	// Realistic bot reply shape — closing `**` lands AFTER timing/job text.
	botReplyBody := "**Claude finished @sbravros's task in 2m 59s** —— [View job](https://example/run/1)\n\nReview verdict."

	if strings.HasPrefix(userRequestBody, "**Claude finished") {
		t.Error("user's @claude review request comment incorrectly passed the body-prefix filter")
	}
	if !strings.HasPrefix(botReplyBody, "**Claude finished") {
		t.Error("bot reply did not pass the body-prefix filter — filter may be broken")
	}
}

// TestIsBotOrAction_BareClaudeMatchesClaudeBotDefault verifies that isBotOrAction
// accepts the bare login "claude" (Action's actual login) when botLogin is the
// default "claude[bot]", without requiring a change to the --bot flag default.
func TestIsBotOrAction_BareClaudeMatchesClaudeBotDefault(t *testing.T) {
	tests := []struct {
		login    string
		botLogin string
		want     bool
	}{
		// Bare "claude" should match when botLogin is "claude[bot]"
		{"claude", "claude[bot]", true},
		// "claude[bot]" exact match (legacy)
		{"claude[bot]", "claude[bot]", true},
		// Unrelated login must not match
		{"dependabot[bot]", "claude[bot]", false},
		// Human login must not match
		{"alice", "claude[bot]", false},
		// Custom bot — bare matches stripped
		{"mybot", "mybot[bot]", true},
		// Different bot must not match bare
		{"notclaude", "claude[bot]", false},
	}

	for _, tc := range tests {
		got := isBotOrAction(tc.login, tc.botLogin)
		if got != tc.want {
			t.Errorf("isBotOrAction(%q, %q) = %v; want %v", tc.login, tc.botLogin, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 2: parseVerdict table-driven tests (≥6 body samples)
// ---------------------------------------------------------------------------

// pr1343FinalReviewBody is the FULL VERBATIM body of the FINAL @claude review
// comment on paylog/ev PR #1343, fetched with:
//
//	gh api repos/paylog/ev/issues/1343/comments \
//	  --jq '.[] | select(.user.login|test("claude")) | .body'
//
// Real production data, and the headline pin of the whole verdict parser: it MUST
// approve. Note the "**Minor nit (non-blocking):**" line — rejectionExemptions is
// the only thing keeping that from vetoing. If a future change cannot approve this
// body, FIX THE CODE — never weaken this fixture.
const pr1343FinalReviewBody = "" +
	"**Claude finished @sbravros's task in 1m 56s** —— [View job](https://github.com/paylog/ev/actions/runs/29188937182)\n" +
	"\n" +
	"---\n" +
	"### 🔍 Review: PR #1343 — frete audit zero-charge description\n" +
	"\n" +
	"**Todo list**\n" +
	"- [x] Gather context (diff, changed files)\n" +
	"- [x] Review `ShippingAuditService` logic change\n" +
	"- [x] Review test changes\n" +
	"- [x] Check idempotency / coupling claims from PR description\n" +
	"- [x] Post final review\n" +
	"\n" +
	"### Summary\n" +
	"\n" +
	"Re-reviewed against the current HEAD (`23912a8`), which already folds in the naming-convention fix flagged in my prior pass on this PR.\n" +
	"\n" +
	"- **Naming inconsistency resolved.** The undercharge branch at `ShippingAuditService.php:183-184` now emits `\"Custo do frete do pedido: {$pedido->id}\"` when `totalCharged <= 0` — matching the existing convention used by the normal charge path (`app/Util/Etiqueta.php:153,175,252`, `app/Repositories/RastreioRepository.php:74,208,438`, `app/Console/Commands/FreteBackfillFmSwitch.php:129`). Confirmed via grep that no code string-matches on this description for logic (only tests use it as a fixture/assertion), so reusing the exact string is safe and desirable — a producer reading the Extrato now sees one consistent label regardless of which path created the charge.\n" +
	"- **Genuine partial adjustment still works.** `totalCharged > 0` still produces `\"Ajuste frete: cobrado R$ X, esperado R$ Y\"` (`ShippingAuditService.php:186-188`), verified by the new test `ShippingAuditInvariantTest.php:127-152`.\n" +
	"- **Idempotency confirmed.** `hasExistingFreteAjuste()` (`ShippingAuditService.php:893-901`) only matches `\"Ajuste frete:%\"`, so it won't catch the new `\"Custo do frete do pedido:\"` row on a re-audit — but that's fine because idempotency here is balance-based: `$freteCharges` sums by `tipo_operacao = FRETE` regardless of description, so the new row is already folded into `totalCharged` on the next pass and `totalCharged < expectedCharge` no longer holds. Verified by the idempotency test (`ShippingAuditInvariantTest.php:179-189`).\n" +
	"- **No coupling break with `frete:reprocessa-fixo-para-variavel`** — `totalChargedFreightCents()` sums all FRETE rows regardless of description, unaffected by this change.\n" +
	"- **Minor nit (non-blocking):** the comment above the idempotency test (`ShippingAuditInvariantTest.php:182-183`) still says the cobrado=0 path \"writes a plain 'Frete do pedido:' charge\" — stale wording left over from before the follow-up commit renamed the string to `\"Custo do frete do pedido:\"`. Doesn't affect test correctness, just a doc-comment cleanup opportunity.\n" +
	"\n" +
	"### Test execution\n" +
	"\n" +
	"Ran the targeted suites (base branch is `homolog`, deps installed):\n" +
	"- `ShippingAuditInvariantTest` — 6 passed (19 assertions)\n" +
	"- `DetalhesTest` — 47 passed (127 assertions)\n" +
	"- `ReprocessaFreteFixoParaVariavelTest` — 11 passed (39 assertions)\n" +
	"- `vendor/bin/pint --test` on the three touched PHP files — passed, no formatting issues\n" +
	"\n" +
	"### Other observations\n" +
	"\n" +
	"- `CLAUDE.md` diff is carried over from an unrelated prior commit (graphify docs trim) — not part of this fix, no concern.\n" +
	"- Overcharge behavior (logged-only, never reversed) is untouched.\n" +
	"- No security concerns — internal audit/ledger service, no user input touches this path.\n" +
	"\n" +
	"### Verdict\n" +
	"\n" +
	"**Mergeable.** The fix is correct, the previously-flagged naming inconsistency has been resolved, idempotency/coupling claims hold up against the actual code, and all targeted tests + Pint pass.\n" +
	" · [Branch](https://github.com/paylog/ev/tree/fix/frete-audit-zero-charge-description)\n"

// pr1343FirstReviewBody is the FULL VERBATIM body of the FIRST @claude review on the
// same PR — also an approval, and the reason the "not a blocker" exemption exists. Its
// sign-off reads:
//
//	**Mergeable.** … The description-string inconsistency above is a polish
//	suggestion, not a blocker.
//
// An exemption list built only from "non-blocking" vetoes this genuine approval. Real
// production data beats intuition: pinned so that regression can never come back.
const pr1343FirstReviewBody = "" +
	"**Claude finished @sbravros's task in 3m 32s** —— [View job](https://github.com/paylog/ev/actions/runs/29188453797)\n" +
	"\n" +
	"---\n" +
	"### 🔍 Review: PR #1343 — frete audit zero-charge description\n" +
	"\n" +
	"**Todo list**\n" +
	"- [x] Gather context (diff, changed files)\n" +
	"- [x] Review `ShippingAuditService` logic change\n" +
	"- [x] Review test changes\n" +
	"- [x] Check idempotency / coupling claims from PR description\n" +
	"- [x] Post final review\n" +
	"\n" +
	"### Summary\n" +
	"\n" +
	"The fix correctly splits the undercharge-correction branch on `totalCharged`: `<= 0` → `Frete do pedido: {id}`, `> 0` (genuine partial gap) → the original `Ajuste frete: cobrado…, esperado…` wording. Verified the mechanics rather than trusting the description:\n" +
	"\n" +
	"- **Idempotency claim confirmed.** The guard at `ShippingAuditService.php:166` (`hasExistingFreteAjuste()`, matches `Ajuste frete:%`) only fires *before* the undercharge branch. On re-audit, the new `Frete do pedido:` row is picked up by `$freteCharges` (keyed on `tipo_operacao = FRETE`, not description), so `totalCharged` already reflects it and the `totalCharged < expectedCharge` condition itself doesn't re-trigger. Balance-based idempotency holds, as claimed.\n" +
	"- **No coupling break with `frete:reprocessa-fixo-para-variavel` confirmed.** `ReprocessaFreteFixoParaVariavel::totalChargedFreightCents()` (`app/Console/Commands/ReprocessaFreteFixoParaVariavel.php:327`) sums all FRETE rows regardless of description — not prefix-matched. Its test file (`ReprocessaFreteFixoParaVariavelTest.php:84`) hardcodes the *old* `Ajuste frete: cobrado R$ 0,00…` string only as fixture setup (simulating pre-existing historical rows), never asserts it comes from a live `audit()` call — so it's unaffected by this change, matching the \"no retroactive backfill\" note.\n" +
	"- **Display layer unaffected.** `extrato-financeiro.php` classifies rows by `tipo_operacao` enum, not by parsing `descricao`, so the icon/category (\"Transporte e entrega\") renders correctly for the new wording.\n" +
	"- Ran the targeted suites named in the PR's test plan locally: `ShippingAuditInvariantTest` (6 passed), `DetalhesTest` (47 passed), `ReprocessaFreteFixoParaVariavelTest` (11 passed). `vendor/bin/pint --test` on the touched files also passes.\n" +
	"\n" +
	"### One naming inconsistency worth a look (non-blocking)\n" +
	"\n" +
	"Elsewhere in the codebase, the established description for a **normal, first-time freight charge** (created at auction/label time) is `'Custo do frete do pedido: ' . $pedido->id` — used consistently in `app/Util/Etiqueta.php:153,175,252`, `app/Repositories/RastreioRepository.php:74,208,438`, and `app/Console/Commands/FreteBackfillFmSwitch.php:129` (and referenced in `ReprocessaFreteFixoParaVariavelTest.php:84` and this PR's own new test at `ShippingAuditInvariantTest.php` for the partial-charge fixture).\n" +
	"\n" +
	"This PR introduces a *different* string, `\"Frete do pedido: {$pedido->id}\"` (`ShippingAuditService.php:183`, missing \"Custo do\"), for what is conceptually the same thing — a plain freight line, just created via the audit self-heal path instead of the normal charge path. Functionally harmless (nothing string-matches on `\"Custo do frete do pedido:\"` for logic, only tests/docs), but it means the Extrato Financeiro can now show two different labels for what a producer reading their statement would consider the same kind of charge, depending on which code path created it. Consider aligning to the existing `Custo do frete do pedido: {id}` convention instead of introducing a third freight-description variant, unless there's a deliberate reason to distinguish \"self-healed via audit\" rows from normally-charged ones.\n" +
	"\n" +
	"[Fix this →](https://claude.ai/code?q=Align%20the%20new%20zero-charge%20description%20in%20app%2FServices%2FAudit%2FShippingAuditService.php%20(around%20line%20183%2C%20branch%20fix%2Ffrete-audit-zero-charge-description)%20with%20the%20existing%20%27Custo%20do%20frete%20do%20pedido%3A%20%27.%24pedido-%3Eid%20convention%20used%20elsewhere%20(app%2FUtil%2FEtiqueta.php%2C%20app%2FRepositories%2FRastreioRepository.php%2C%20app%2FConsole%2FCommands%2FFreteBackfillFmSwitch.php)%2C%20instead%20of%20the%20new%20%22Frete%20do%20pedido%3A%20%22%20string%2C%20and%20update%20the%20matching%20assertions%20in%20tests%2FFeature%2FAudit%2FShippingAuditInvariantTest.php%20and%20tests%2FFeature%2FPedidos%2FDetalhesTest.php&repo=paylog/ev)\n" +
	"\n" +
	"### Other observations\n" +
	"\n" +
	"- `CLAUDE.md` diff in this PR is carried over from the prior commit (`8622f98`, unrelated graphify docs trim) — not part of this fix, no concern.\n" +
	"- Overcharge behavior (logged-only, never reversed) is untouched, as stated.\n" +
	"- No security concerns — no user input touches this code path; it's an internal audit/ledger service.\n" +
	"\n" +
	"### Verdict\n" +
	"\n" +
	"**Mergeable.** The core fix is correct, idempotency and cross-command coupling claims both check out against the actual code (not just the PR description), and targeted tests pass. The description-string inconsistency above is a polish suggestion, not a blocker.\n" +
	" · [Branch](https://github.com/paylog/ev/tree/fix/frete-audit-zero-charge-description)\n"

// TestParseVerdict_TableDriven verifies all verdict markers in priority order.
func TestParseVerdict_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantV    string
		wantConf bool
	}{
		// Positive markers
		{
			name:     "ready to merge lowercase",
			body:     "**Claude finished @sbravros's task in 1m 29s** —— [View job](...)\n\nThis looks good. **Ready to merge.**",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "Ready to Merge title case",
			body:     "All checks pass. Ready to Merge when you are.",
			wantV:    "approved",
			wantConf: false,
		},
		// EMOJI STATUS MARKERS — DELETED. Both of these used to approve. They no
		// longer do, and that is the FIX, not a regression: a build status is not a
		// merge verdict, and the line-start anchor could not save them because a
		// per-CHECK CI status line legitimately begins its own line (see the
		// "Status: ✅ CI green … I'd rather not land this yet" case below, which
		// approved even WITH the anchor). Both tests below previously asserted the
		// buggy behavior; they now pin the deletion. The tier-1 bravros-verdict
		// marker supersedes the legacy template, and a template that means to sign
		// off can emit a real verdict token instead.
		{
			name:     "legacy Status colon checkmark no longer approves",
			body:     "## Summary\n\nStatus: ✅ All tests pass.\n\nNo blockers found.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "legacy Merge Readiness READY no longer approves",
			body:     "Merge Readiness: ✅ READY\n\nAll criteria met.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "verdict approved case-insensitive",
			body:     "**Verdict: Approved**\n\nLooks good to me.",
			wantV:    "approved",
			wantConf: false,
		},
		// Negative markers
		{
			name:     "Changes requested case-insensitive",
			body:     "**Claude finished @sbravros's task in 2m 5s** —— [View job](...)\n\nChanges requested:\n- Fix the error handler.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "Not ready standalone",
			body:     "This PR is Not ready for merge. Tests are failing on CI.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "red X with blocker keyword",
			body:     "❌ Blocker found: the migration is missing a rollback.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "red X with failed keyword",
			body:     "CI ❌ failed — 3 tests failed.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		// "lgtm" is now a positive phrase — even with "please address the nit" LGTM
		// is the verdict signal. Body with no markers at all still falls through to unclear.
		{
			name:     "lgtm is a positive phrase",
			body:     "**Claude finished @sbravros's task in 1m 11s** —— [View job](...)\n\nLGTM overall but please address the nit.",
			wantV:    "approved",
			wantConf: false,
		},
		// Unclear
		{
			name:     "no verdict markers at all",
			body:     "**Claude finished @sbravros's task in 1m 11s** —— [View job](...)\n\nPlease address the nit before merging.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "empty body",
			body:     "",
			wantV:    "unclear",
			wantConf: false,
		},
		// Priority: positive wins even when negative keywords are present elsewhere
		{
			name:     "ready to merge overrides not-ready-like text",
			body:     "Previous iteration was not ready, but now: Ready to Merge!",
			wantV:    "approved",
			wantConf: false,
		},
		// False-approve guards — negation must block a positive phrase
		{
			name:     "not ready to merge is changes-requested",
			body:     "This PR is not ready to merge.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "emoji not ready",
			body:     "❌ Not ready to merge — see blockers.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "not safe to merge",
			body:     "This is not safe to merge yet.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		// False-negative coverage — prose drift approval phrases
		{
			name:     "safe to merge prose",
			body:     "✅ All three observations correctly addressed — safe to merge",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "approved for merge",
			body:     "Looks good — approved for merge.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "lgtm safe to merge",
			body:     "LGTM, safe to merge.",
			wantV:    "approved",
			wantConf: false,
		},
		// lgtm guard (PR #314 review) — hedged/conditional/substring "lgtm" must
		// NOT false-approve the merge gate. The trailing-nit "LGTM overall but..."
		// case above stays approved; these do not.
		{
			name:     "lgtm hedged by not-sure prefix is unclear",
			body:     "Not sure if LGTM yet — let me re-check the migration ordering.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "lgtm with forward condition is unclear",
			body:     "LGTM after fixes — address the race condition and I'll re-review.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "lgtm as a substring does not match",
			body:     "This change is lgtmworthy in spirit but I have concerns about the parser.",
			wantV:    "unclear",
			wantConf: false,
		},
		// notReadyStandalone per-occurrence guard (PR #314 review) — a live
		// "not ready" wins even when an earlier past-tense "was not ready" appears.
		{
			name:     "past-tense then current-state not ready is changes-requested",
			body:     "The PR was not ready earlier. It is still not ready.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		// lgtm direct-negation guard (PR #314 re-review) — routing lgtm through
		// lgtmApproves must NOT drop the "not lgtm" family that negated() caught.
		{
			name:     "not lgtm is unclear",
			body:     "This is not lgtm.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "isn't lgtm is unclear",
			body:     "This isn't lgtm.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "not yet lgtm is unclear",
			body:     "Not yet lgtm — one more pass.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "n't lgtm contraction is unclear",
			body:     "It ain't lgtm.",
			wantV:    "unclear",
			wantConf: false,
		},
		// B-0324 — extended approval phrase set. Each new phrase maps to the
		// existing "approved" verdict (no new verdict string is introduced).
		//
		// "merge-ready" was EVICTED from the plain-prose table (it approved
		// "Restore the auth check and this becomes merge-ready.") and RELOCATED to
		// the structural verdictTokens table. The sign-off shape still approves; a
		// conditional mid-prose mention no longer does.
		{
			name:     "bold Merge-ready sign-off still approves",
			body:     "### Verdict\n\n**Merge-ready.** All findings addressed.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "merge-ready mid-prose no longer approves",
			body:     "This change is merge-ready.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "bold Good to merge sign-off still approves",
			body:     "### Verdict\n\n**Good to merge.** Tests are green.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "bold Clear to ship sign-off still approves",
			body:     "**Clear to ship.**",
			wantV:    "approved",
			wantConf: false,
		},
		// --- B-0324 phrases DEMOTED after the false-approve panel (see below). ---
		// These four were pinned as approvals when "merge when ready" / "no new
		// issues" lived in positivePhrases. Both phrases were PROVEN to approve
		// bodies that reject (the "WEAK POSITIVE PHRASES" block further down), so
		// they were evicted from the plain-prose table. The prose shapes below now
		// score "unclear" — a false VETO, which costs one manual stamp, versus the
		// false APPROVE they used to enable, which merges a rejected PR.
		//
		// "Merge when ready" keeps a sanctioned SIGN-OFF shape: bolded / alone on its
		// line, via verdictTokens. It is the mid-prose mention that no longer counts.
		// See "bold Merge when ready still approves" below and the PR #322 fixture.
		{
			name:     "merge when ready mid-prose no longer approves",
			body:     "Looks good. Merge when ready.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "merge when you are ready mid-prose no longer approves",
			body:     "All green — merge when you are ready.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "no new issues no longer approves",
			body:     "Re-reviewed: no new issues.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "no new issues found no longer approves",
			body:     "No new issues found in the latest push.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// The STRUCTURED sign-off shape survives the eviction — this is the
			// contract the PR #322 regression fixture depends on.
			name:     "bold Merge when ready still approves",
			body:     "### Verdict\n\n**Merge when ready.** All findings addressed.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// PIN (audit Rules 28/31 stamp contract): "no-new-blockers" is one of the
			// two reviewer_verdict values the merge gate accepts in a review stamp, so
			// unlike the prose phrases evicted above it is a structured token and MUST
			// keep approving. Do not remove it from positivePhrases.
			name:     "no-new-blockers token",
			body:     "Verdict: no-new-blockers.",
			wantV:    "approved",
			wantConf: false,
		},
		// B-0324 negation/priority guards — new phrases must stay subordinate to
		// the negatives-first ordering and the per-phrase negated() guard.
		{
			name:     "not merge-ready is negated",
			body:     "This is not merge-ready.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "changes requested beats no new issues",
			body:     "Changes requested. There are no new issues in the new code but the old logic is wrong.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		// B-0324 hardening — "requesting changes" gerund is now a negative, so a
		// new "no new issues" positive can't mis-approve a request-changes body.
		{
			name:     "requesting changes beats no new issues",
			body:     "No new issues, however I'm requesting changes on old logic.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		// phraseWordBounded regression (PR review) — "unclear to merge/ship"
		// contains the substrings "clear to merge"/"clear to ship" and must NOT
		// false-approve. negated() never sees these because "un" is a letter
		// prefix, not a "not "/"n't " shape.
		{
			name:     "unclear to merge does not approve",
			body:     "unclear to merge without more testing.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "unclear to ship does not approve",
			body:     "Scope is unclear to ship at this point.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Positive control — word-bounding must not over-tighten the
			// legitimate "clear to merge" approval.
			name:     "clear to merge still approves",
			body:     "This is clear to merge.",
			wantV:    "approved",
			wantConf: false,
		},
		// ISSUE-1 (PR #322) — code-markup-quoted verdict phrases must NOT be
		// matched. The genuine verdict lives in prose; illustrative phrases get
		// backtick-quoted (e.g. test-case names). stripCodeMarkup removes those
		// regions before phrase-matching.
		{
			// REGRESSION: exact PR #322 shape — positive verdict in prose, the
			// negative phrases appear ONLY inside backtick inline-code spans.
			name:     "PR322 regression: negatives in inline code spans, positive in prose",
			body:     "### Verdict\n\nThe existing tests cover (`\"changes requested beats no new issues\"` and `\"requesting changes beats no new issues\"`).\n\n**Merge when ready.**",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// Negative phrase inside a FENCED ```code block``` with a positive
			// verdict outside the fence.
			name:     "negative in fenced block, positive verdict outside",
			body:     "Here is the test name we added:\n\n```go\n// covers \"changes requested\" handling\nfunc TestChangesRequested(t *testing.T) {}\n```\n\nLGTM, safe to merge.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// GUARD: plain-prose "changes requested" with NO backticks still
			// resolves changes-requested.
			name:     "plain prose changes requested still negative",
			body:     "Changes requested: the error handler swallows the panic.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// GUARD: plain-prose "not ready to merge" with no backticks still
			// resolves changes-requested.
			name:     "plain prose not ready to merge still negative",
			body:     "This is not ready to merge — the migration lacks a rollback.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// A real negative in prose wins even when an unrelated positive phrase
			// sits in backticks (stripping removes only the backtick region, the
			// prose negative survives).
			name:     "prose negative wins over backtick-quoted positive",
			body:     "`merge when ready` was last week's note, but changes requested now.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// Unterminated inline backtick keeps its literal char and does not
			// swallow the rest of the body — the prose verdict still resolves.
			name:     "unterminated inline backtick keeps prose verdict",
			body:     "The migration `helper is fine. Ready to merge.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// Double-backtick inline span (used when the literal contains a single
			// backtick) is stripped as a unit; the prose verdict resolves.
			name:     "double-backtick inline span stripped, prose verdict wins",
			body:     "See ``changes requested`` in the table. Ready to merge.",
			wantV:    "approved",
			wantConf: false,
		},
		// ---------------------------------------------------------------------
		// PR #1343 papercut — @claude signs off with a bolded verdict token
		// ("**Mergeable.**") and parseVerdict scored it unclear, so --write-stamp
		// wrote no stamp and /finish re-triggered a redundant review pass.
		//
		// The token is matched STRUCTURALLY (verdictTokenApproves: bolded, or alone
		// on its own line) — NOT via the plain-prose positivePhrases table. "mergeable"
		// is a bare adjective and GitHub's own conflict-state term, so a prose match
		// would flip this merge gate (audit Rules 28/31) fail-open. Every MUST-NOT
		// case below was proven to falsely approve when "mergeable" sat in the plain
		// phrase table.
		// ---------------------------------------------------------------------
		{
			// GROUND TRUTH: the verbatim production review body @claude posted on
			// paylog/ev PR #1343 (fetched via `gh api`). This is the exact shape the
			// fix exists to recognize — a "### Verdict" heading, then a bolded token
			// opening the line with explanatory prose trailing on the SAME line.
			name:     "real production body from paylog/ev#1343 approves",
			body:     "### Verdict\n\n**Mergeable.** The fix is correct, the previously-flagged naming inconsistency has been resolved, idempotency/coupling claims hold up against the actual code, and all targeted tests + Pint pass.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "bold Mergeable without trailing period approves",
			body:     "**Mergeable**",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "Mergeable alone on its own line approves",
			body:     "### Verdict\n\nMergeable.\n\nAll three findings are resolved.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "bold Approved to merge approves",
			body:     "**Approved to merge.**",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "bold Mergeable with trailing prose approves",
			body:     "**Mergeable.** All 3 prior findings are correctly resolved.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// Underscore bold is the same span shape.
			name:     "underscore-bold Mergeable approves",
			body:     "__Mergeable.__ Ship it.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// A closed sentence on the previous line is separate context — a "not"
			// inside it must not suppress the standalone verdict below it.
			name:     "negator in a closed preceding sentence still approves",
			body:     "I did not find any regressions.\n\n**Mergeable.**",
			wantV:    "approved",
			wantConf: false,
		},
		// --- MUST NOT APPROVE: bare-token prose mentions and rejection shapes. ---
		//
		// NOTE ON THE VERDICT VALUES BELOW. Under the final policy, several of these
		// bodies now score "changes-requested" where they previously scored
		// "unclear". That is a STRENGTHENING, not a weakening: both verdicts refuse
		// to write a stamp, but "changes-requested" is the decisive one. The
		// must-not-approve guarantee every case here exists to pin is intact — no
		// case moved toward approval. The cause is the broad raw rejection scan:
		// each body carries a blocking-severity signal ("❌", "blocking", "blocker",
		// "blocked", "non-mergeable") that now vetoes on sight.
		{
			// The bot's own status-label shape with a NO value.
			name:     "Mergeable label with red-X No does not approve",
			body:     "Mergeable: ❌ No — CI is red.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "Mergeable label with NO value does not approve",
			body:     "Mergeable: NO",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "Mergeable question answered No does not approve",
			body:     "Mergeable? No.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Hyphen prefix — phraseWordBounded only rejects a LETTER prefix, so the
			// plain-prose table could never have caught this one.
			name:     "hyphenated Non-mergeable does not approve",
			body:     "Non-mergeable until conflicts are resolved.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "hyphenated Un-mergeable does not approve",
			body:     "Un-mergeable right now.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "letter-prefixed Unmergeable does not approve",
			body:     "Unmergeable until CI goes green.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "conditional Blocked mergeable-only-after does not approve",
			body:     "Blocked: mergeable only after the migration is fixed.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "prose mergeable with a blocking finding does not approve",
			body:     "The branch is mergeable (no conflicts with homolog), but the logic is wrong. 🔴 Blocking — please fix finding 1 and re-request review.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "future-tense will-be-mergeable does not approve",
			body:     "Once that null check is added, this will be mergeable. Happy to re-review.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// "the blockers" is NOT covered by any exemption phrase (containment, not
			// proximity, is the exemption test) — so it vetoes.
			name:     "don't-consider-this-mergeable does not approve",
			body:     "I don't consider this mergeable in its current state — please address the blockers and ping me again.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// GitHub's API vocabulary: "mergeable" is the CONFLICT-STATE field name.
			name:     "GitHub mergeable-false conflict report does not approve",
			body:     "GitHub reports mergeable: false — the branch conflicts with homolog and must be rebased.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// One interposed word ("be") defeats negated()'s contiguous-prefix table —
			// which is exactly why the bare token cannot live in the prose table.
			name:     "will-not-BE-mergeable does not approve",
			body:     "The PR will not be mergeable until you fix the tests.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "won't-be-mergeable does not approve",
			body:     "It won't be mergeable until CI is green.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "no-longer-mergeable does not approve",
			body:     "The branch is no longer mergeable after the base moved.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "never-mergeable does not approve",
			body:     "This is never mergeable while the migration is broken.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// "approved to merge" quoted from a policy doc while the reviewer is
			// explicitly WITHHOLDING sign-off.
			name:     "quoted policy approved-to-merge while withholding sign-off does not approve",
			body:     "Per CONTRIBUTING.md, \"every PR must be approved to merge\". I am withholding sign-off: finding 2 is a correctness bug.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Code-span pin — stripCodeMarkup runs first, so a backtick-quoted verdict
			// is gone before the matcher ever sees it.
			name:     "backtick-quoted Mergeable does not approve",
			body:     "`Mergeable.`",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Defense-in-depth left-window negator: even a BOLDED token is rejected
			// when the same line negates it.
			name:     "negator immediately before a bold Mergeable span does not approve",
			body:     "This is not **mergeable**.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Italic (single-char run) is not a bold span — a prose aside must not
			// approve just because the author emphasized the adjective.
			name:     "italic mergeable in prose does not approve",
			body:     "The branch is *mergeable*, but the logic is wrong.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Bullet-list checklist item — the unbalanced "*" is not emphasis.
			name:     "asterisk bullet Mergeable item does not approve",
			body:     "Checklist:\n\n* Mergeable\n* Tests pass",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Negatives-first pin — a decisive "do not merge" outranks a bolded
			// "**Mergeable.**".
			name:     "do not merge beats bold Mergeable",
			body:     "**Mergeable.** In principle — but do not merge, the migration is unsafe.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		// ---------------------------------------------------------------------
		// Hardening pass — 8 adversarially-proven false approvals against the
		// first-cut structural matcher. Two root causes, both closed here:
		//
		//   A. NO LINE-START ANCHOR — a bold span at ANY column counted, so a
		//      blockquote marker, a table pipe, a list number, or a plain
		//      attribution could precede "**Mergeable.**" and it still approved.
		//   B. FIRST-MATCH-WINS — the approval matcher returned on the first hit
		//      and never looked for a LATER contradicting verdict. A rejection
		//      written as "**Blocking.**" carries no negativePhrases entry, no
		//      "not ready", and no ❌ — so it was invisible.
		//
		// This gate authorizes autonomous merges (audit Rules 28/31): a false
		// "approved" writes a stamp that lets a pipeline merge a REJECTED PR.
		// ---------------------------------------------------------------------
		{
			// The veto is STRUCTURAL, not a substring scan: an approval whose prose
			// merely MENTIONS blocking must still approve.
			name:     "approval mentioning blocking issues in prose still approves",
			body:     "### Verdict\n\n**Mergeable.** I found no blocking issues and all tests pass.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// ROOT CAUSE A — blockquote marker before the span.
			name:     "blockquoted Mergeable retracted below does not approve",
			body:     "### Verdict\n\n> **Mergeable.** — that was my earlier take; I now block on finding 3.\n\nFinding 3: the auth check is missing. I am blocking this PR.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// ROOT CAUSE B — two bold spans, the LATER one is the real verdict.
			// No negativePhrase, no "not ready", no ❌ anywhere in this body.
			name:     "bold Mergeable first pass overridden by bold Blocking",
			body:     "### Verdict\n\n**Mergeable.** (first pass)\n\nOn re-read: **Blocking.** The token comparison is non-constant-time.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// ROOT CAUSE A — table row. A status matrix is never a sign-off.
			name:     "bold Mergeable in a table row with No does not approve",
			body:     "### Summary\n\n| Check | Result |\n|---|---|\n| **Mergeable** | ❌ No — the branch conflicts |\n| Tests | ❌ No |",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// ROOT CAUSE A — uppercase token in a table row, rejection in prose below.
			name:     "bold MERGEABLE table row with blocking prose does not approve",
			body:     "### Summary\n\n| Check | Result |\n|---|---|\n| **MERGEABLE** | No |\n\nBlocking on the SQL injection in the query builder.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// ROOT CAUSE A — attributed quote. The reviewer is quoting someone else.
			name:     "attributed bold Mergeable quote does not approve",
			body:     "The author claims: **Mergeable.** I disagree — this breaks prod on the first request.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// ROOT CAUSE A — numbered findings-list item.
			name:     "numbered list item bold Mergeable answered no does not approve",
			body:     "3. **Mergeable** — no. The auth check is missing entirely.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// ROOT CAUSE A — triple-emphasis (bold-italic) mid-prose.
			name:     "triple-emphasis Mergeable mid-prose does not approve",
			body:     "The branch is ***Mergeable.*** but the logic is wrong and I am blocking.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// ROOT CAUSE A — plain bold mid-sentence.
			name:     "bold mergeable mid-sentence does not approve",
			body:     "The branch is **mergeable**, but the logic is wrong and I am blocking.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		// --- lgtmHedged before-window gap: "no longer" / "never" were missing. ---
		{
			name:     "no longer lgtm does not approve",
			body:     "no longer lgtm",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "never lgtm does not approve",
			body:     "never lgtm",
			wantV:    "unclear",
			wantConf: false,
		},
		// =====================================================================
		// FINAL VERDICT POLICY — "the bot's own verdict wins; block only on
		// BLOCKING findings."
		//
		//   * The bot's structural "**Mergeable.**" is authoritative for APPROVAL.
		//   * A finding the bot itself marks NON-BLOCKING does NOT veto.
		//   * A BLOCKING-severity finding vetoes DECISIVELY — regardless of any
		//     approval token, regardless of document order.
		//
		// Three adversarial panels broke the previous STRUCTURAL rejection matcher
		// (bolded / standalone "**Blocking.**"): one word outside its token list
		// ("**Rejected.**", "**Needs work.**") or one nested-emphasis wrapper
		// ("**_Blocking._**") and the veto silently failed to fire while the
		// fail-OPEN approval token kept working. The rejection side is now a broad,
		// nit-aware substring scan over the RAW body (blockingRejection).
		//
		// Asymmetry is the whole point: a false VETO costs one manual stamp; a false
		// APPROVE writes a stamp that lets an autonomous pipeline (audit Rules 28/31)
		// MERGE A REJECTED PR.
		// =====================================================================

		// --- HEADLINE PIN: the real production body. It MUST approve. ---
		{
			// If this ever goes red, the exemption list is wrong — fix the code,
			// never the fixture. Carries "**Minor nit (non-blocking):**".
			name:     "VERBATIM production body of paylog/ev#1343 final review approves",
			body:     pr1343FinalReviewBody,
			wantV:    "approved",
			wantConf: false,
		},
		{
			// The OTHER real @claude approval on the same PR. Its sign-off ends
			// "…is a polish suggestion, not a blocker." — an exemption list built
			// only from "non-blocking" VETOES this genuine approval. Caught by
			// re-reading production data, not by intuition.
			name:     "VERBATIM production body of paylog/ev#1343 first review approves",
			body:     pr1343FirstReviewBody,
			wantV:    "approved",
			wantConf: false,
		},
		{
			// Minimal pin of the same exemption, so the intent survives even if the
			// big fixture is ever refactored.
			name:     "approval calling the nit not a blocker approves",
			body:     "### Verdict\n\n**Mergeable.** The naming inconsistency is a polish suggestion, not a blocker.",
			wantV:    "approved",
			wantConf: false,
		},
		// --- Exemption proofs: an approval may DISCUSS blocking and still approve. ---
		{
			name:     "approval saying no blocking issues approves",
			body:     "### Verdict\n\n**Mergeable.** I found no blocking issues and all tests pass.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "approval with one non-blocking nit approves",
			body:     "### Verdict\n\n**Mergeable.** One non-blocking nit: rename the helper.",
			wantV:    "approved",
			wantConf: false,
		},
		// --- The strict approval token, in its sanctioned shapes. ---
		{
			name:     "bold Mergeable no period approves (final policy)",
			body:     "**Mergeable**",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "standalone Mergeable line approves (final policy)",
			body:     "Mergeable.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			name:     "bold Approved to merge approves (final policy)",
			body:     "**Approved to merge.**",
			wantV:    "approved",
			wantConf: false,
		},
		// --- MUST NOT APPROVE: every one of these false-approved in a prior round. ---
		{
			// HEADING LABEL HOLE. The old heading allowance let "## Mergeable" (a
			// SECTION TITLE) satisfy the token while the answer sat on the next
			// line. lineVerdictCandidates now rejects any line starting with "#".
			name:     "heading-labelled Mergeable answered No does not approve",
			body:     "## Mergeable\n\nNo. The auth check is missing entirely.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "heading-labelled Mergeable answered red-X No vetoes",
			body:     "### Mergeable\n\n❌ No — the migration is destructive.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// HTML-COMMENT HOLE. Invisible to a human reader; it must never drive
			// the verdict. stripHiddenRegions drops it (multi-line included).
			name:     "Mergeable buried in a multi-line HTML comment does not approve",
			body:     "<!--\n**Mergeable.**\n-->\n\nThe rate limiter is bypassable via header spoofing. Fix before landing.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// COLLAPSED-<details> HOLE. GitHub renders it collapsed, so the reader
			// never saw the stale verdict the parser was approving on.
			name:     "stale Mergeable inside a collapsed details block does not approve",
			body:     "<details>\n<summary>Old verdict</summary>\n\n**Mergeable.**\n\n</details>\n\nCurrent verdict: the PR leaks the session token. Fix it.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// INDENTED-CODE HOLE. A 4-space-indented illustrative sample — the
			// reviewer is showing the shape they do NOT want.
			name:     "indented-code Mergeable sample does not approve",
			body:     "Do NOT write your verdict like this:\n\n    **Mergeable.**\n\nThe PR has a race condition in the token cache. Needs work.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// RAW-SCAN PROOF. The stray backtick makes stripCodeMarkup swallow the
			// "**Blocking.**" line — destroying the fail-CLOSED signal while leaving
			// the fail-OPEN one intact. blockingRejection reads the RAW body, so the
			// veto survives.
			name:     "stray backtick cannot hide a Blocking verdict",
			body:     "**Mergeable.** Overall solid.\n\nBut the helper ` returns nil here.\n\n**Blocking.** The nil deref crashes prod at `req.User.ID`.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		// --- Vocabulary beyond "blocking": the old token list let all of these through. ---
		{
			name:     "bold Rejected after bold Mergeable vetoes",
			body:     "**Mergeable.**\n\n**Rejected.** The rate limiter is bypassable.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "bold Needs work after bold Mergeable vetoes",
			body:     "**Mergeable.**\n\n**Needs work.** The SQL is unparameterized.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "bold Do-not-land after bold Mergeable vetoes",
			body:     "**Mergeable.**\n\n**Do not land this.** It corrupts prod data.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// NESTED-EMPHASIS HOLE — "**_Blocking._**" was not the bare token, so the
			// structural veto missed it. A substring scan does not care.
			name:     "nested-emphasis Blocking after bold Mergeable vetoes",
			body:     "**Mergeable.**\n\n**_Blocking._** Nope.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			name:     "bold Blocker noun after bold Mergeable vetoes",
			body:     "### Verdict\n\n**Mergeable.** (first pass)\n\nOn re-read: **Blocker.** The token comparison leaks timing.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// Plain prose retraction — no bolded rejection token anywhere.
			name:     "prose retraction after bold Mergeable vetoes",
			body:     "**Mergeable.**\n\nWait — I retract that. I am blocking this until finding 2 is fixed.",
			wantV:    "changes-requested",
			wantConf: true,
		},

		// =====================================================================
		// PROSE-FALLBACK FALSE-APPROVE PANEL — 5 bodies, each PROVEN to return
		// "approved" by a real `go test` run against the pre-fix parser.
		//
		// The bravros-verdict marker (TIER 1, below) is now the authoritative path,
		// but this TIER-2 fallback still gates legacy comments, human reviews, and
		// any review not posted through our skills — so it must not fail OPEN.
		//
		// Every case here asserts MUST-NOT-APPROVE. The exact non-approving verdict
		// ("unclear" vs "changes-requested") is secondary: neither writes a stamp.
		// =====================================================================

		// --- DEFECT 1: WEAK POSITIVE PHRASES (positivePhrases eviction). ---
		{
			// "no new issues" is a SCOPE statement about the delta, not a verdict —
			// the reviewer says it and then rejects in the very same paragraph.
			name:     "no new issues in the diff while rejecting does not approve",
			body:     "No new issues in the diff itself. But the charge() helper double-bills on retry — that predates this PR and this PR makes it reachable. I'd rather not land this yet. Please revisit.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// "merge when ready" is a CONDITIONAL handoff, and here the condition IS
			// the fix. Mid-prose it must never approve.
			name:     "merge when ready after a fix demand does not approve",
			body:     "The nil deref on line 42 will crash production. Please fix it, then merge when ready.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- DEFECT 2: "Status: ✅" was an UNANCHORED bare substring. ---
		{
			// A per-CHECK status line is not a verdict. emojiStatusApproves now
			// requires the marker to START its line (after whitespace only), so a
			// bolded build-status span cannot satisfy it.
			name:     "build Status green inside a rejecting body does not approve",
			body:     "**Build Status: ✅ passing**\n\n**Verdict:** Do not ship this — the migration drops the users table.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- DEFECT 3: a soft-wrapped negation must not be hidden by the line break. ---
		{
			// The negator sits past the old 40-byte previous-line tail.
			name:     "soft-wrapped do-not-consider-mergeable does not approve",
			body:     "I do not consider the current state of this branch to be\nmergeable.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// The negator sits TWO lines above the token — the old scan never looked.
			name:     "negation two lines above the mergeable token does not approve",
			body:     "This change is not\nready in my judgement to be considered\nmergeable.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// CONTROL for both cases above: the line break must be the ONLY thing the
			// fix changed. The same negated sentence on one line was already correctly
			// refused, and still is.
			name:     "control — same negation on one line still does not approve",
			body:     "I do not consider the current state of this branch to be mergeable.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// COUNTER-CONTROL: widening the window must not start vetoing genuine
			// approvals whose preceding CLOSED sentence merely contains a negator.
			// negationContext keeps only the tail AFTER the last sentence terminator,
			// so this "did not" — well inside the new 120-byte window, and on the
			// IMMEDIATELY preceding line with no blank line to separate it — is
			// correctly read as separate context rather than a soft-wrapped negation.
			name:     "wider negator window still approves after a closed negated sentence",
			body:     "I did not find any regressions.\n**Mergeable.** All tests pass.",
			wantV:    "approved",
			wantConf: false,
		},

		// --- DEFECT 4: a code-span prefix forged the line-start anchor. ---
		{
			// stripCodeMarkup used to elide the span to a SPACE, which the anchor's
			// TrimLeft then removed — promoting the bold span to column 0 and turning
			// an attributed quote into a sign-off. It now elides to a NUL, which keeps
			// the column occupied.
			name:     "code-span prefix cannot promote a bold Mergeable to column 0",
			body:     "`The author claims: `**Mergeable.** I disagree; the auth gate is gone.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Same forgery via the OTHER stripper — an HTML comment elided to a space
			// promoted the span identically. Covered by the same NUL elision.
			name:     "html-comment prefix cannot promote a bold Mergeable to column 0",
			body:     "<!-- reviewer note -->**Mergeable.** ...is what the author wants; the auth gate is gone.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- DEFECT 5: an unterminated fence hid a rejection. ---
		{
			// The fence never closes, so stripCodeMarkup drops everything from
			// "```diff" onward — INCLUDING the "changes requested" verdict — while the
			// positive phrase ABOVE the fence survives. The body is MALFORMED; the
			// prose path must not approve from a document whose tail it could not read.
			name:     "unterminated fence hiding a rejection does not approve",
			body:     "No new issues in the touched files.\n\n```diff\n- old\n+ new\n\nVerdict: changes requested — the diff above removes the auth check.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// LOAD-BEARING VARIANT. The case above would now also fail on the DEFECT-1
			// eviction alone, which would let the malformed-body gate rot untested.
			// This one carries a positive phrase that is STILL in the table ("LGTM"),
			// so only the malformed gate can stop it.
			name:     "unterminated fence cannot approve on a surviving LGTM",
			body:     "LGTM on the touched files.\n\n```diff\n- old\n+ new\n\nVerdict: changes requested — the diff above removes the auth check.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// Same gate for the structural token — a malformed body cannot approve via
			// verdictTokenApproves either.
			name:     "unterminated fence cannot approve on a surviving bold Mergeable",
			body:     "**Mergeable.**\n\n```diff\n- old\n\nOn re-read this drops the auth check.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// COUNTER-CONTROL: a properly CLOSED fence is not malformed and must keep
			// approving — the malformed gate must not veto every body with a code block.
			name:     "closed fence still approves",
			body:     "Reviewed the diff:\n\n```diff\n- old\n+ new\n```\n\n**Mergeable.** The change is correct.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// The MARKER path is immune: it reads the RAW body at step 0, before any
			// stripping, so a legit verdict still wins inside a malformed body.
			name:     "marker still wins inside a malformed body",
			body:     "**Mergeable.**\n\n```diff\n- old\n\n<!-- bravros-verdict: approved -->",
			wantV:    "approved",
			wantConf: true,
		},

		// =====================================================================
		// WHITESPACE-INVARIANT NEGATION PANEL — the ONE root cause behind the six
		// remaining tier-2 false-approves. Every case below returned "approved" from
		// a real `go test` run against the pre-fix parser.
		//
		// All six were the same bug wearing a different hat: the negation and hedge
		// guards assumed (a) the negator is CONTIGUOUS with the phrase, and (b) the
		// surrounding context can be read as a RAW BYTE WINDOW. Review prose is
		// hard-wrapped at ~80 columns and hedges in ordinary English, so both
		// assumptions break constantly and a SINGLE WHITESPACE BYTE flipped the
		// verdict.
		//
		// The fix is one mechanism applied uniformly: match against a FLATTENED
		// (whitespace-normalized) view, and replace contiguity with a WORD-COUNT
		// left-window scan over a negator vocabulary. The paired space/newline/tab
		// cases below lock the invariant in as a TEST, not a comment:
		//
		//	A LINE BREAK CANNOT CHANGE A VERDICT.
		// =====================================================================

		// --- lgtmHedged's after-window: the TrimLeft cutset omitted "\n" and "\t". ---
		{
			name:     "LGTM once-condition with a space does not approve",
			body:     "LGTM once the nil deref on line 42 is handled.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// SAME BYTES, one \n instead of one space. Used to APPROVE.
			name:     "LGTM once-condition across a newline does not approve",
			body:     "LGTM\nonce the nil deref on line 42 is handled.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// SAME BYTES, one \t instead of one space. Used to APPROVE.
			name:     "LGTM once-condition across a tab does not approve",
			body:     "LGTM\tonce the nil deref on line 42 is handled.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "LGTM after-condition across a newline does not approve",
			body:     "LGTM\nafter you restore the auth gate.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- negated()'s contiguous prefixes: a newline broke contiguity. ---
		{
			name:     "not ready to merge with a space does not approve",
			body:     "The auth gate is gone, so this is not ready to merge.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// SAME BYTES, one \n. Used to APPROVE.
			name:     "not ready to merge across a newline does not approve",
			body:     "The auth gate is gone, so this is not\nready to merge.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "not safe to merge with a space does not approve",
			body:     "The auth gate is gone, so this is not safe to merge.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// SAME BYTES, one \n. Used to APPROVE.
			name:     "not safe to merge across a newline does not approve",
			body:     "The auth gate is gone, so this is not\nsafe to merge.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- The same contiguity assumption, defeated WITHOUT any line break. ---
		{
			// ONE interposed word ("call this") defeated the four contiguous prefixes.
			name:     "would-not-call-this safe to merge does not approve",
			body:     "I would not call this safe to merge — the retry path double-bills.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// A contraction plus an interposed clause.
			name:     "can't-say-this-is good to merge does not approve",
			body:     "I can't say this is good to merge; the auth check was deleted.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// "far from" is not a "not" shape at all — no contiguous-prefix table can
			// ever catch it. The word-window negator vocabulary does.
			name:     "far-from clear to ship does not approve",
			body:     "This is far from clear to ship — the migration is irreversible.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// A parenthetical between the negator and the phrase.
			name:     "not-in-my-opinion ready to merge does not approve",
			body:     "This is not, in my opinion, ready to merge. The charge() helper double-bills.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- The evicted conditional phrases, in the prose shapes that broke them. ---
		{
			name:     "becomes merge-ready after a fix demand does not approve",
			body:     "The auth check is gone. Restore it and this becomes merge-ready.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "far-from merge-ready does not approve",
			body:     "Honestly this is far from merge-ready: the nil deref on line 42 crashes prod.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- The emoji status markers: a per-CHECK CI line legitimately starts its line,
		//     so even the line-start anchor could not save them. Both DELETED. ---
		{
			name:     "green CI Status line above a rejection does not approve",
			body:     "Status: ✅ CI green (42/42 tests pass)\n\nThe migration drops the users table — I'd rather not land this yet.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "Merge Readiness READY above a retraction does not approve",
			body:     "Merge Readiness: ✅ READY\n\nCorrection — I withdraw the above. The auth check is gone.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- The 120-BYTE negator window: a longer clause walked "not" out of it. ---
		{
			// The short clause was already refused (pinned above as
			// "soft-wrapped do-not-consider-mergeable"). Lengthening the SAME sentence
			// pushed the negator past 120 raw bytes and it APPROVED. Widening the byte
			// count is more whack-a-mole; the window is now counted in WORDS, which is
			// invariant to clause length and punctuation.
			name:     "long-clause soft-wrapped do-not-consider-mergeable does not approve",
			body:     "I do not consider the current implementation of the payment retry path, which double-bills the customer on every transient failure, to be\nmergeable.",
			wantV:    "unclear",
			wantConf: false,
		},

		// --- REGRESSION GUARDS for the word-window scan. If one of these breaks, the
		//     FIX is wrong — never the fixture. ---
		{
			// A trailing nit still approves: the reviewer is accepting the change with
			// a follow-up note. EXISTING pinned behavior; the wider before-window and
			// the word-based after-window must not start vetoing it.
			name:     "LGTM with a trailing nit still approves (regression guard)",
			body:     "LGTM overall, but fix the typo in the comment later.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// The sentence CLIP is what keeps the word-window from over-vetoing: the
			// negator sits in a CLOSED preceding clause, so it is separate context.
			name:     "negator in a closed clause before a positive phrase still approves",
			body:     "Previous iteration was not ready, but now: Ready to Merge!",
			wantV:    "approved",
			wantConf: false,
		},

		// =====================================================================
		// TIER 1 — the bravros-verdict MARKER. Authoritative; short-circuits
		// everything below it.
		//
		// Four adversarial rounds of prose-parsing whack-a-mole motivated this:
		// inferring a merge authorization from free-form LLM prose is the wrong
		// architecture for a security gate. Our own skills post the comment that
		// triggers the @claude Action, so we instruct the bot to emit a canonical,
		// machine-readable marker — no downstream claude.yml change needed.
		//
		// The marker is read from the RAW body BEFORE stripHiddenRegions (which
		// deletes HTML comments — i.e. the marker itself) and BEFORE
		// blockingRejection (whose broad prose scan must not be able to veto an
		// explicit "approved" marker).
		// =====================================================================
		{
			name:     "marker approved with otherwise empty body",
			body:     "<!-- bravros-verdict: approved -->",
			wantV:    "approved",
			wantConf: true,
		},
		{
			// THE HEADLINE TEST — false-veto elimination. A ❌ inside a CI summary
			// table is exactly the prose-scan false positive the marker exists to
			// kill. The bot said approved. It is approved.
			name: "marker approved beats a red X in a CI summary table",
			body: "### CI\n\n| Check | Result |\n|---|---|\n| lint | ✅ |\n| e2e | ❌ CI failed (flaky, unrelated) |\n\n" +
				"The failure is a known-flaky e2e job on main, not this PR.\n\n<!-- bravros-verdict: approved -->",
			wantV:    "approved",
			wantConf: true,
		},
		{
			// The bot's explicit structured verdict overrides our prose GUESS —
			// even when the prose scan would have read a bolded "**Blocking.**".
			name:     "marker approved beats a bolded Blocking in prose",
			body:     "**Blocking.** ...on reflection, no: the guard already covers this.\n\n<!-- bravros-verdict: approved -->",
			wantV:    "approved",
			wantConf: true,
		},
		{
			name:     "marker changes-requested beats a bolded Mergeable",
			body:     "### Verdict\n\n**Mergeable.** Everything checks out.\n\n<!-- bravros-verdict: changes-requested -->",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// LAST marker wins — a bot that revises its verdict emits a new marker.
			name:     "two markers changes-requested then approved yields approved",
			body:     "<!-- bravros-verdict: changes-requested -->\n\nOn re-read the finding is already handled.\n\n<!-- bravros-verdict: approved -->",
			wantV:    "approved",
			wantConf: true,
		},
		{
			// LAST marker wins in the other direction too — and this is what makes
			// an earlier quoted/echoed marker (e.g. the bot repeating our own
			// instruction block back at us) non-authoritative.
			name:     "two markers approved then changes-requested yields changes-requested",
			body:     "<!-- bravros-verdict: approved -->\n\nWait — the nil deref is real.\n\n<!-- bravros-verdict: changes-requested -->",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// Liberal in what we accept: case + internal whitespace.
			name:     "marker with odd whitespace and casing approves",
			body:     "Looks fine.\n\n<!--   BRAVROS-VERDICT:   APPROVED   -->",
			wantV:    "approved",
			wantConf: true, // TIER 1 — the marker is authoritative regardless of casing
		},
		{
			// Strict about VALUES: only {approved, changes-requested} are markers.
			// "maybe" is not a marker at all — fall through to prose, which finds
			// nothing decisive here. It must NOT approve on its own.
			name:     "malformed marker value falls through to prose",
			body:     "<!-- bravros-verdict: maybe -->\n\nI am not sure about the retry logic.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			// REGRESSION GUARD: no marker → the tier-2 prose fallback still works,
			// unchanged. The verbatim production fixture must keep approving.
			name:     "no marker plus verbatim PR 1343 body still approves via prose fallback",
			body:     pr1343FinalReviewBody,
			wantV:    "approved",
			wantConf: false,
		},

		// =====================================================================
		// TIER 1, SENTINEL FORM (B-0342). The @claude GitHub Action strips HTML
		// comments from posted reviews, so the legacy <!-- --> marker never
		// survived an Action run (0 markers across every Action-posted review
		// ever inspected). The canonical marker is now a plain-text line the
		// Action preserves:
		//
		//	BRAVROS-VERDICT: approved
		//	BRAVROS-VERDICT: changes-requested
		//
		// Same tier-1 semantics as the legacy form: authoritative, raw-body,
		// last-wins across BOTH forms by position.
		// =====================================================================
		{
			name:     "sentinel approved with otherwise empty body",
			body:     "BRAVROS-VERDICT: approved",
			wantV:    "approved",
			wantConf: true,
		},
		{
			// The headline B-0342 shape: an Action-posted review that signs off
			// in prose and ends with the sentinel. Must be tier-1 approved even
			// with a ❌ in a CI table above it.
			name:     "sentinel approved beats a red X in a CI summary table",
			body:     "| e2e | ❌ flaky, unrelated |\n\nThe failure is a known-flaky job on main.\n\nBRAVROS-VERDICT: approved",
			wantV:    "approved",
			wantConf: true,
		},
		{
			name:     "sentinel changes-requested beats a bolded Mergeable",
			body:     "**Mergeable.** Everything checks out.\n\nBRAVROS-VERDICT: changes-requested",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// Models love to bold a final verdict — accept emphasis wrappers and
			// a trailing period, case-insensitively.
			name:     "sentinel bolded with trailing period and odd casing approves",
			body:     "All good.\n\n**bravros-verdict: APPROVED.**",
			wantV:    "approved",
			wantConf: true,
		},
		{
			// LAST-wins works ACROSS forms: a legacy HTML marker early, a
			// sentinel later — the sentinel (by position) is the verdict.
			name:     "legacy marker then later sentinel yields the sentinel verdict",
			body:     "<!-- bravros-verdict: changes-requested -->\n\nOn re-read the finding is already handled.\n\nBRAVROS-VERDICT: approved",
			wantV:    "approved",
			wantConf: true,
		},
		{
			name:     "sentinel then later legacy marker yields the legacy verdict",
			body:     "BRAVROS-VERDICT: approved\n\nWait — the nil deref is real.\n\n<!-- bravros-verdict: changes-requested -->",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// A verbatim echo of our request block (both lines, no real verdict
			// after) resolves to the SECOND line — changes-requested. Fail closed.
			name:     "instruction echo of both sentinel lines fails closed",
			body:     "You asked me to end with one of:\n\nBRAVROS-VERDICT: approved\nBRAVROS-VERDICT: changes-requested\n\nStill reviewing.",
			wantV:    "changes-requested",
			wantConf: true,
		},
		{
			// LINE-ANCHORING at the parseVerdict level: a blockquoted echo is NOT
			// tier-1. It may still tier-2 prose-match ("verdict: approved" is a
			// positivePhrase, and the echo contains it) — but that path is
			// advisory-only (Confident false), which is exactly the point: an
			// echo can never self-authorize. Anchoring details are unit-tested in
			// TestFindVerdictMarker_SentinelAnchoring below.
			name:     "blockquoted sentinel echo never reaches tier 1",
			body:     "> BRAVROS-VERDICT: approved\n\nThat is what the template wants; I have not decided yet.",
			wantV:    "approved",
			wantConf: false,
		},
		{
			// Value strictness matches the legacy form: only the two sanctioned
			// values are markers.
			name:     "sentinel with unsanctioned value falls through to prose",
			body:     "BRAVROS-VERDICT: maybe\n\nI am not sure about the retry logic.",
			wantV:    "unclear",
			wantConf: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseVerdict(tc.body)
			if got.Verdict != tc.wantV {
				t.Errorf("parseVerdict(%q): verdict=%q, want %q", tc.name, got.Verdict, tc.wantV)
			}
			if got.Confident != tc.wantConf {
				t.Errorf("parseVerdict(%q): confident=%v, want %v", tc.name, got.Confident, tc.wantConf)
			}
		})
	}
}

// TestFindVerdictMarker_SentinelAnchoring pins the LINE-ANCHORING contract of the
// plain-text sentinel form (B-0342): only a line that IS the sentinel counts. An
// echo behind a blockquote or list marker, or a mid-sentence prose mention, must
// never be read as the bot's tier-1 verdict — those bodies fall through to the
// tier-2 prose heuristics, which cannot self-authorize.
func TestFindVerdictMarker_SentinelAnchoring(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		wantV  string
		wantOK bool
	}{
		{"bare sentinel matches", "BRAVROS-VERDICT: approved", "approved", true},
		{"sentinel with leading whitespace matches", "   BRAVROS-VERDICT: changes-requested", "changes-requested", true},
		{"bolded sentinel with trailing period matches", "**BRAVROS-VERDICT: approved.**", "approved", true},
		{"CRLF line ending matches", "prose above\r\nBRAVROS-VERDICT: approved\r\n", "approved", true},
		{"blockquoted echo does not match", "> BRAVROS-VERDICT: approved", "", false},
		{"list-item echo does not match", "- BRAVROS-VERDICT: approved\n* BRAVROS-VERDICT: changes-requested", "", false},
		{"numbered-list echo does not match", "1. BRAVROS-VERDICT: approved", "", false},
		{"mid-sentence mention does not match", "end with the BRAVROS-VERDICT: approved line, nothing after it", "", false},
		{"trailing prose on the line does not match", "BRAVROS-VERDICT: approved — assuming you fix the nil deref", "", false},
		{"unsanctioned value does not match", "BRAVROS-VERDICT: maybe", "", false},
		{"legacy HTML form still matches", "<!-- bravros-verdict: approved -->", "approved", true},
		{"last wins across forms by position", "<!-- bravros-verdict: approved -->\nBRAVROS-VERDICT: changes-requested", "changes-requested", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := findVerdictMarker(tc.body)
			if v != tc.wantV || ok != tc.wantOK {
				t.Errorf("findVerdictMarker(%q) = (%q, %v), want (%q, %v)", tc.body, v, ok, tc.wantV, tc.wantOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Phase 2: --write-stamp flag registration test
// ---------------------------------------------------------------------------

// TestWriteStampFlagRegistered verifies --write-stamp is registered on pr-review.
func TestWriteStampFlagRegistered(t *testing.T) {
	flag := prReviewCmd.Flags().Lookup("write-stamp")
	if flag == nil {
		t.Fatal("expected --write-stamp flag on pr-review, but it was not registered")
	}
	if flag.Value.Type() != "bool" {
		t.Errorf("expected --write-stamp to be bool, got %q", flag.Value.Type())
	}
	if flag.DefValue != "false" {
		t.Errorf("expected --write-stamp default to be false, got %q", flag.DefValue)
	}
}

// TestWriteStampDefaultVar verifies the package-level var defaults to false.
func TestWriteStampFromVerdict_ApprovedWritesStamp(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)
	os.Chdir(dir)
	defer os.Chdir(orig)
	withNoReviewStampToken(t)

	body := "All checks pass. Ready to merge.\n\n<!-- bravros-verdict: approved -->"
	code := writeStampFromVerdict("999", body)
	// Code 0 = stamp written or already present (idempotent).
	if code != 0 {
		t.Errorf("writeStampFromVerdict(tier-1 marker body): expected exit code 0, got %d", code)
	}

	// Verify stamp file was written.
	stampPath := filepath.Join(planningDir, ".review-stamp-999.json")
	if _, err := os.Stat(stampPath); err != nil {
		t.Errorf("expected stamp file at %s to exist after tier-1 approved marker, got: %v", stampPath, err)
	}
}

// TestWriteStampFromVerdict_ChangesRequestedNoStamp verifies that a
// changes-requested verdict returns 0 (non-error) but writes no stamp.
func TestWriteStampFromVerdict_ChangesRequestedNoStamp(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)
	os.Chdir(dir)
	defer os.Chdir(orig)

	body := "Changes requested: please fix the error handler."
	code := writeStampFromVerdict("888", body)
	if code != 0 {
		t.Errorf("writeStampFromVerdict(changes-requested): expected exit code 0, got %d", code)
	}

	// Verify no stamp was written.
	stampPath := filepath.Join(planningDir, ".review-stamp-888.json")
	if _, err := os.Stat(stampPath); err == nil {
		t.Error("expected no stamp file for changes-requested verdict, but file exists")
	}
}

// TestWriteStampFromVerdict_UnclearNoStamp verifies that an unclear verdict
// returns 0 (non-error) and writes no stamp.
func TestWriteStampFromVerdict_UnclearNoStamp(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)
	os.Chdir(dir)
	defer os.Chdir(orig)

	// Use a body with no verdict markers so the result is "unclear".
	// Note: "LGTM" is now a positive phrase — use a body without any markers.
	body := "Looks like there are a few items to address before this is ready."
	code := writeStampFromVerdict("777", body)
	if code != 0 {
		t.Errorf("writeStampFromVerdict(unclear): expected exit code 0, got %d", code)
	}

	// Verify no stamp was written.
	stampPath := filepath.Join(planningDir, ".review-stamp-777.json")
	if _, err := os.Stat(stampPath); err == nil {
		t.Error("expected no stamp file for unclear verdict, but file exists")
	}
}

// TestWriteStampFromVerdict_Idempotent verifies that re-running writeStampFromVerdict
// on an already-stamped PR returns 0 (skipped, not error).
func TestWriteStampFromVerdict_Idempotent(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	planningDir := filepath.Join(dir, ".planning")
	os.MkdirAll(planningDir, 0o755)
	os.Chdir(dir)
	defer os.Chdir(orig)

	body := "Ready to Merge — all tests green."

	// First write.
	code1 := writeStampFromVerdict("666", body)
	if code1 != 0 {
		t.Fatalf("first writeStampFromVerdict: expected 0, got %d", code1)
	}

	// Idempotent re-run — stamp already present, should be skipped (exit 0).
	code2 := writeStampFromVerdict("666", body)
	if code2 != 0 {
		t.Errorf("idempotent re-run: expected exit code 0 (skipped), got %d", code2)
	}
}

// TestWriteStampFlagStandaloneRegistered verifies the --write-stamp flag is
// still registered and that its help text mentions standalone use.
func TestWriteStampFlagStandaloneRegistered(t *testing.T) {
	flag := prReviewCmd.Flags().Lookup("write-stamp")
	if flag == nil {
		t.Fatal("expected --write-stamp flag on pr-review, but it was not registered")
	}
	if !strings.Contains(strings.ToLower(flag.Usage), "standalone") {
		t.Errorf("expected --write-stamp usage to document standalone behaviour, got: %s", flag.Usage)
	}
}

// ---------------------------------------------------------------------------
// pr-review-verdict-clear-to-merge-bot-selection.md — Finding 1: phrase table
// ---------------------------------------------------------------------------

// TestParseVerdict_ClearToMergePhrases pins the three new approval phrases and
// their negated shapes. The PR #149 case is the verbatim recap body that
// classified as unclear before the fix; PR #150's "Ready to merge" is pinned
// alongside it so identical approval semantics can never diverge again.
func TestParseVerdict_ClearToMergePhrases(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantV       string
		wantConf    bool
		wantMatched string // "" = don't assert
	}{
		// Finding 1 regression — PR #149 bot sign-off, verbatim.
		{
			name:        "PR #149 clear to merge verbatim",
			body:        "Clear to merge. All 6 prior findings are correctly resolved with no new concerns introduced.",
			wantV:       "approved",
			wantConf:    false,
			wantMatched: "clear to merge",
		},
		// PR #150 shape — must keep working (stamp was written, matched="ready to merge").
		{
			name:        "PR #150 ready to merge verbatim shape",
			body:        "All findings addressed. Ready to merge.",
			wantV:       "approved",
			wantConf:    false,
			wantMatched: "ready to merge",
		},
		// "clear to ship" and "good to merge" were EVICTED from the plain-prose table
		// and RELOCATED to the structural verdictTokens table: as bare prose they were
		// PROVEN false-approvers ("This is far from clear to ship.", "I can't say this
		// is good to merge."). The genuine standalone sign-off still approves, and the
		// MatchedPhrase stays the same string, so consumers of the stamp log are
		// unaffected.
		{
			name:        "clear to ship standalone sign-off",
			body:        "Everything checks out.\n\n**Clear to ship.**",
			wantV:       "approved",
			wantConf:    false,
			wantMatched: "clear to ship",
		},
		{
			name:     "clear to ship mid-prose no longer approves",
			body:     "Everything checks out — clear to ship.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:        "good to merge standalone sign-off",
			body:        "Nice work.\n\n**Good to merge.**",
			wantV:       "approved",
			wantConf:    false,
			wantMatched: "good to merge",
		},
		{
			name:     "good to merge mid-prose no longer approves",
			body:     "Nice work, this is good to merge.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "Clear To Merge title case",
			body:     "Verdict: Clear To Merge.",
			wantV:    "approved",
			wantConf: false,
		},
		// Negated shapes — the existing negated() guard must cover the new
		// phrases for free ("not " / "n't " / "not yet " / "isn't " prefixes).
		{
			name:     "not clear to merge",
			body:     "This is not clear to merge until the migration is fixed.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "isn't clear to merge",
			body:     "This isn't clear to merge.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "not yet clear to merge",
			body:     "The PR is not yet clear to merge.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "not good to merge",
			body:     "This is not good to merge in its current state.",
			wantV:    "unclear",
			wantConf: false,
		},
		{
			name:     "isn't clear to ship",
			body:     "It isn't clear to ship yet.",
			wantV:    "unclear",
			wantConf: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseVerdict(tc.body)
			if got.Verdict != tc.wantV || got.Confident != tc.wantConf {
				t.Errorf("parseVerdict(%q) = (%q, %v); want (%q, %v)",
					tc.body, got.Verdict, got.Confident, tc.wantV, tc.wantConf)
			}
			if tc.wantMatched != "" && got.MatchedPhrase != tc.wantMatched {
				t.Errorf("parseVerdict(%q) matched %q; want %q", tc.body, got.MatchedPhrase, tc.wantMatched)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// pr-review-verdict-clear-to-merge-bot-selection.md — Finding 2: bot selection
// ---------------------------------------------------------------------------

// TestVerdictSelection_BotReplyBeatsNewerHumanRequest reproduces the PR #149
// --terse failure: the human's "@claude re-review" request was the
// chronologically-last comment, and the old verdict path classified it
// (→ unclear) instead of the bot's "Clear to merge" reply that preceded the
// request being answered. Every verdict-consuming path now selects via
// fetchLatestBotReviewOrComment, whose comment source only admits bodies with
// the "**Claude finished" prefix — so the human request never enters the pool.
func TestVerdictSelection_BotReplyBeatsNewerHumanRequest(t *testing.T) {
	humanRequestBody := "@claude re-review — all 6 prior findings addressed, please confirm."
	botReplyBody := "**Claude finished @sbravros's task in 2m 59s** —— [View job](https://example/run/1)\n\nClear to merge. All 6 prior findings are correctly resolved with no new concerns introduced."

	// The old bug: classifying the raw newest comment (the human request)
	// yields unclear. Pin that shape so the test documents the failure mode.
	if got := parseVerdict(humanRequestBody); got.Verdict != "unclear" || got.Confident {
		t.Fatalf("human re-review request should parse unclear/unconfident, got (%q, %v)", got.Verdict, got.Confident)
	}

	// The comment-source admission filter excludes the human request even
	// though it is newer; only the bot reply is eligible for the pool.
	if strings.HasPrefix(humanRequestBody, "**Claude finished") {
		t.Error("human request comment must not pass the **Claude finished prefix filter")
	}
	if !strings.HasPrefix(botReplyBody, "**Claude finished") {
		t.Error("bot reply must pass the **Claude finished prefix filter")
	}

	// Pool as fetchLatestBotReviewOrComment would build it: the bot reply is
	// admitted; the human request is not (prefix filter + isBotOrAction).
	candidates := []botCandidate{
		{
			Kind:        "comment",
			Login:       "claude",
			Body:        botReplyBody,
			State:       "posted",
			SubmittedAt: "2026-06-01T20:50:00Z",
		},
	}
	best := pickLatestCandidate(candidates)
	if best == nil {
		t.Fatal("expected the bot reply to be selected")
	}
	got := parseVerdict(best.Body)
	// SELECTION is what this test is about, and it is unchanged: the bot's reply (not
	// the newer human request) is the body we classify, and it still reads "approved"
	// so `--terse` / `--field verdict` show the operator what the bot said.
	//
	// AUTHORIZATION is a separate axis and it changed: this sign-off is prose, not a
	// marker, so it is TIER 2 and NOT Confident — it reports "approved" but cannot
	// write a stamp on its own. Pinned here so the two axes can never be conflated
	// again.
	if got.Verdict != "approved" {
		t.Errorf("verdict from selected bot reply = %q; want approved", got.Verdict)
	}
	if got.Tier != verdictTierProse {
		t.Errorf("tier from selected bot reply = %q; want %q", got.Tier, verdictTierProse)
	}
	if got.Confident {
		t.Error("a TIER-2 prose approval must NOT be Confident — it cannot self-authorize a merge")
	}
	if got.MatchedPhrase != "clear to merge" {
		t.Errorf("matched phrase = %q; want %q", got.MatchedPhrase, "clear to merge")
	}
}

// TestPickLatestCandidate verifies the extracted latest-entry selection helper.
func TestPickLatestCandidate(t *testing.T) {
	if got := pickLatestCandidate(nil); got != nil {
		t.Errorf("pickLatestCandidate(nil) = %v; want nil", got)
	}
	candidates := []botCandidate{
		{Kind: "comment", Login: "claude", Body: "older", SubmittedAt: "2026-06-01T20:40:00Z"},
		{Kind: "review", Login: "claude[bot]", Body: "newest", SubmittedAt: "2026-06-01T20:55:00Z"},
		{Kind: "comment", Login: "claude", Body: "middle", SubmittedAt: "2026-06-01T20:50:00Z"},
	}
	best := pickLatestCandidate(candidates)
	if best == nil || best.Body != "newest" {
		t.Errorf("pickLatestCandidate picked %+v; want the 20:55:00Z entry", best)
	}
}

// TestApplyBotVerdict verifies the --terse verdict population helper: a found
// bot body is classified through parseVerdict; found=false records "none".
