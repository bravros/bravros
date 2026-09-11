# promote

INTENT: promote accumulated homolog work to production — token check → PR → merge → sync → revoke token. Calm-day merges only: `/hotfix` for incidents, `/finish` for feature-branch completions. Operates on `homolog` exclusively.

> **CRITICAL — authority boundary:** Claude Code CANNOT mint the promote token. If `bravros promote status --field present` is not `true`, instruct the user to run `bravros promote unlock` in a **non-Claude-Code terminal**, then re-invoke `/promote`. This is intentional — the token proves human presence at the keyboard before a main merge; the session that wants the merge must not authorize its own merge.
>
> **One token, two consumers.** The `bravros police` PreToolUse hook honours the promote token too, so the main merge needs **no second token** — never send the operator to `bravros police unlock` on top of `promote unlock`. The token is short-lived (5 minutes): do the PR and its CI wait first, and check `bravros promote status --field ttl_remaining` **immediately before** `gh pr merge`. Expired → ask for exactly one re-mint, wait, never loop the merge.

## Step 1: Pre-flight — refuse with a clear message if ANY check fails

1. On `homolog`; working tree clean (`git status --porcelain` empty); not ahead of remote.
2. No autonomous lock: `bravros autopr mode is-autonomous` exits non-zero.
3. Commits to promote exist; promote token present (missing → print the unlock instructions above, fire the PT-BR announce in [`references/close-out.md`](close-out.md) § Announces, exit 1).
4. `git fetch origin main --quiet` FIRST — a stale `origin/main` is as wrong as a lagging local main (B-0338).
5. Snapshot the PRE-merge main tip **as a git ref** — shell vars do NOT survive between Bash tool calls; a ref does, and leaves git status clean:

```bash
git update-ref refs/bravros/promote-base "$(git rev-parse origin/main)"
```

Load-bearing: `origin/main` moves the moment the promote PR merges, and Step 5 fast-forwards homolog onto it — a live `origin/main..homolog` is then empty by construction, so plan closure would walk zero commits.

## Steps 2–5

2. **PR body** (date, commit count, `--oneline` list, `Closes #N` refs) from the range `"$PROMOTE_BASE..homolog"`. Re-derive the snapshot with `git rev-parse --verify --quiet refs/bravros/promote-base` — a bare rev-parse of a missing ref echoes the LITERAL ref name to stdout, every range then silently fails, and the PR ships "Commits: 0". Missing ref → **fatal by design**: re-invoke `/promote` from Step 1. Never range against local main — it can lag arbitrarily (a 6-commit promote once reported 431 commits, B-0338).
3. **Open PR**: `gh pr create --base main --head homolog --title "🔀 promote: <date> — <n> commit(s) homolog → main"`; number via `gh pr view homolog --json number -q .number`.
4. **Merge** per [`../shared/merge-flow.md`](../../shared/merge-flow.md) — TTL check first, then lock, then the merge with the **literal** PR number (no `cd … &&`, no `| tail`). The police hook reads the raw command text, so `"$PR_NUMBER"` or a backtick on the merge line makes the target indeterminate and the merge is blocked; each Bash call is a fresh shell anyway, and the variable stays fine on the non-gated `gh pr view` line:

```bash
until [ "$(gh pr view "$PR_NUMBER" --json mergeStateStatus -q .mergeStateStatus)" != "UNKNOWN" ]; do sleep 5; done
TTL=$(bravros promote status --field ttl_remaining 2>/dev/null); echo "ttl_remaining=$TTL"
# TTL expired / token gone → ask for ONE re-mint (`bravros promote unlock`, separate terminal), wait, then continue here. Never loop.
```

```bash
bravros merge-lock acquire --timeout 60s --ttl 10m --meta reason=promote --meta pr=1234
# substitute the literal number — a $VAR here is unreadable to the hook
gh pr merge 1234 --merge > /tmp/bravros-merge-1234.txt 2>&1
MERGE_RC=$?; cat /tmp/bravros-merge-1234.txt; echo "merge_rc=$MERGE_RC"
```

   A `✋🏽 Police Block` names the failed condition (expired token, `UNKNOWN` mergeability, autonomous lock) — relay it in one line and act on that condition; never `gh api`/raw HTTP (B-0036). Verify via PR `state` == `MERGED` — `mergeStateStatus` is a pre-merge hint, unreliable after merge. Anything else → `bravros merge-lock release`, exit 1. The lock is held through the homolog push in Step 5.
5. **Sync + close-out** — full code in [`references/close-out.md`](close-out.md): fast-forward homolog from main (fall back to `--no-ff`), push, release the lock, close ONLY the plans the promoted range actually shipped (events append per `.planning/CONVENTIONS.md` — no renames, no CLI verb), delete the snapshot ref, `bravros promote revoke` (single-use consumed), PT-BR announce.

Branch pruning is manual-only — `/promote` never prunes; point the operator at `/prune-merged`.
