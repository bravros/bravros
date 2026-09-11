---
name: hotfix
core: true
description: Emergency hotfix deploy — commit, push homolog, PR to main, merge now. Use on `/hotfix <description>`.
---

# hotfix

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

INTENT: ship an urgent production fix now, bypassing the plan workflow. Flow: confirm repo → commit → push/merge into homolog → PR homolog→main → wait for mergeability → merge → sync back → deploy status. `$ARGUMENTS` is the description — ask if empty.

## Hard constraints

- **Running `/hotfix` IS the approval for merge-to-main** — the emergency-path exemption: no user question checkpoints between commit and merge.
- **Never route a hotfix through `/pr` → `/pr-review` → `/finish`.** If a review is wanted it is not a hotfix — say so and stop instead of drifting into "how far should /finish take it?".
- **The autopr lock is the one hard gate that remains.** Refuse if `bravros autopr status` reports lock present.
- **The police staging lane — not a token — is what lets the merge through**: head `homolog`, `mergeStateStatus` `CLEAN`, no autonomous lock, plain `gh pr merge 1234 --merge` with the **literal** PR number (no `cd … &&`, no `-R`/URL) — the hook reads raw command text, so a `$VAR` in the PR argument is unreadable → blocked. A repo with `police.staging_lane: reviewed` needs `bravros police unlock` from a separate terminal — say so ONCE, do not loop.
- **Merge-lock is intentionally skipped** — one emergency at a time.
- **NEVER delete the homolog branch after merge. NEVER skip the PR** — main is protected.
- If targeted tests fail, STOP and ask.

## Quick Flow Summary

0. **Confirm repo identity** before anything: `git remote get-url origin` + `basename "$(git rev-parse --show-toplevel)"` — a hotfix in the wrong checkout is the worst possible mistake.
1. Refuse on `main`/`master`. Strip issue ref for PR title / `Closes #42`.
2. Format files → `bravros commit "🩹 hotfix: <description>" <changed files only>`.
3. Push & merge to `homolog` → `gh pr create --base main --head homolog --title "🩹 hotfix: <description>"` (body via `--body-file` written in a previous step).
4. **Wait for mergeability**, or the first merge hits the `UNKNOWN` block: `until [ "$(gh pr view "$PR_NUMBER" --json mergeStateStatus -q .mergeStateStatus)" != "UNKNOWN" ]; do sleep 2; done`.
5. Check autopr gate → `gh pr merge 1234 --merge > /tmp/bravros-merge-1234.txt 2>&1` (substitute the literal number — a `$VAR` here is unreadable to the hook; no `cd … &&`, no `| tail`) → verify state == `MERGED`. A `✋🏽 Police Block` names its reason — relay it in one line; never `gh api`/raw HTTP, never `/promote`.
6. Sync `homolog` from `main` (`git checkout homolog && git pull && git fetch origin main && git merge ...`).
7. Close plan if applicable (`.planning/events.jsonl`) → `bravros commit`.
8. **Deploy status line, explicit**: `merged main @ <sha> · deploy: <auto-triggered / manual / none> · built+restarted: <yes / no / n-a> · verified: <how / not yet>`. Compiled or infra targets (Go binaries, containers, homelab) must say whether a rebuild + restart on the server is still required.
9. Announce via `bash ~/.agent_config/scripts/announce.sh --force "<PT-BR>" studio || true` (template in briefing).
