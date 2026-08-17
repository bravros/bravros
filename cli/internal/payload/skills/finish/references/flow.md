# /finish — executable flow detail

Loaded by SKILL.md. The gates here are **mandatory**, not optional detail — load this file
before merging.

## Shell traps that have already broken a merge — read once, obey everywhere

Every one of these was observed live in a single `/finish` run (PR #1919). They cost three
rounds of improvised bash in the middle of a production merge. The recipes below are written
to avoid them — **run them as written, do not "simplify"**.

| Trap | What happens | The form to use |
|---|---|---|
| `git show "$SHA:app/..."` | zsh expands `:a` as a **parameter modifier** (absolute-path) — the `a` is eaten and cwd is prepended: `fatal: Not a valid object name /…/<sha>pp/...`. Fires on any literal path whose first letter is a modifier (`a c e h l p q r s t u x A P Q`). Braces do NOT help. | Put the path in a variable: `git cat-file blob "$SHA:$f"`. The char after `:` is then `$`, never a modifier. |
| `git rev-parse "$SHA:$f" \|\| echo ABSENT` | On a missing path `rev-parse` **echoes the argument to stdout** before failing, so the capture holds two lines and every comparison is garbage. | `git rev-parse --verify --quiet "$SHA:$f" \|\| echo ABSENT` |
| `some-cmd \| tail -5; echo "rc=$?"` | `$?` is **tail's** status, not the command's. A failing CI gate reports `rc=0`. | Never pipe a gate. `some-cmd; RC=$?` then print/inspect separately. |
| `sleep 25; check` | Bare foreground sleeps are blocked by the agent shell and the call errors out. | `until <check>; do sleep 5; done` |
| `sed "s\|^\|$f \|"` over a multi-line var | `sed: unescaped newline inside substitute pattern`. | Iterate with `while IFS= read -r f`, emit with `printf`. |

## Step 1: Resolve PR, base branch, worktree state

```bash
PR_NUMBER=$(gh pr view --json number -q .number 2>/dev/null)
[ -z "$PR_NUMBER" ] && { echo "❌ No PR for this branch"; exit 1; }

FEATURE_BRANCH=$(git branch --show-current)
if [ "$FEATURE_BRANCH" = "homolog" ]; then
    BASE_BRANCH="main";    HAS_HOMOLOG=false
elif git show-ref --verify --quiet refs/remotes/origin/homolog; then
    BASE_BRANCH="homolog"; HAS_HOMOLOG=true
else
    BASE_BRANCH="main";    HAS_HOMOLOG=false
fi

# Non-empty = another worktree holds the base branch, so `git checkout` here would fail.
BASE_HELD_AT=$(git worktree list --porcelain | awk -v b="branch refs/heads/$BASE_BRANCH" \
  '/^worktree / { wt=$2 } $0 == b && wt != ENVIRON["PWD"] { print wt }')
```

Approval check: `gh pr view --json reviewDecision -q .reviewDecision`.

### Step 1b: Stamp freshness — a stamp for an older commit is not authorization

`bravros pr-review --write-stamp` is presence-idempotent: it **skips** when a stamp file
already exists. So on a multi-round `/address-pr` the stamp still records **round 1's**
`commit_sha` while HEAD has moved on — an approval for code that no longer exists. That is not
a merge blocker to negotiate with the operator; it is a stale artifact with one sanctioned
resolution, and `/finish` performs it itself:

```bash
STAMP=".planning/.review-stamp-${PR_NUMBER}.json"
if [ -f "$STAMP" ]; then
  STAMP_SHA=$(grep -o '"commit_sha": *"[^"]*"' "$STAMP" | cut -d'"' -f4)
  HEAD_SHA=$(git rev-parse HEAD)
  if [ "$STAMP_SHA" != "$HEAD_SHA" ]; then
    echo "stale stamp: records $STAMP_SHA, HEAD is $HEAD_SHA — dropping and re-stamping from the latest verdict"
    rm -f "$STAMP"
    bravros pr-review "$PR_NUMBER" --write-stamp
  fi
fi
```

**Why deleting it is safe, and why it needs no operator sign-off.** Removing a stamp can only
ever *remove* authority — the merge gate reads presence, so a missing stamp blocks the merge and
never permits one. The re-stamp then goes through the one stamp authority, which writes only on
a fresh `BRAVROS-VERDICT: approved`; a `changes-requested` or marker-less latest review leaves
the PR correctly unstamped. Never hand-write the replacement, and never `rm` a stamp whose
`commit_sha` **equals** HEAD — that one is real authorization for exactly this code.

This is exactly how PR #1919 stalled mid-merge and needed a hand-run `rm`.

## Step 2: Close the plan (events model — no CLI verb)

Resolve the plan id — `pr_opened` event first, legacy `pr:` frontmatter second:

```bash
PLAN_ID=$(grep -h '"kind":"pr_opened"' .planning/events*.jsonl 2>/dev/null \
  | grep -E "\"pr\": ?\"?#?${PR_NUMBER}\"?[,}]" | tail -1 \
  | grep -oE '"subject":"[^"]+"' | cut -d'"' -f4)
if [ -z "$PLAN_ID" ]; then
  LEGACY=$(grep -lE "^pr: *#?${PR_NUMBER}\$" .planning/P-*.md .planning/P-*/PLAN.md .planning/P-*/README.md 2>/dev/null | head -1)
  [ -n "$LEGACY" ] && PLAN_ID=$(echo "$LEGACY" | grep -oE 'P-[0-9]+' | head -1)
fi
if [ -z "$PLAN_ID" ]; then
  # Folder-plans may carry README.md instead of PLAN.md, and /pr does not always
  # emit a pr_opened event. Last resort: the PR title/body, which /pr writes from
  # the dossier — FIRST P-NNNN mention wins, so a body that also references other
  # plans must lead with its own. Echo what was resolved so a wrong pick is visible.
  PLAN_ID=$(gh pr view "$PR_NUMBER" --json title,body -q '.title + " " + .body' \
    | grep -oE 'P-[0-9]{4}' | head -1)
  [ -n "$PLAN_ID" ] && echo "plan resolved from PR text: $PLAN_ID"
fi
```

No `PLAN_ID` → batch/aggregate PR; skip closure and carry on. Already has a `completed`
event → done; never re-close. Otherwise append (never rename the plan file — files are
identity, events are state; `.planning/CONVENTIONS.md`):

```bash
TS=$(date -u +%FT%TZ)
echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"$PLAN_ID\",\"pr\":$PR_NUMBER,\"by\":\"agent:finish\"}" >> .planning/events.jsonl
# Close the plan's backlog items too (legacy `backlog:` frontmatter list).
PLAN_FILE=$(ls .planning/${PLAN_ID}-*.md .planning/${PLAN_ID}-*/PLAN.md .planning/${PLAN_ID}-*/README.md 2>/dev/null | head -1)
BACKLOG_IDS=($(awk '/^backlog:/{flag=1;next} /^[a-z_]+:/{flag=0} flag' "$PLAN_FILE" 2>/dev/null | grep -oE 'B-[0-9]+' | sort -u))
for bid in "${BACKLOG_IDS[@]}"; do
  echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"$bid\",\"by\":\"agent:finish\"}" >> .planning/events.jsonl
done
bravros commit "📋 plan: close $PLAN_ID (PR #$PR_NUMBER)" .planning/events.jsonl
git push
```

`PLAN_NUM` (for the announce) = `$PLAN_ID` without the `P-` prefix / leading zeros.

## Step 3: CI — capture the exit code, never pipe the gate

```bash
gh pr checks "$PR_NUMBER" --watch --fail-fast > /tmp/bravros-checks-$PR_NUMBER.txt 2>&1
RC=$?
tail -8 /tmp/bravros-checks-$PR_NUMBER.txt
echo "checks_rc=$RC"
```

Redirect to a file and `tail` it **as a separate command** — `gh pr checks … | tail` throws the
real status away and reports `rc=0` on a red build. Read `RC` as:

| RC | Meaning | Action |
|---|---|---|
| `0` | every check passed | proceed |
| `1` | a check failed — **or** the repo reports no checks at all | grep the output for `no checks reported`: that is a skip, not a failure. Otherwise ask: fix and retry / merge anyway / abort |
| `8` | still pending | `--watch` should have blocked; treat as pending and re-enter the wait, never as a pass |

### Step 3b: Pre-merge readiness gate — mandatory, and it is not the same as Step 3

Step 3 proves *the checks* went green. This gate proves *GitHub agrees the PR is mergeable now* —
they diverge whenever a check was added after the watch, a required review lapsed, or the base
moved. Merging at `UNSTABLE` (PR #1922 did) means merging with a gate still running.

```bash
gh pr view "$PR_NUMBER" --json mergeable,mergeStateStatus,reviewDecision \
  -q '"mergeable=\(.mergeable) status=\(.mergeStateStatus) review=\(.reviewDecision)"'
```

| `mergeStateStatus` | Meaning | Action |
|---|---|---|
| `CLEAN` | ready | merge |
| `UNSTABLE` | mergeable but a check is pending or failed **non-required** | re-enter Step 3's watch; if it stays `UNSTABLE`, name the offending check and **ask** — never merge through it silently |
| `BLOCKED` | a required review or gate is unsatisfied | stop, report which |
| `DIRTY` | conflicts | conflict path below |
| `BEHIND` | base moved | update the branch, re-run Step 3 |
| `UNKNOWN` | GitHub is still computing mergeability (~5 min after a push/open) | one bounded wait, then decide on `mergeable` alone — **never** a poll loop |

```bash
until [ "$(gh pr view "$PR_NUMBER" --json mergeStateStatus -q .mergeStateStatus)" != "UNKNOWN" ]; do sleep 5; done
```

Bare `sleep N; <check>` is blocked by the agent shell — always the `until` form. And
`mergeable=MERGEABLE` alone is **not** the gate: PR #1922 was `MERGEABLE`+`UNSTABLE` and got
merged with its main-branch protection check still pending.

## Step 4: Merge — capture, gate, merge, verify

Pre-merge capture for the blob verification. One line per path, `"<blob-or-ABSENT> <path>"` —
`ABSENT` is the correct expectation for a file the feature **deletes or renames away**, and
comparing it against the merge commit is how that case stays a pass instead of a false alarm:

```bash
PRE_MERGE_COMMIT=$(git rev-parse HEAD)
BLOBS="/tmp/bravros-blobs-$PR_NUMBER.txt"
git diff --name-only "origin/$BASE_BRANCH"..."$PRE_MERGE_COMMIT" \
  -- '*.php' '*.ts' '*.tsx' '*.js' '*.jsx' '*.py' '*.go' \
     ':(exclude)tests/' ':(exclude)test/' ':(exclude)spec/' ':(exclude)__tests__/' \
| while IFS= read -r f; do
    printf '%s %s\n' "$(git rev-parse --verify --quiet "$PRE_MERGE_COMMIT:$f" || echo ABSENT)" "$f"
  done > "$BLOBS"
cat "$BLOBS"
```

`--verify --quiet` is load-bearing (bare `rev-parse` prints the missing path to stdout), and the
path stays in `$f` so zsh never reads a `:<letter>` modifier — see the trap table at the top.

### The `--delete-branch` gate

⛔ Add `--delete-branch` **only** when the head branch is neither permanent nor checked out
anywhere. Fail closed: if the head cannot be resolved, drop the flag.

```bash
HEAD_BRANCH=$(gh pr view "$PR_NUMBER" --json headRefName -q .headRefName 2>/dev/null || echo "")
# Keep the bash ARRAY — unquoted-string iteration breaks in zsh, which is how the
# remote homolog branch got deleted twice (PRs #289/#290).
PERMANENT_BRANCHES=(main homolog staging develop)

DELETE_FLAG=""
if [ -n "$HEAD_BRANCH" ]; then
  PERMANENT_HIT=""
  for pb in "${PERMANENT_BRANCHES[@]}"; do
    [ "$HEAD_BRANCH" = "$pb" ] && { PERMANENT_HIT=1; break; }
  done
  HEAD_WT=$(git worktree list --porcelain | awk -v b="branch refs/heads/$HEAD_BRANCH" \
    '/^worktree / { wt=$2 } $0 == b { print wt }')
  [ -z "$PERMANENT_HIT" ] && [ -z "$HEAD_WT" ] && DELETE_FLAG="--delete-branch"
fi
```

Say out loud which condition suppressed the flag, so the leftover branch is not a surprise.

Step 3b must have passed **and still hold** — re-read `mergeStateStatus` if anything pushed since,
because the readiness fact expires the moment the branch or base moves:

```bash
STRATEGY=$(awk -F': *' '/^merge_strategy:/{print $2}' .bravros.yml 2>/dev/null); STRATEGY=${STRATEGY:-merge}
bravros merge-lock acquire --timeout 60s --ttl 10m --meta reason=finish --meta pr="$PR_NUMBER"
gh pr merge "$PR_NUMBER" --"$STRATEGY" $DELETE_FLAG || { bravros merge-lock release; exit 1; }
bravros merge-lock release
```

On conflict: surface the conflicted files, release the lock, stop and ask. Test-only add/add
conflicts under `tests/` may be auto-resolved with `--theirs`; application-code conflicts never are.

**Post-merge blob verification.** Run this verbatim. It replaced a prose description of the same
check, which is what sent PR #1919 through three rounds of broken improvised bash mid-merge:

```bash
MERGE_SHA=$(gh pr view "$PR_NUMBER" --json mergeCommit -q .mergeCommit.oid)
git fetch origin "$BASE_BRANCH" -q          # the merge commit is not local yet
FAIL=0
while IFS=' ' read -r expected f; do
  [ -n "$f" ] || continue
  actual=$(git rev-parse --verify --quiet "$MERGE_SHA:$f" || echo ABSENT)
  if [ "$expected" = "$actual" ]; then
    printf '  ok       %s\n' "$f"
  else
    printf '  MISMATCH %s expected=%s actual=%s\n' "$f" "$expected" "$actual"
    FAIL=1
  fi
done < "$BLOBS"
echo "verification_fail=$FAIL"
```

A mismatch means the merge did not carry that change — a feature file whose blob matches *base*
instead of feature means the change was silently lost. Ask in normal mode, `exit 1` under
`--batch`. Do **not** wave a mismatch away as a quoting artifact: this recipe has no quoting
artifacts left, so a mismatch here is real. If you nonetheless need a second opinion, confirm with
`git ls-tree -r --name-only "$MERGE_SHA" -- <dir>/` rather than another hand-rolled `git show`.

Confirm `gh pr view "$PR_NUMBER" --json state -q .state` is `MERGED` before reporting success,
then `rm -f "$BLOBS"`.

## Step 5: Sync local

A linked worktree is pinned to its feature branch — the operator runs several in parallel,
and a `git checkout <base>` here silently moves the worktree off the branch every other
step assumed. **Never switch branches inside a linked worktree**; detect it first:

```bash
IN_LINKED_WORKTREE=0
[ "$(git rev-parse --git-dir)" != "$(git rev-parse --git-common-dir)" ] && IN_LINKED_WORKTREE=1

if [ -n "$BASE_HELD_AT" ]; then
  git fetch origin "$BASE_BRANCH"
  git -C "$BASE_HELD_AT" pull --ff-only origin "$BASE_BRANCH" \
    || echo "⚠️  could not fast-forward $BASE_BRANCH in $BASE_HELD_AT — sync it manually"
  echo "ℹ️  kept local $FEATURE_BRANCH — held by this worktree; /worktree destroy when done"
elif [ "$IN_LINKED_WORKTREE" = "1" ]; then
  git fetch origin "$BASE_BRANCH"
  echo "ℹ️  linked worktree: staying on $FEATURE_BRANCH — $BASE_BRANCH is updated on origin only"
  echo "ℹ️  sync a primary checkout when convenient; /worktree destroy this one when done"
else
  git checkout "$BASE_BRANCH" && git fetch origin "$BASE_BRANCH" && git reset --hard "origin/$BASE_BRANCH"
  git branch -d "$FEATURE_BRANCH" 2>/dev/null \
    || echo "ℹ️  kept local $FEATURE_BRANCH (held by a worktree, unmerged, or already gone)"
fi
```

## Step 7: main — decision announce + merge

**Step 7 vs `/promote` — both are sanctioned; they are not rivals.** Step 7 is the
feature-completion path: the operator answered the main question in this session, and the merge
goes through a **PR against `main`** with its full check suite, which is what "main is PR-gated
only" requires. `/promote` is the standalone path for work already sitting on homolog with no
`/finish` in flight, and it needs the out-of-band token because nothing else in that flow proves
a human is present. A project `CLAUDE.md` that routes promotion through `/promote` is describing
the standalone case; it does not make Step 7 a bypass, and Step 7 never needs a promote token.
Say which path you are on, then take it — do not stop mid-merge to reconcile the two.

```bash
# <!-- announce-template: "Mesclagem na produção aguarda sua decisão. Projeto {PROJECT}." -->
bravros ha say --force "Mesclagem na produção aguarda sua decisão. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```

First **report the promotion scope** — this merge ships everything accumulated on homolog, not
just this feature, and the operator answered "merge to main" about their own PR:

```bash
git fetch origin main homolog -q
git log --oneline origin/main..origin/homolog
echo "count: $(git rev-list --count origin/main..origin/homolog)"
```

Commits from other features in that list → name them before merging.

On "merge to main":

```bash
MAIN_PR=$(gh pr list --base main --head homolog --json number -q '.[0].number')
if [ -z "$MAIN_PR" ]; then
  gh pr create --base main --head homolog --title "🔀 merge: homolog → main" \
    --body "Promoting accumulated homolog work to main. Opened by /finish." \
    || { echo "❌ could not open the homolog→main PR"; exit 1; }
  MAIN_PR=$(gh pr list --base main --head homolog --json number -q '.[0].number')
  [ -z "$MAIN_PR" ] && { echo "❌ PR opened but its number could not be resolved"; exit 1; }
fi
```

⛔ **The main PR gets its own Step 3 + Step 3b — no exceptions.** Step 3 ran against the *feature*
PR; a PR opened seconds ago has every check `queued`, and `main` is exactly where a protection
workflow (`Protect Main Branch`, `check-source-branch`) is most likely to be the gate that matters.
PR #1922 was merged at `mergeStateStatus=UNSTABLE` with its source-branch check still pending, and
the run only came back green afterwards — luck, not a gate.

```bash
gh pr checks "$MAIN_PR" --watch --fail-fast > /tmp/bravros-checks-$MAIN_PR.txt 2>&1
RC=$?; tail -8 /tmp/bravros-checks-$MAIN_PR.txt; echo "checks_rc=$RC"
until [ "$(gh pr view "$MAIN_PR" --json mergeStateStatus -q .mergeStateStatus)" != "UNKNOWN" ]; do sleep 5; done
STATUS=$(gh pr view "$MAIN_PR" --json mergeStateStatus -q .mergeStateStatus)
[ "$STATUS" = "CLEAN" ] || { echo "⚠️  main PR not CLEAN (status=$STATUS, checks_rc=$RC) — stopping"; exit 1; }
```

A newly created PR whose checks are still `queued` is the *expected* state here — wait it out;
never read "mergeable=MERGEABLE" as permission to skip the wait.

```bash
bravros merge-lock acquire --timeout 60s --ttl 10m --meta reason=finish-main --meta pr="$MAIN_PR"
gh pr merge "$MAIN_PR" --"$STRATEGY" || { bravros merge-lock release; exit 1; }   # never --delete-branch: homolog is permanent
bravros merge-lock release

# Keep homolog from drifting behind main for the next cycle. Server-side
# fast-forward — no checkout, so it is safe from any worktree (the old
# `git checkout homolog` form silently moved a linked worktree off its branch):
git fetch origin main homolog -q
git push origin origin/main:homolog \
  || echo "⚠️  homolog diverged from main — from a checkout that holds homolog: merge origin/main, then push"
```

**Hard conflicts** — do not fight them in place: close the PR, branch
`merge/homolog-to-main` off `origin/main`, merge `origin/homolog` there, resolve, push, and
open a fresh PR from that branch.

## Step 8: Cleanup + announce

```bash
# Drop review stamps for PRs that are no longer open. A stamp authorizes ONE merge;
# past that it is litter, and it accumulated 42 files across three repos before this
# swept. Self-healing: covers stamps this run never wrote.
for s in .planning/.review-stamp-*.json; do
  [ -e "$s" ] || continue
  n=$(basename "$s" .json); n=${n#.review-stamp-}
  [ "$(gh pr view "$n" --json state -q .state 2>/dev/null)" = "OPEN" ] || rm -f "$s"
done

rm -f "/tmp/review-cache-${PR_NUMBER}.txt"
# <!-- announce-template: "Plano {NUM} finalizado, mesclagem concluída. Projeto {PROJECT}." -->
bravros ha say --force "${PLAN_NUM:+Plano $PLAN_NUM }finalizado, mesclagem concluída. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```
