# Shared Recipe: Smoke Gate

**A green test is a claim, not evidence.**

P-0180 is the cautionary tale: five green phase agents shipped a feature that did not
work. Every unit test passed — but the tests called `ParsePlanHeader(dir)` directly,
while the binary's real path is `FindPlanFile → ParsePlanHeader`, and the seam was
broken. This recipe closes that gap: **build the real artifact, run the real verb**,
record the exact invocation and observed output. Nothing else counts as evidence.

Used by `/plan` (its inline review injects a smoke acceptance criterion when a phase
touches `cli/`) and by any flow that must answer "does the shipped binary actually do
the thing?" before its final step.

## Step 1: Applicability Detection

The gate applies only when the diff touches `cli/`. Base = the PR's base branch when one
exists, else `homolog`:

```bash
BASE=$(gh pr view --json baseRefName -q .baseRefName 2>/dev/null)
[ -z "$BASE" ] && BASE="homolog"

CLI_CHANGES=$(git diff --name-only "origin/${BASE}...HEAD" -- cli/ 2>/dev/null)

if [ -z "$CLI_CHANGES" ]; then
  echo '{"status":"skipped","reason":"no cli/ changes"}'
  exit 0
fi
```

Non-`cli/` plans stop here with `status:"skipped"` and are **not** blocked.

## Step 2: Build to a Scratch Path

**Never build to `bin/bravros`.** A committed binary breaks the selfupdate drift
detector (releases ship via tag-push only). Read the module path from `cli/go.mod` —
never hardcode it.

```bash
SCRATCH=$(mktemp -d)
SMOKE_BIN="$SCRATCH/bravros-smoke"
MODULE=$(awk '/^module /{print $2; exit}' cli/go.mod)

( cd cli && go build \
    -ldflags="-s -w -X ${MODULE}/cmd.Version=v0.0.0-smoke" \
    -o "$SMOKE_BIN" . ) > "$SCRATCH/build.log" 2>&1
BUILD_EXIT=$?
cat "$SCRATCH/build.log"

if [ "$BUILD_EXIT" -ne 0 ] || [ ! -x "$SMOKE_BIN" ]; then
  echo "❌ [smoke-gate] BUILD FAILED — the gate BLOCKS."
  jq -n --arg base "$BASE" --arg head "$(head -20 "$SCRATCH/build.log")" \
    '{"status":"fail","binary":"","base":$base,"commands":[],"reason":("build failed: " + $head)}'
  exit 1
fi
```

A build failure is a **hard fail**, not a skip — if the artifact cannot be produced,
nothing downstream was verified.

## Step 3: Choose the Verb(s) to Exercise

Derive verbs from the diff; `go test` is **never** a substitute for the CLI entry point.

**Case A — `cli/cmd/*.go` changed:** the basename is the verb (Cobra convention:
`cli/cmd/promote.go` → `bravros promote`).

```bash
VERBS=()
while IFS= read -r f; do
  case "$f" in
    cli/cmd/*.go)
      b=$(basename "$f" .go)
      case "$b" in *_test|root) continue ;; esac
      VERBS+=("$b")
      ;;
  esac
done < <(printf '%s\n' "$CLI_CHANGES")
```

**Case B — only `cli/internal/**` changed:** smoke the command file(s) that import the
changed package.

```bash
if [ ${#VERBS[@]} -eq 0 ]; then
  while IFS= read -r f; do
    case "$f" in
      cli/internal/*/*.go)
        PKG=$(basename "$(dirname "$f")")
        while IFS= read -r cmdfile; do
          b=$(basename "$cmdfile" .go)
          case "$b" in *_test|root) continue ;; esac
          VERBS+=("$b")
        done < <(grep -rl "${MODULE}/internal/${PKG}" cli/cmd --include='*.go' 2>/dev/null)
        ;;
    esac
  done < <(printf '%s\n' "$CLI_CHANGES")
fi

VERBS=($(printf '%s\n' "${VERBS[@]}" | sort -u))
```

If the derivation yields **no verb**, that is a `suspicious` result (Step 5) — the caller
must name the verb explicitly rather than let the gate pass empty.

## Step 4: Run the Real Verb(s) in a Scratch Fixture

Run each verb against a throwaway fixture carrying whatever `.planning/`/config state it
needs. Never run destructive verbs against the working repo.

```bash
FIXTURE="$SCRATCH/fixture"
mkdir -p "$FIXTURE/.planning"
cat > "$FIXTURE/.bravros.yml" <<'YAML'
project: smoke-fixture
base: homolog
YAML

RESULTS_FILE="$SCRATCH/commands.jsonl"
: > "$RESULTS_FILE"
GATE_STATUS="pass"

for verb in "${VERBS[@]}"; do
  # Use the verb's real, non-destructive entry invocation. --help ONLY as a last
  # resort — it exercises Cobra, not your change, and must be flagged in `reason`.
  INVOCATION="$SMOKE_BIN $verb"
  OUT=$( cd "$FIXTURE" && eval "$INVOCATION" 2>&1 )
  EXIT=$?
  HEAD=$(printf '%s\n' "$OUT" | head -10)

  echo "─── $INVOCATION (exit=$EXIT)"
  printf '%s\n' "$HEAD"

  jq -n --arg cmd "$INVOCATION" --argjson exit "$EXIT" --arg head "$HEAD" \
    '{"cmd":$cmd,"exit":$exit,"output_head":$head}' >> "$RESULTS_FILE"

  if [ "$EXIT" -ne 0 ]; then
    GATE_STATUS="fail"
  elif [ -z "$(printf '%s' "$OUT" | tr -d '[:space:]')" ]; then
    # Exit 0 with no output where output was expected: record and judge — never auto-pass.
    GATE_STATUS="suspicious"
  fi
done
```

The chosen verbs **and their observed outputs** are the evidence the acceptance
criterion asks you to paste.

## Step 5: Falsification Guard

A suspicious result **fails** — a gate that ran nothing has verified nothing.
`status:"pass"` with an empty `commands` array is forbidden.

```bash
CMD_COUNT=$(grep -c '\S' "$RESULTS_FILE" 2>/dev/null || echo 0)

if [ "$CMD_COUNT" -eq 0 ]; then
  GATE_STATUS="suspicious"
  REASON="no verb was executed — derivation produced an empty command set"
fi

COMMANDS_JSON=$(jq -s '.' "$RESULTS_FILE" 2>/dev/null || echo '[]')

jq -n \
  --arg status "$GATE_STATUS" \
  --arg binary "$SMOKE_BIN" \
  --arg base   "$BASE" \
  --argjson commands "$COMMANDS_JSON" \
  --arg reason "${REASON:-}" \
  '{"status":$status,"binary":$binary,"base":$base,"commands":$commands,"reason":$reason}'

case "$GATE_STATUS" in
  pass) echo "✅ [smoke-gate] PASS — $CMD_COUNT verb(s) exercised against a freshly built binary." ;;
  *)    echo "❌ [smoke-gate] $GATE_STATUS — the caller MUST NOT proceed to its final step." ; exit 1 ;;
esac
```

## Result-JSON Contract

```json
{
  "status": "pass|fail|skipped|suspicious",
  "binary": "<scratch path>",
  "base": "<base branch>",
  "commands": [
    {"cmd": "<exact invocation>", "exit": 0, "output_head": "<first lines observed>"}
  ],
  "reason": "<non-pass explanation>"
}
```

- `pass` = a real verb ran on a freshly built binary. `skipped` = no `cli/` changes.
  `fail` = build failed or a verb exited non-zero. `suspicious` = the gate proved
  nothing — **treat as fail**.
- `commands`: one entry per verb actually executed; empty + `pass` is forbidden.
- `reason`: required for any non-`pass` status.

One minimal correct example:

```json
{"status":"pass","binary":"/tmp/tmp.7Kq9/bravros-smoke","base":"homolog",
 "commands":[{"cmd":"/tmp/tmp.7Kq9/bravros-smoke plan-rounds","exit":0,
              "output_head":"Round 1: Phase 1 [H] + Phase 2 [H]"}],
 "reason":""}
```

**Gate semantics for callers:** `pass`/`skipped` → proceed. `fail`/`suspicious` → do NOT
proceed to the final step (PR creation, merge, review stamp, plan completion); fix the
binary or name the verb, then re-run. The smoke criterion is a normal `## Acceptance`
checkbox in the plan.

Clean up (`rm -rf "$SCRATCH"`) only *after* the observed output has been pasted into the
plan's acceptance evidence — the scratch binary is disposable; the recorded output is
the deliverable.

## Invoking This Recipe

Bash-in-markdown prose — do **not** `bash` this file directly; copy the Step 1–5 blocks
into the calling skill or reference this file by path. Arrays use `"${ARRAY[@]}"`
throughout because the Bash tool runs under zsh on macOS, where bare `for X in $VAR`
does not word-split (`skills/CLAUDE.md` → "Bash hygiene: portable array iteration").
