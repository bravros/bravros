---
name: address-pr
core: true
description: Fetch PR review comments, implement the fixes, and push.
---

# address-pr

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: read the latest review (GitHub bot + local), fix everything, push, stamp, route the next step. A round ends with a routing act, never with a report.

PR number: `$ARGUMENTS` if numeric, else `PR=$(gh pr view --json number -q .number)`. Non-numeric `$ARGUMENTS` are instructions for this round ("skip the docs nit", "also fix X"), never a PR number.

## Quick Execution Summary

1. **Fetch Review**: GitHub bot comment + local `.planning/pr-reviews/${PR}-*.md`.
2. **Classify against HEAD, then fix**: check every finding against the current tree before editing — already satisfied → skip and say so. A **non-blocking finding that names a concrete code change is a fix, not informational** (operator rule: "always fix the nits on address pr"); "informational" is reserved for findings with no code to change. Apply blockers → code issues → style → suggestions. Touch only files named in the review.
3. **Push, Verify, Stamp** — skip this whole step when no fixes were applied; HEAD has not moved, so there is nothing to ship and the stamp still keys to HEAD:
   - `/ship` with `🐛 fix: address PR #XX review feedback`
   - `gh pr checks "$PR" --watch --fail-fast > /tmp/bravros-checks-$PR.txt 2>&1` then `RC=$?` — **never pipe the gate**; `| tail` returns the pipe's status and a red build reads as success.
   - `bravros pr-review "$PR" --write-stamp` is commit-sha-keyed and safe to re-run every round: same HEAD → no-op, new HEAD → refreshes in place. No manual stamp deletion needed.
4. **Route** — the routing act is the LAST tool call of this same turn:
   - **🟢 No fixes**: ZERO code changes this round — every finding informational (no code to change), out of scope, or already satisfied -> `Skill({skill: "finish"})` **in this same turn, no args, no ask**. At most one line ("nothing actionable") before it. A gate table, "ready for /finish", or "run /finish when you want" that ends the turn is a **failed round** — the operator then types `/finish` by hand. If the tool answers that `finish` is already loaded, execute the finish flow anyway.
     Only four anomalies may interrupt the hand-off, each as ONE `ask_question` that names the anomaly (never "what's next?" / "how far should I take it?"): sentinel `changes-requested` · an actionable finding you skipped · PR base ≠ the repo's staging branch (unless `bravros config get police.direct_main` prints `true` — then `main` is the expected base) · PR stacked on / sharing a tree with another open PR or session. Anything else is 🟢.
   - **⚠️ Re-review**: blockers fixed, logic changed, test behavior modified, or security files touched -> `Skill({skill: "pr-review"})`.
   - **✅ Optional**: fixes applied and all cosmetic (style/typos/comments/simple additions/nits) -> ONE `ask_question` under a **`Main merge`** header, in `/finish` Step 7 vocabulary: *Yes — merge to homolog, then main* (`--merge-main`) · *Merge to homolog only* (`--no-main`) · *Re-review anyway* · *Not yet — accumulate*. Never the word "promote".
   - **Third review round on the same PR**: say so and offer a fresh-context `/local-review --deep` instead of a fourth loop.

Announce below only on ⚠️ / ✅. On 🟢 nothing was published and `/finish` fires its own — one announcement per event.

```bash
bash ~/.agent_config/scripts/announce.sh --force "Correções da revisão $PR publicadas, próxima etapa pendente. Ramo <fragmento>, projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio || true
```
