# hotfix

INTENT: ship an urgent production fix now, bypassing the plan workflow. Flow: commit → push/merge into homolog → PR homolog→main → merge → sync back. `$ARGUMENTS` is the description — ask if empty.

## Hard constraints

- **Running `/hotfix` IS the approval for merge-to-main** — the emergency-path exemption: no `ask_question` checkpoints between commit and merge. The exemption covers approval only, never the gates below.
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

1. Refuse on `main`/`master`. Strip a leading issue ref (`#42` / `issue 42`) from `$ARGUMENTS` for the description; carry it as `Closes #42` in the PR body.
2. Run the stack's formatter on the changed files, then `bravros commit "🩹 hotfix: <description>" <changed files only>` — review the file list first and drop anything unrelated to the hotfix.
3. `git push`; if not on `homolog`: merge the branch into homolog (`🔀 merge: <branch> into homolog (hotfix)`) and push it. Then `gh pr create --base main --head homolog --title "🩹 hotfix: <description>"` with what/why, the `origin/main..homolog` commit list, and a verification checklist.
4. autopr gate (constraint above) → `gh pr merge "$PR_NUMBER" --merge` → verify state.
5. Sync homolog from main so the next homolog→main merge has no conflicts: `git checkout homolog && git pull && git fetch origin main`, then `git merge --ff-only origin/main` falling back to `--no-ff -m "🔀 merge: sync hotfix from main"`; push; return to the original branch.
6. **Plan closure — events model, no renames** (`.planning/CONVENTIONS.md`; the old advance verbs are retired). Resolve `P-NNNN` from `$ARGUMENTS`, else a single plan file matching the branch slug (empty-slug guard above). Found → append `completed` events for the plan and its `backlog:` B-ids, then commit + push:

```bash
TS=$(date -u +%FT%TZ)
echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"P-NNNN\",\"by\":\"agent:hotfix\"}" >> .planning/events.jsonl
bravros commit "🩹 hotfix: close P-NNNN after emergency deploy" .planning/events.jsonl
```

   No plan resolvable → skip silently.
7. Announce (100% PT-BR):

```bash
# <!-- announce-template: "Correção urgente publicada em produção. Projeto {PROJECT}." -->
bravros ha say --force "Correção urgente publicada em produção. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```
