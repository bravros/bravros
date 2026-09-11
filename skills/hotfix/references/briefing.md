# hotfix

INTENT: ship an urgent production fix now, bypassing the plan workflow. Flow: confirm repo → commit → push/merge into homolog → PR homolog→main → wait for mergeability → merge → sync back → deploy status. `$ARGUMENTS` is the description — ask if empty.

## Hard constraints

- **Running `/hotfix` IS the approval for merge-to-main** — the emergency-path exemption: no `ask_question` checkpoints between commit and merge. The exemption covers approval only, never the gates below.
- **Never route a hotfix through `/pr` → `/pr-review` → `/finish`.** If a review is wanted it is not a hotfix — say so and stop. Observed drift: a `/hotfix` that ended in "PR #57 is approved and stamped. How far should /finish take it?" (blocksniper, 2026-09-06). The operator's own words: "ship as /hotfix which doesn't do pr and pr review, just push".
- **The police staging lane — not a token — is what lets the main merge through.** The `bravros police` hook passes `gh pr merge 1234 --merge` into `main` token-free when the head is the staging branch (`homolog`), `mergeStateStatus` is `CLEAN`, no `.planning/.auto-*-lock` exists, and the command is the plain form (no `-R`/URL/branch argument, no `cd … &&` unless the `cd` targets the exact session cwd). **The PR number on the merge line is a literal** — the hook reads the raw command text, so `"$PR_NUMBER"` or a backtick there makes the target indeterminate and the merge is blocked; each Bash call is a fresh shell anyway, so `$PR_NUMBER` stays for the non-gated `gh pr view` / `gh pr checks` lines only. Step 4's wait is what makes the `CLEAN` condition true before the first merge attempt. A repo that declares `police.staging_lane: reviewed` additionally requires an approved/stamped PR — a hotfix has none, so it needs `bravros police unlock` from a separate terminal: say so **once**, wait, never loop the merge. A `✋🏽 Police Block` names the failed condition — relay it in one line; never `gh api`/raw HTTP (B-0036), never `/promote`.
- **The autopr lock is the one hard gate that remains.** If `bravros autopr status` reports the lock present, refuse to merge — an autonomous `/auto-pr` session holds the repo and the hotfix must not punch through it. The user clears it explicitly: `bravros autopr clear-lock` from a separate terminal, then re-run `/hotfix`.
- **Merge-lock is intentionally skipped** — documented here per [`../shared/merge-flow.md`](../../shared/merge-flow.md): one emergency at a time; lock-wait latency is not acceptable in an incident. Every other part of the merge recipe still applies.
- **NEVER delete the homolog branch after merge. NEVER skip the PR** — main is protected; there is no direct-push path.
- If targeted tests fail, STOP and ask — don't push broken code, even in an emergency.

## Traps (incident-derived)

- **Pick the targeted-test slice from the graph, not the diff**: `mcp_graphify__query_graph {question: "who calls <ChangedSymbol>"}` and run the callers' tests too — a hotfix ships straight to `main` with no review loop, so the unseen caller is the whole risk. Graph empty → fall back to `grep -rl "<ChangedClassName>" tests/`.
- `git fetch origin main --quiet` BEFORE building the PR body's `origin/main..homolog` range — a stale `origin/main` is as wrong as local main (B-0338).
- Verify the merge via PR `state` == `MERGED`, never `mergeStateStatus` — the latter is a pre-merge hint, unreliable once the PR is merged.
- Plan-closure fallback matches a plan file by branch slug — **guard against an empty slug**: without the guard the glob matches ANY active plan and could close an unrelated one.

## Flow

0. **Confirm repo identity before anything else**: `git remote get-url origin` and `basename "$(git rev-parse --show-toplevel)"` — print both. Several product repos share a workspace (`paylog/{paylog,afterpay,payloglp}`), and an emergency merge into the wrong one is not recoverable by a revert alone.
1. Refuse on `main`/`master`. Strip a leading issue ref (`#42` / `issue 42`) from `$ARGUMENTS` for the description; carry it as `Closes #42` in the PR body.
2. Run the stack's formatter on the changed files, then `bravros commit "🩹 hotfix: <description>" <changed files only>` — review the file list first and drop anything unrelated to the hotfix.
3. `git push`; if not on `homolog`: merge the branch into homolog (`🔀 merge: <branch> into homolog (hotfix)`) and push it. Write the body with the Write tool to an absolute path in a **previous** step (what/why, the `origin/main..homolog` commit list, a verification checklist), then `gh pr create --base main --head homolog --title "🩹 hotfix: <description>" --body-file <abs path>` — the police hook validates the body before the command runs, so a heredoc in the same command is blocked.
4. **Wait for mergeability** — GitHub computes it asynchronously after PR creation, and the police lane refuses `UNKNOWN`:

```bash
until [ "$(gh pr view "$PR_NUMBER" --json mergeStateStatus -q .mergeStateStatus)" != "UNKNOWN" ]; do sleep 2; done
gh pr view "$PR_NUMBER" --json mergeStateStatus -q .mergeStateStatus
```

   Anything other than `CLEAN` (`BLOCKED`, `DIRTY`, `BEHIND`) → stop and report which; the lane will not open and there is nothing to retry.
5. autopr gate (constraint above) → merge with the **literal** PR number, redirected, never piped:

```bash
# substitute the literal number — a $VAR here is unreadable to the hook; no `cd … &&`, no `| tail`
gh pr merge 1234 --merge > /tmp/bravros-merge-1234.txt 2>&1
MERGE_RC=$?; cat /tmp/bravros-merge-1234.txt; echo "merge_rc=$MERGE_RC"
```

   Then verify `state` == `MERGED`. On `✋🏽 Police Block`, act on the named reason (constraint above).
6. Sync homolog from main so the next homolog→main merge has no conflicts: `git checkout homolog && git pull && git fetch origin main`, then `git merge --ff-only origin/main` falling back to `--no-ff -m "🔀 merge: sync hotfix from main"`; push; return to the original branch.
7. **Plan closure — events model, no renames** (`.planning/CONVENTIONS.md`; the old advance verbs are retired). Resolve `P-NNNN` from `$ARGUMENTS`, else a single plan file matching the branch slug (empty-slug guard above). Found → append `completed` events for the plan and its `backlog:` B-ids, then commit + push:

```bash
TS=$(date -u +%FT%TZ)
echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"P-NNNN\",\"by\":\"agent:hotfix\"}" >> .planning/events.jsonl
bravros commit "🩹 hotfix: close P-NNNN after emergency deploy" .planning/events.jsonl
```

   No plan resolvable → skip silently.
8. **Deploy status line — the operator asks "is it live?" after every hotfix, so answer first.** One explicit line:

```
merged main @ <short sha> · deploy: <auto-triggered (Forge/Cloud/Actions) | manual — not started | none configured> · built+restarted: <yes | no | n/a> · verified: <curl/log/route check | not yet>
```

   Compiled or infra targets (Go binaries, Docker images, homelab services) are **not live on merge** — say plainly whether a rebuild + restart on the server is still required and who does it. Never end on "merged" alone.
9. Announce (100% PT-BR):

```bash
# <!-- announce-template: "Correção urgente publicada em produção. Ramo <fragmento>, projeto {PROJECT}." -->
bash ~/.agent_config/scripts/announce.sh --force "Correção urgente publicada em produção. Ramo <fragmento>, projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio || true
```
