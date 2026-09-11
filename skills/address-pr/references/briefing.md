# address-pr — Detailed Briefing

INTENT: read the latest review (GitHub bot + local), fix everything, push, stamp, route the next step.

PR number: `$ARGUMENTS` if numeric, else `PR=$(gh pr view --json number -q .number)`. Non-numeric
`$ARGUMENTS` ("skip the docs nit", "also fix X") are instructions for this round, never a PR number.

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

**Classify every finding against HEAD before editing.** A review is written against the commit it
saw; if the tree already satisfies a finding, skip it and say so — never re-apply. Then apply ALL
remaining fixes without a confirmation prompt, blockers → code issues → style → suggestions.
**A "non-blocking" finding that names a concrete code change is a fix, not informational** — apply
it (operator rule: "always fix the nits on address pr"). "Informational" is reserved for findings
with no code change to make.
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

**🟢 no fixes** when the round applied ZERO code changes — every finding was informational (no code
to change), out of scope, or already satisfied. A nit you *could* fix in one commit is ✅, not 🟢 —
fix it, then take the ✅ single-ask path. HEAD is
unchanged since the review, so steps 2–3 were correctly skipped: nothing to ship, nothing to push,
no CI to re-watch, and the existing stamp still keys to HEAD.
**⚠️ re-review** if ANY: blockers fixed (logic/security/validation) · files significantly restructured · business logic or control flow changed · test behavior modified (not just added) · security-sensitive files touched (auth, payments, permissions).
**✅ optional** only when fixes WERE applied and all of them were style/formatting, typos/comments, simple additions (return types, null checks), or test-only additions.

- **Autonomous:** print `STATUS: fixes-pushed. NEXT: review`, return. On 🟢 print `STATUS: no-fixes-needed. NEXT: finish` instead — the pipeline owns the hand-off, never call `/finish` yourself here.
- **🟢 matched (interactive): invoke `Skill({skill: "finish"})` immediately — do NOT ask.** State in one line that nothing was actionable, then hand off. There is nothing to re-review (no diff) and no merge question to pose that `/finish` does not already ask itself, so parking on `ask_question` only burns the stamp's lifetime. **Pass NO args** — `--merge-main`/`--no-main` are pre-authorizations reserved for a skill that just ran its own `ask_question`, and this branch deliberately ran none; bare `/finish` merges to homolog and runs its own homolog→main confirmation.
  - **Same turn, last tool call.** Write at most one line before it. A gate table, "ready for
    `/finish`", or "run `/finish` when you want" that ends the turn without the `Skill` call is a
    failed round — the operator then types `/finish` by hand (observed 5× in 26 no-fix rounds). The
    20-tool-call recap followed by a question is the exact pattern the operator rejected (2026-08-13:
    "instead of doing 20 tool call and then asking about merge to main"). If the tool answers that
    `finish` is already loaded, execute the finish flow anyway — the load message is not a stop.
  - *Why auto:* `.planning/.review-stamp-${PR}.json` is commit-sha-keyed to HEAD. Any pull, rebase, branch switch, or hook that moves HEAD stales it and forces an entire extra review round for a PR that needed no work. Advancing straight to the merge closes that window; asking holds it open.
  - **Only four anomalies may interrupt the hand-off**, each as ONE `ask_question` that names the anomaly — never an open-ended "what's next?" / "how far should I take it?" / "how should it land?":
    1. the review's sentinel says `BRAVROS-VERDICT: changes-requested` (you judged nothing actionable, the reviewer disagreed — the operator's call);
    2. a finding WAS actionable and you chose not to fix it (blocked, not clean — "informational" is a property of the finding, never a label for skipped work);
    3. the PR's base is not the repo's staging branch (`bravros config get staging_branch`; e.g. it targets `master`/`main` directly) — **unless `bravros config get police.direct_main` prints `true`**, in which case `main` is the expected base and this is not an anomaly (otherwise every 🟢 round in a direct-main repo becomes an ask);
    4. the PR is stacked on, or shares a working tree with, another open PR or another live session (merge order is the operator's call).
    Anything else is 🟢 → `/finish`.
  - Skip the step-8 announce on this branch — nothing was published, and `/finish` fires its own. One announcement per event.
- **⚠️ matched (interactive): invoke `Skill({skill: "pr-review"})` immediately — state which condition fired, do NOT ask.** Announcing the recommendation instead of acting on it is the exact failure this branch prevents. Only two skip conditions: already auto-triggered this invocation, or the stale-review gate owns the wait. "Bot already approved" / "small change" are NOT skips.
- **✅ only (interactive): ask ONCE for the whole remaining path** — the single merge-decision handoff, under a **`Main merge`** header and in `/finish` Step 7's exact vocabulary so the operator sees one set of words across both skills. Never the word "promote" (it sends the operator off to mint a token this path does not use). Only a skill that just ran its own ask_question may pass `--merge-main`/`--no-main`:
  - *Yes — merge to homolog, then main* → `Skill({skill: "finish", args: "--merge-main"})` — a pre-authorization, not a guarantee: `/finish` still stops on failing CI, conflicts, an autonomous lock, or a police block. Say so in the option text.
  - *Merge to homolog only* → `Skill({skill: "finish", args: "--no-main"})`
  - *Re-review anyway* → `Skill({skill: "pr-review"})` · *Not yet — accumulate* → stop.
- **Third review round on the same PR** (three `address PR #` fix commits, or three reviews fetched): say so in one line and offer a fresh-context `/local-review --deep` instead of a fourth loop — a reviewer that keeps finding new nits on the same diff is context drift, not progress.

```bash
bash ~/.agent_config/scripts/announce.sh --force "Correções da revisão $PR publicadas, próxima etapa pendente. Ramo <fragmento>, projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio || true
```
