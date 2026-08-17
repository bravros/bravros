# address-pr — Detailed Briefing

INTENT: read the latest review (GitHub bot + local), fix everything, push, stamp, route the next step.

PR number: `$ARGUMENTS` if numeric, else `PR=$(gh pr view --json number -q .number)`.

## Fetch the review — two sources, never CI logs

**GitHub — latest bot review.** The @claude Action posts under login `claude`, NOT `claude[bot]` — keep BOTH filter halves. Keep `--paginate`: GitHub returns issue comments oldest-first, so a page-1-only read on a >30-comment PR returns the 30th-oldest and calls it "latest".

```bash
BODY=$(gh api --paginate "repos/{owner}/{repo}/issues/$PR/comments" --jq '[.[] | select(.user.login == "claude" or (.user.login | endswith("[bot]")))] | sort_by(.created_at) | last | .body')
VERDICT=$(printf '%s\n' "$BODY" | grep -oE '^BRAVROS-VERDICT: (approved|changes-requested)$' | tail -1)
```

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

**Round ≥ 2: drop the previous round's stamp first.** `--write-stamp` skips when a stamp file
already exists, so from the second round on it silently preserves round 1's `commit_sha` — an
approval for code that no longer exists. That stale record then blocks `/finish`, which is exactly
how PR #1919 stalled mid-merge and needed a hand-run `rm`. Deleting a stamp only ever *removes*
authority (the gate reads presence, so a missing stamp blocks and never permits), so this needs no
operator sign-off — but only ever delete one whose `commit_sha` differs from HEAD:

```bash
STAMP=".planning/.review-stamp-${PR}.json"
[ -f "$STAMP" ] && [ "$(grep -o '"commit_sha": *"[^"]*"' "$STAMP" | cut -d'"' -f4)" != "$(git rev-parse HEAD)" ] && rm -f "$STAMP"
```

Then unconditionally: `bravros pr-review "$PR" --write-stamp` — the ONE stamp authority. It writes only on a sentinel `BRAVROS-VERDICT: approved`; prose approval, `changes-requested`, or no review are safe no-ops. NEVER hand-write `.planning/.review-stamp-*` files. Prose-only approval blocked? The operator runs `bravros pr-review unlock` in a separate terminal (Claude cannot mint it) — never loop the review to force a marker.

## Route — severity matrix selects the branch (not advisory)

**⚠️ re-review** if ANY: blockers fixed (logic/security/validation) · files significantly restructured · business logic or control flow changed · test behavior modified (not just added) · security-sensitive files touched (auth, payments, permissions).
**✅ optional** only when ALL fixes were style/formatting, typos/comments, simple additions (return types, null checks), or test-only additions.

- **Autonomous:** print `STATUS: fixes-pushed. NEXT: review`, return.
- **⚠️ matched (interactive): invoke `Skill({skill: "pr-review"})` immediately — state which condition fired, do NOT ask.** Announcing the recommendation instead of acting on it is the exact failure this branch prevents. Only two skip conditions: already auto-triggered this invocation, or the stale-review gate owns the wait. "Bot already approved" / "small change" are NOT skips.
- **✅ only (interactive): ask ONCE for the whole remaining path** — the single merge-decision handoff. Only a skill that just ran its own ask_question may pass `--merge-main`/`--no-main`:
  - Merge to homolog, then main → `Skill({skill: "finish", args: "--merge-main"})` — a pre-authorization, not a guarantee: `/finish` still stops on failing CI, conflicts, or an autonomous lock. Say so in the option text.
  - Merge to homolog only → `Skill({skill: "finish", args: "--no-main"})`
  - Re-review anyway → `Skill({skill: "pr-review"})` · Done for now → stop.

```bash
bravros ha say --force "Correções da revisão $PR publicadas, próxima etapa pendente. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." >/dev/null 2>&1 || true
```
