# Shared Recipe: Verify Suite

Framework-agnostic full-suite regression gate: run the suite, parse counts, reconcile
against the pre-implementation baseline, block on **new** failures only. Opt-in for
`/orchestrate` and `/auto-pr` via `features.extra.verify_suite: true` in `.bravros.yml`
(default false).

## Contract

**Caller responsibility:** run **Step 0** *before implementation begins* — it records
the base-branch baseline Step 4 reconciles against.

```bash
PRE_FAIL_FILE=".planning/.verify-suite-pre-fail.json"
SUMMARY_FILE=".planning/.verify-suite-result.json"
# Result schema: { "runner": "...", "passed": N, "failed": N, "skipped": N,
#                  "new_failures": ["test1"], "status": "pass"|"fail"|"skipped" }
```

## Step 0: Write the Base-Branch Baseline (BEFORE implementation)

**Why it must exist:** without a recorded baseline, "that test was already failing" is
unfalsifiable. A worker `git stash`-ing a clean tree and re-running the suite proves
nothing — stashing nothing changes nothing (see `dispatch.md` § Pre-existing failure
floor). The baseline runs in a throwaway worktree at `origin/<base>` so the working tree
is never touched.

```bash
BASE_BR=$(awk '/^base:/{print $2; exit}' .bravros.yml 2>/dev/null)
[ -z "$BASE_BR" ] && BASE_BR=homolog
RUNNER=$(awk '/test_runner:/{sub(/^[^:]*:[[:space:]]*/,""); print; exit}' .bravros.yml 2>/dev/null)
PRE_FAIL_FILE="$PWD/.planning/.verify-suite-pre-fail.json"
mkdir -p .planning

if [ -z "$RUNNER" ]; then
  echo "⚠️  [verify-suite:0] no test_runner — writing empty baseline (plan is UNVERIFIABLE)."
  printf '{"base":"%s","commit":"","framework":"","failed_count":0,"failures":[],"created":"%s"}\n' \
    "$BASE_BR" "$(date "+%Y-%m-%dT%H:%M")" > "$PRE_FAIL_FILE"
else
  git fetch origin "$BASE_BR" --quiet 2>/dev/null || true
  BASE_SHA=$(git rev-parse "origin/$BASE_BR" 2>/dev/null || git rev-parse "$BASE_BR")
  BASE_WT=$(mktemp -d /tmp/verify-base-XXXXXX)
  BASE_OUT=$(mktemp /tmp/verify-base-out-XXXXXX.txt)

  git worktree add --detach --quiet "$BASE_WT" "$BASE_SHA"
  ( cd "$BASE_WT" && eval "$RUNNER" ) > "$BASE_OUT" 2>&1
  BASE_EXIT=$?
  echo "[verify-suite:0] baseline suite exit=$BASE_EXIT at origin/$BASE_BR ($BASE_SHA)"
  # ... extract FRAMEWORK / FAIL_COUNT / FAIL_NAMES from "$BASE_OUT" per the recipes below ...
fi
```

### Failure-NAME extraction recipes (portable — no `grep -P`)

`grep -oP` is a GNU-only extension, unavailable on macOS BSD grep — a silent error there
would leave counts empty and let a red suite pass. Use `grep -E` + `sed -E`/`awk` only.
One test name per line to `$BASE_NAMES`:

```bash
BASE_NAMES=$(mktemp /tmp/verify-base-names-XXXXXX.txt)
: > "$BASE_NAMES"
FRAMEWORK="unknown"; FAIL_COUNT=""

# Count helper. NEVER write `$(grep -c ... || echo 0)`: on no-match grep -c PRINTS "0"
# *and* exits 1, so the `|| echo 0` appends a second line → "0\n0" → every later
# `[ "$N" -eq 0 ]` dies with "integer expression expected" and the guard FAILS OPEN.
count_matches() { c=$(grep -cE "$1" "$2" 2>/dev/null || true); echo "${c:-0}"; }
count_word() { grep -oE "[0-9]+ $1" "$2" 2>/dev/null | tail -1 | grep -oE '^[0-9]+' || true; }

# --- Pest / PHPUnit / Laravel ---
#   Pest:    "  FAILED  Tests\Feature\CardTest > it restores the card"
#   PHPUnit: "1) Tests\Feature\CardTest::test_restores_card"
if echo "$RUNNER" | grep -qE 'pest|phpunit|artisan test'; then
  FRAMEWORK=$(echo "$RUNNER" | grep -q phpunit && echo phpunit || echo pest)
  sed -nE 's/^[[:space:]]*(FAILED|⨯)[[:space:]]+(.*)$/\2/p'      "$BASE_OUT" >> "$BASE_NAMES"
  sed -nE 's/^[[:space:]]*[0-9]+\)[[:space:]]+(.+)$/\1/p'        "$BASE_OUT" >> "$BASE_NAMES"
  FAIL_COUNT=$(sed -nE 's/.*Failures:[[:space:]]*([0-9]+).*/\1/p' "$BASE_OUT" | tail -1)
  [ -z "$FAIL_COUNT" ] && FAIL_COUNT=$(count_word failed "$BASE_OUT")

# --- Jest / Vitest ---
#   "  ✕ renders the card"   |   "● CardSuite › renders the card"
elif echo "$RUNNER" | grep -qE 'jest|vitest'; then
  FRAMEWORK=$(echo "$RUNNER" | grep -q vitest && echo vitest || echo jest)
  sed -nE 's/^[[:space:]]*[✕×✗][[:space:]]+(.+)$/\1/p'           "$BASE_OUT" >> "$BASE_NAMES"
  sed -nE 's/^[[:space:]]*●[[:space:]]+(.+›.+)$/\1/p'            "$BASE_OUT" >> "$BASE_NAMES"
  FAIL_COUNT=$(count_word failed "$BASE_OUT")

# --- pytest ---
#   "FAILED tests/test_card.py::test_restore - AssertionError"
elif echo "$RUNNER" | grep -q pytest; then
  FRAMEWORK="pytest"
  sed -nE 's/^FAILED[[:space:]]+([^[:space:]]+).*$/\1/p'         "$BASE_OUT" >> "$BASE_NAMES"
  FAIL_COUNT=$(count_word failed "$BASE_OUT")

# --- Go ---
#   "--- FAIL: TestRestoreCard (0.00s)"
elif echo "$RUNNER" | grep -q 'go test'; then
  FRAMEWORK="go"
  sed -nE 's/^[[:space:]]*--- FAIL:[[:space:]]+([^[:space:]]+).*$/\1/p' "$BASE_OUT" >> "$BASE_NAMES"
  FAIL_COUNT=$(count_matches '^[[:space:]]*--- FAIL:' "$BASE_OUT")
fi

FAIL_COUNT=${FAIL_COUNT:-0}
sort -u "$BASE_NAMES" -o "$BASE_NAMES"
NAME_COUNT=$(count_matches '.' "$BASE_NAMES")
```

### SUSPICIOUS guard (hard gate — a reported failure must produce a name)

If the summary says failures happened but the parser produced ZERO names, the parser did
not match this runner's output. An empty baseline there would make every real failure
look "new" — or let a later `status:"pass"` be written over a red suite. **Exit non-zero
with status SUSPICIOUS.**

```bash
if [ "$FAIL_COUNT" -gt 0 ] && [ "$NAME_COUNT" -eq 0 ]; then
  echo "❌ [verify-suite:0] SUSPICIOUS — baseline reports $FAIL_COUNT failure(s) but parsed 0 names."
  echo "    Fix the extraction recipe — do NOT implement against a blind baseline."
  printf '{"base":"%s","commit":"%s","framework":"%s","failed_count":%s,"failures":[],"created":"%s","status":"SUSPICIOUS"}\n' \
    "$BASE_BR" "$BASE_SHA" "$FRAMEWORK" "$FAIL_COUNT" "$(date "+%Y-%m-%dT%H:%M")" > "$PRE_FAIL_FILE"
  git worktree remove --force "$BASE_WT" 2>/dev/null || true
  rm -f "$BASE_OUT" "$BASE_NAMES"
  exit 1
fi
```

### Write the baseline + tear down the worktree

```bash
FAILURES_JSON=$(jq -R -s 'split("\n") | map(select(length > 0))' < "$BASE_NAMES")

jq -n \
  --arg base "$BASE_BR" \
  --arg commit "$BASE_SHA" \
  --arg framework "$FRAMEWORK" \
  --argjson failed_count "${FAIL_COUNT:-0}" \
  --argjson failures "$FAILURES_JSON" \
  --arg created "$(date "+%Y-%m-%dT%H:%M")" \
  '{base:$base,commit:$commit,framework:$framework,failed_count:$failed_count,failures:$failures,created:$created}' \
  > "$PRE_FAIL_FILE"

git worktree remove --force "$BASE_WT" 2>/dev/null || true
rm -f "$BASE_OUT" "$BASE_NAMES"
```

A missing baseline file is NOT "the suite was clean" — it means Step 0 was skipped.
Step 4 then treats the pre-fail set as empty so every failure surfaces as new. That is
intentional: the safe default is to over-report, never under-report.

## Step 1: Runner Resolution

Read `stack.test_runner` from `.bravros.yml`; if unset, infer from the repo (`go.mod` →
`go test ./...`, `artisan` + Pest → `php artisan test`, `package.json` `scripts.test`,
`pytest.ini`/`pyproject`). If nothing resolves:

```bash
if [ -z "$RUNNER" ]; then
  echo "⚠️  [verify-suite] NO TEST RUNNER CONFIGURED — plan treated as UNVERIFIED."
  echo "    Fix: add stack.test_runner to .bravros.yml and re-run."
  echo '{"runner":"","passed":0,"failed":0,"skipped":0,"new_failures":[],"status":"skipped"}' \
    > .planning/.verify-suite-result.json
  exit 0
fi
```

The parent skill MUST NOT silently proceed past `status:"skipped"` — surface it to the
operator and get an explicit acknowledgement.

## Step 2: Execute

Run in background (`run_in_background`/`--bg`) and poll; never hold the session
foreground on a long suite.

```bash
SUITE_OUTPUT=$(mktemp /tmp/verify-suite-XXXXXX.txt)
eval "$RUNNER" > "$SUITE_OUTPUT" 2>&1
RUNNER_EXIT=$?
cat "$SUITE_OUTPUT"   # audit trail
```

## Step 3: Parse Pass/Fail/Skipped Counts

Same portability rules as Step 0 (no `grep -oP`, no `|| echo 0` after `grep -c`):

```bash
PASSED=0; FAILED=0; SKIPPED=0

if echo "$RUNNER" | grep -q "go test"; then
  PASSED=$(grep -c '^ok ' "$SUITE_OUTPUT" 2>/dev/null || true)
  FAILED=$(grep -c '^FAIL' "$SUITE_OUTPUT" 2>/dev/null || true)
  SKIPPED=$(grep -cE '^[[:space:]]*--- SKIP' "$SUITE_OUTPUT" 2>/dev/null || true)
elif echo "$RUNNER" | grep -qE "pest|phpunit|artisan test"; then
  PASSED=$(sed -nE 's/.*Tests:[[:space:]]*([0-9]+).*/\1/p'    "$SUITE_OUTPUT" | tail -1)
  FAILED=$(sed -nE 's/.*Failures:[[:space:]]*([0-9]+).*/\1/p' "$SUITE_OUTPUT" | tail -1)
  SKIPPED=$(sed -nE 's/.*Skipped:[[:space:]]*([0-9]+).*/\1/p' "$SUITE_OUTPUT" | tail -1)
  # Pest's newer summary line: "Tests:  2 failed, 40 passed (120 assertions)"
  [ -z "$FAILED" ] && FAILED=$(sed -nE 's/.*[^0-9]([0-9]+) failed.*/\1/p'   "$SUITE_OUTPUT" | tail -1)
  [ -z "$PASSED" ] && PASSED=$(sed -nE 's/.*[^0-9]([0-9]+) passed.*/\1/p'   "$SUITE_OUTPUT" | tail -1)
elif echo "$RUNNER" | grep -qE "vitest|jest|pytest"; then
  PASSED=$(sed -nE 's/.*[^0-9]([0-9]+) passed.*/\1/p'  "$SUITE_OUTPUT" | tail -1)
  FAILED=$(sed -nE 's/.*[^0-9]([0-9]+) failed.*/\1/p'  "$SUITE_OUTPUT" | tail -1)
  SKIPPED=$(sed -nE 's/.*[^0-9]([0-9]+) skipped.*/\1/p' "$SUITE_OUTPUT" | tail -1)
else
  # Unknown runner — exit-code-only judgment
  [ $RUNNER_EXIT -eq 0 ] && PASSED=1 || FAILED=1
fi

# Unmatched parser branches leave these empty — normalize before arithmetic.
PASSED=${PASSED:-0}; FAILED=${FAILED:-0}; SKIPPED=${SKIPPED:-0}

# Exit-code cross-check: non-zero exit with FAILED=0 means the parser missed.
# Never report that as a pass — force at least one failure.
if [ "$RUNNER_EXIT" -ne 0 ] && [ "$FAILED" -eq 0 ]; then
  echo "⚠️  [verify-suite] runner exited $RUNNER_EXIT but parsed 0 failures — treating as failure."
  FAILED=1
fi
```

## Step 4: Reconcile Against the Baseline

Only **new** failures block — pre-existing failures may persist (this is a regression
gate, not a clean-suite gate).

```bash
PRE_FAIL_FILE="${PRE_FAIL_FILE:-.planning/.verify-suite-pre-fail.json}"

# Extract current failing names from $SUITE_OUTPUT with the SAME per-framework
# recipes as Step 0. Go shown:
CURRENT_FAILS=$(sed -nE 's/^[[:space:]]*--- FAIL:[[:space:]]+([^[:space:]]+).*$/\1/p' "$SUITE_OUTPUT" | sort -u || true)

if [ -f "$PRE_FAIL_FILE" ]; then
  if [ "$(jq -r '.status // empty' "$PRE_FAIL_FILE" 2>/dev/null)" = "SUSPICIOUS" ]; then
    echo "❌ [verify-suite] baseline is SUSPICIOUS — refusing to reconcile against a blind baseline." >&2
    exit 1
  fi
  PRE_FAILS=$(jq -r 'if type=="array" then .[] else (.failures // [])[] end' "$PRE_FAIL_FILE" 2>/dev/null | sort -u || true)
else
  echo "⚠️  [verify-suite] no baseline — Step 0 was skipped; all failures count as new."
  PRE_FAILS=""
fi

NEW_FAILS=$(comm -23 <(echo "$CURRENT_FAILS") <(echo "$PRE_FAILS") || true)
NEW_FAIL_COUNT=$(echo "$NEW_FAILS" | grep -cE '[^[:space:]]' 2>/dev/null || true)
NEW_FAIL_COUNT=${NEW_FAIL_COUNT:-0}
```

## Step 5: Block / Proceed Decision

```bash
SUMMARY_FILE=".planning/.verify-suite-result.json"
NEW_FAIL_JSON=$(echo "$NEW_FAILS" | jq -R -s 'split("\n") | map(select(length > 0))' 2>/dev/null || echo '[]')

if [ "$NEW_FAIL_COUNT" -gt 0 ]; then
  jq -n --arg runner "$RUNNER" --argjson passed "$PASSED" --argjson failed "$FAILED" \
    --argjson skipped "$SKIPPED" --argjson new_failures "$NEW_FAIL_JSON" \
    '{"runner":$runner,"passed":$passed,"failed":$failed,"skipped":$skipped,"new_failures":$new_failures,"status":"fail"}' \
    > "$SUMMARY_FILE"
  echo "❌ [verify-suite] BLOCKED — $NEW_FAIL_COUNT new test failure(s):"
  echo "$NEW_FAILS"
  rm -f "$SUITE_OUTPUT"
  exit 1
else
  jq -n --arg runner "$RUNNER" --argjson passed "$PASSED" --argjson failed "$FAILED" \
    --argjson skipped "$SKIPPED" \
    '{"runner":$runner,"passed":$passed,"failed":$failed,"skipped":$skipped,"new_failures":[],"status":"pass"}' \
    > "$SUMMARY_FILE"
  echo "✅ [verify-suite] PASSED — no new failures (passed=$PASSED, pre-existing=$FAILED)"
  rm -f "$SUITE_OUTPUT"
  exit 0
fi
```

On exit 1 the caller must NOT proceed to PR creation.

## Suspicious-Result Detection

Any caller reading `.verify-suite-result.json` MUST flag all-zero counts with
`status:"pass"` — the runner exited 0 but nothing was parsed: a missing parser branch,
a misconfigured empty suite, or a falsified worker-written file. Re-run the suite or
block the plan pending investigation; never accept it as a pass.

## Invoking This Recipe

Bash-in-markdown prose — copy the step blocks into the calling skill or reference this
file by path; do not `bash` this file directly. This file is the single source of truth
for the no-runner and suspicious-result policy — edit here, never duplicate in callers.
