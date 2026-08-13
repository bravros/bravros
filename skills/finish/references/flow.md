# /finish — executable flow detail

Loaded by SKILL.md. The gates here are **mandatory**, not optional detail — load this file
before merging.

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

## Step 2: Close the plan (events model — no CLI verb)

Resolve the plan id — `pr_opened` event first, legacy `pr:` frontmatter second:

```bash
PLAN_ID=$(grep -h '"kind":"pr_opened"' .planning/events*.jsonl 2>/dev/null \
  | grep -E "\"pr\": ?\"?#?${PR_NUMBER}\"?[,}]" | tail -1 \
  | grep -oE '"subject":"[^"]+"' | cut -d'"' -f4)
if [ -z "$PLAN_ID" ]; then
  LEGACY=$(grep -lE "^pr: *#?${PR_NUMBER}\$" .planning/P-*.md .planning/P-*/PLAN.md 2>/dev/null | head -1)
  [ -n "$LEGACY" ] && PLAN_ID=$(echo "$LEGACY" | grep -oE 'P-[0-9]+' | head -1)
fi
```

No `PLAN_ID` → batch/aggregate PR; skip closure and carry on. Already has a `completed`
event → done; never re-close. Otherwise append (never rename the plan file — files are
identity, events are state; `.planning/CONVENTIONS.md`):

```bash
TS=$(date -u +%FT%TZ)
echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"$PLAN_ID\",\"pr\":$PR_NUMBER,\"by\":\"agent:finish\"}" >> .planning/events.jsonl
# Close the plan's backlog items too (legacy `backlog:` frontmatter list).
PLAN_FILE=$(ls .planning/${PLAN_ID}-*.md .planning/${PLAN_ID}-*/PLAN.md 2>/dev/null | head -1)
BACKLOG_IDS=($(awk '/^backlog:/{flag=1;next} /^[a-z_]+:/{flag=0} flag' "$PLAN_FILE" 2>/dev/null | grep -oE 'B-[0-9]+' | sort -u))
for bid in "${BACKLOG_IDS[@]}"; do
  echo "{\"ts\":\"$TS\",\"id\":\"e_$(date -u +%s)$RANDOM\",\"kind\":\"completed\",\"subject\":\"$bid\",\"by\":\"agent:finish\"}" >> .planning/events.jsonl
done
bravros commit "📋 plan: close $PLAN_ID (PR #$PR_NUMBER)" .planning/events.jsonl
git push
```

`PLAN_NUM` (for the announce) = `$PLAN_ID` without the `P-` prefix / leading zeros.

## Step 4: Merge — capture, gate, merge, verify

Pre-merge capture for the blob verification:

```bash
PRE_MERGE_COMMIT=$(git rev-parse HEAD)
FEATURE_FILES=$(git diff --name-only "origin/$BASE_BRANCH"..."$PRE_MERGE_COMMIT" \
  -- '*.php' '*.ts' '*.tsx' '*.js' '*.jsx' '*.py' '*.go' \
     ':(exclude)tests/' ':(exclude)test/' ':(exclude)spec/' ':(exclude)__tests__/')
```

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

```bash
STRATEGY=$(awk -F': *' '/^merge_strategy:/{print $2}' .bravros.yml 2>/dev/null); STRATEGY=${STRATEGY:-merge}
bravros merge-lock acquire --timeout 60s --ttl 10m --meta reason=finish --meta pr="$PR_NUMBER"
gh pr merge "$PR_NUMBER" --"$STRATEGY" $DELETE_FLAG || { bravros merge-lock release; exit 1; }
bravros merge-lock release
```

On conflict: surface the conflicted files, release the lock, stop and ask. Test-only add/add
conflicts under `tests/` may be auto-resolved with `--theirs`; application-code conflicts never are.

**Post-merge blob verification.** Get the merge commit (`gh pr view --json mergeCommit -q
.mergeCommit.oid`) and compare each `FEATURE_FILES` blob hash at `PRE_MERGE_COMMIT` vs the
merge commit. A file matching the *base* rather than the feature means the change was lost —
ask in normal mode, `exit 1` in `--batch`.

Confirm `gh pr view "$PR_NUMBER" --json state -q .state` is `MERGED` before reporting success.

## Step 5: Sync local

```bash
if [ -n "$BASE_HELD_AT" ]; then
  git fetch origin "$BASE_BRANCH"
  git -C "$BASE_HELD_AT" pull --ff-only origin "$BASE_BRANCH" \
    || echo "⚠️  could not fast-forward $BASE_BRANCH in $BASE_HELD_AT — sync it manually"
  echo "ℹ️  kept local $FEATURE_BRANCH — held by this worktree; /worktree destroy when done"
else
  git checkout "$BASE_BRANCH" && git fetch origin "$BASE_BRANCH" && git reset --hard "origin/$BASE_BRANCH"
  git branch -d "$FEATURE_BRANCH" 2>/dev/null \
    || echo "ℹ️  kept local $FEATURE_BRANCH (held by a worktree, unmerged, or already gone)"
fi
```

## Step 7: main — decision announce + merge

```bash
# <!-- announce-template: "Mesclagem na produção aguarda sua decisão. Projeto {PROJECT}." -->
bravros ha say --force "Mesclagem na produção aguarda sua decisão. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```

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

bravros merge-lock acquire --timeout 60s --ttl 10m --meta reason=finish-main --meta pr="$MAIN_PR"
gh pr merge "$MAIN_PR" --"$STRATEGY" || { bravros merge-lock release; exit 1; }   # never --delete-branch: homolog is permanent
bravros merge-lock release

# Keep homolog from drifting behind main for the next cycle.
git checkout homolog && git fetch origin
git merge --ff-only origin/main || git merge --no-ff -m "🔀 merge: sync homolog from main (post-finish)" origin/main
git push origin homolog
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
