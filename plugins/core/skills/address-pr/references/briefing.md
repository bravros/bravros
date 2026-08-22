# address-pr — Detailed Briefing

INTENT: read the latest review (GitHub bot + local), fix everything, push, stamp, route the next step.

PR number: `$ARGUMENTS` if numeric, else `PR=$(gh pr view --json number -q .number)`.

## Fetch the review — two sources, never CI logs

**GitHub — latest bot review.** Use the CLI's own read-only verb instead of hand-rolling `gh api`
pagination — it already handles both bot-comment and formal-review sources, paginates fully, and
never writes the stamp:

```bash
bravros pr-review "$PR" --latest --json
```

`.body` carries the raw review text, `.verdict.value` the parsed sentinel (`approved` /
`changes-requested` / `unclear`), `.verdict.tier` (`marker`/`prose`/``) and `.verdict.confident`
whether it's stamp-grade. Human-readable form: `bravros pr-review "$PR" --latest` (no `--json`).

**Local** — newest `.planning/pr-reviews/${PR}-*.md` (written by `/local-review`; earlier files are historical). Frontmatter schema, merge/dedupe rules, fix priorities: `references/fetch-review-data.md`.

**Stale-review race:** if the latest review predates your last `address PR #` fix commit, the re-review is likely still running — interactive: ask (wait / force re-analyze); autonomous (`.planning/.auto-*-lock`): warn and proceed with the stale one. Both sources empty → ask ("run /pr-review or /local-review first?") and STOP.

## Fix

Apply ALL fixes without a confirmation prompt, blockers → code issues → style → suggestions.
Touch only files the review names. For each fix, grep for ALL sibling occurrences of the pattern — security fixes need every occurrence independently verified. Reviewer questions get a reply on the PR.

gh gotcha: `gh pr diff` has NO `--stat` — use `--name-only` or `gh pr view --json files,additions,deletions`.

## Push, verify, stamp

`/ship` with `🐛 fix: address PR #XX review feedback`, then wait on CI — redirected, never piped,
because `| tail` returns the pipe's status and a red build then reads as success:

```bash
gh pr checks "$PR" --watch --fail-fast > /tmp/bravros-checks-$PR.txt 2>&1
RC=$?; tail -8 /tmp/bravros-checks-$PR.txt; echo "checks_rc=$RC"
```

`bravros pr-review "$PR" --write-stamp` is the ONE stamp authority, and it is commit-sha-keyed:
same HEAD as the existing stamp → skip (no-op); different HEAD → refresh the stamp in place. Safe
to re-run every round without any manual stamp deletion first — the old "delete the stamp if its
`commit_sha` differs from HEAD" dance is obsolete. It writes only on a sentinel `BRAVROS-VERDICT:
approved`; prose approval, `changes-requested`, or no review are safe no-ops. NEVER hand-write
`.planning/.review-stamp-*` files. Prose-only approval blocked? The operator runs `bravros
pr-review unlock` in a separate terminal (Claude cannot mint it) — never loop the review to force
a marker.

## Route — severity matrix selects the branch (not advisory)

**🟢 no fixes** when the round applied ZERO code changes — every finding was informational, out of
scope, already satisfied, or a suggestion you declined *and* the decline needs no code. HEAD is
unchanged since the review, so steps 2–3 were correctly skipped: nothing to ship, nothing to push,
no CI to re-watch, and the existing stamp still keys to HEAD.
**⚠️ re-review** if ANY: blockers fixed (logic/security/validation) · files significantly restructured · business logic or control flow changed · test behavior modified (not just added) · security-sensitive files touched (auth, payments, permissions).
**✅ optional** only when fixes WERE applied and all of them were style/formatting, typos/comments, simple additions (return types, null checks), or test-only additions.

- **Autonomous:** print `STATUS: fixes-pushed. NEXT: review`, return. On 🟢 print `STATUS: no-fixes-needed. NEXT: finish` instead — the pipeline owns the hand-off, never call `/finish` yourself here.
- **🟢 matched (interactive): invoke `Skill({skill: "finish"})` immediately — do NOT ask.** State in one line that nothing was actionable, then hand off. There is nothing to re-review (no diff) and no merge question to pose that `/finish` does not already ask itself, so parking on `ask_question` only burns the stamp's lifetime. **Pass NO args** — `--merge-main`/`--no-main` are pre-authorizations reserved for a skill that just ran its own `ask_question`, and this branch deliberately ran none; bare `/finish` merges to homolog and runs its own homolog→main confirmation.
  - *Why auto:* `.planning/.review-stamp-${PR}.json` is commit-sha-keyed to HEAD. Any pull, rebase, branch switch, or hook that moves HEAD stales it and forces an entire extra review round for a PR that needed no work. Advancing straight to the merge closes that window; asking holds it open.
  - **Two hard exceptions — stop and report, do not auto-finish:** the review's sentinel says `BRAVROS-VERDICT: changes-requested` (you judged nothing actionable, the reviewer disagreed — that conflict is the operator's call), or a finding WAS actionable and you chose not to fix it (that is blocked, not clean). "Informational" is a property of the finding, never a convenience label for work you skipped.
  - Skip the step-8 announce on this branch — nothing was published, and `/finish` fires its own. One announcement per event.
- **⚠️ matched (interactive): invoke `Skill({skill: "pr-review"})` immediately — state which condition fired, do NOT ask.** Announcing the recommendation instead of acting on it is the exact failure this branch prevents. Only two skip conditions: already auto-triggered this invocation, or the stale-review gate owns the wait. "Bot already approved" / "small change" are NOT skips.
- **✅ only (interactive): ask ONCE for the whole remaining path** — the single merge-decision handoff. Only a skill that just ran its own ask_question may pass `--merge-main`/`--no-main`:
  - Merge to homolog, then main → `Skill({skill: "finish", args: "--merge-main"})` — a pre-authorization, not a guarantee: `/finish` still stops on failing CI, conflicts, or an autonomous lock. Say so in the option text.
  - Merge to homolog only → `Skill({skill: "finish", args: "--no-main"})`
  - Re-review anyway → `Skill({skill: "pr-review"})` · Done for now → stop.

```bash
bravros ha say --force "Correções da revisão $PR publicadas, próxima etapa pendente. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." >/dev/null 2>&1 || true
```
