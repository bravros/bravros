# Autonomous Flow Mode

This document defines the autonomous behavior for `/auto-pr` and `/auto-pr-wt` skills — zero user intervention, all decisions made by Claude.

**Contrast with:**
- `mode-interactive.md` for `/flow` (3 mandatory checkpoints)
- `pipeline.md` for shared core stages

## Critical Rule: NEVER Use AskUserQuestion

This is the #1 rule. Every decision point that exists in interactive flow must be handled autonomously here.

**Decision tree (examples):**
- "Continue to execute?" → Yes, if context < 70%
- "Fix issues and re-check?" → Yes, dispatch fix agents
- "Create PR now?" → Yes, after Green Gate passes
- "PR looks good?" → Yes, if Quality Sweep + Green Gate both pass

The `--auto` flag on delegated skills (`/plan`, `/plan-review`, `/plan-check`, `/pr`) suppresses their checkpoint prompts too.

## Stage Execution

All 6 core stages execute automatically:

| Stage | Skill | AskUserQuestion? | Notes |
|-------|-------|-----------------|-------|
| 1 | `/plan` | ❌ Never | Delegate with `--auto` flag |
| 2 | `/plan-review` | ❌ Never | Delegate with `--auto` flag |
| 3 | `/plan-approved` | ❌ Never | Execute in-house, don't delegate |
| 4 | `/plan-check` + Quality Sweep + Green Gate | ❌ Never | Quality gate before PR creation |
| 5 | `/pr` | ❌ Never | Delegate with `--auto` flag |
| 6 | `/review` + Review Loop | ❌ Never | Trigger review, then poll async, max 3 fix cycles |

No pauses. No checkpoints. One command → PR ready.

## Stage 3: Autonomous Execution

The coordinator runs execution in-house (doesn't delegate to `/plan-approved`). This allows better control over:
- Worker dispatch decisions
- Fix round limits
- Phase-by-phase data validation

**Model tier enforcement — the marker IS the model:**

Workers MUST be dispatched with the correct model based on their `[H]`/`[S]`/`[O]` markers from plan-review:

- `[H]` → `model: "haiku"` — Haiku for all mechanical tasks (CRUD, config, migrations, styling)
- `[S]` → `model: "sonnet"` — Sonnet for business logic, services, tests, components
- `[O]` → `model: "opus"` — Opus for architecture and multi-system reasoning (rare)

This is enforced by audit Rule 19. If a task has no marker, it was not reviewed — STOP and run `/plan-review` first.

**Execution pattern:**

1. Read plan file, identify pending phases
2. For each phase:
   - Pre-validate: check dependencies, setup
   - Dispatch worker agents with correct `model:` parameter matching `[H]`/`[S]`/`[O]` markers
   - Dispatch N workers in parallel for independent phases (all in ONE message)
   - Wait for completion
   - Verify code committed and tests passing
   - Update plan progress
3. After all phases: `git commit -m "✨ feat: complete NNNN-description"`

**Worker failure handling (Immediate Dispatch Rule):**

If a phase fails:
1. Categorize the failure in ONE pass (bad logic? missing setup? test issue?)
2. Dispatch N fix agents in parallel for each category
3. Wait for completion, verify tests
4. Max 2 fix rounds per phase before moving on and noting the issue

If fixes don't land after 2 rounds:
```
⚠️ Phase NNNN: Partial — incomplete feature XXX, test YYY still failing
   [list specific issues]
   [note in plan]
   Continuing to next phase...
```

**Data validation (post-import phases):**

After any phase that imports, seeds, or creates data:

```bash
# Example for Laravel: verify record counts match plan expectations
herd php artisan tinker --execute="echo 'Count: ' . \App\Models\ModelName::count();"
```

- If counts off by >20% from expectations: log warning but continue
- If counts = 0 (complete failure): dispatch fix agent before proceeding
- This prevents cascading errors from undetected import failures

## Stage 4: Quality Sweep + Green Gate

This is where autonomous mode differs most from interactive. Instead of asking "is this ready?", the pipeline enforces quality gates.

### 4a: Plan Check

Delegate to `/plan-check` with `--auto` flag:
- Compares plan vs implementation
- Checks `[x]` marks match commits
- Detects deleted tasks
- Verifies acceptance criteria

### 4b: Quality Sweep (Coordinator Self-Review)

Before any external review, review your own diff:

```bash
git diff $(git merge-base HEAD "${BASE_BRANCH:-main}") HEAD
```

Scan for common issues:
- **Incomplete implementations:** TODO/FIXME/HACK comments, placeholder values, `dd()` or `dump()` calls
- **Import/namespace issues:** unused imports, missing use statements
- **Code quality:** duplicated blocks (>5 lines), overly complex methods, missing return types on public methods
- **Security:** hardcoded credentials, `env()` outside config files, raw SQL without bindings
- **Convention violations:** non-Pest test syntax, missing factory usage, Vue/React where Livewire should be used
- **Schema mismatches:** When diff contains `updateOrCreate()`/`create()`/`update()` with attribute arrays, verify each key exists in the model's `$fillable` or the table's columns. Copy-pasted patterns often include columns that don't exist in the target table.
- **Route registration:** When diff adds controllers, API endpoints, or package service providers, verify routes are registered and no path conflicts exist with existing packages

**For each category with issues:**
1. Dispatch a fix agent with specific file:line references
2. Wait for completion
3. Re-read diff and verify fixes

**Max 2 self-review rounds** — catch the obvious, don't chase perfection. This catches 80%+ of what an external reviewer would flag.

### 4c: Integration Test Sweep (Green Gate)

Run ALL test files created/modified during execution:

```bash
TEST_FILES=$(git diff --name-only "$(git merge-base HEAD "${BASE_BRANCH:-main}")" HEAD | grep -E '(Test|test).*\.php$' | tr '\n' ' ')
if [ -n "$TEST_FILES" ]; then
  vendor/bin/pest $TEST_FILES
fi
```

**Transitive test sweep** — catch tests affected by modified classes:

```bash
# Find application classes that were modified
MODIFIED_CLASSES=$(git diff --name-only "$(git merge-base HEAD "${BASE_BRANCH:-main}")" HEAD | grep -E '^app/' | sed 's|/|\\\\|g;s|\.php$||;s|^app|App|')
TRANSITIVE_TESTS=""
for CLASS in $MODIFIED_CLASSES; do
  EXTRA=$(grep -rl --fixed-strings "$CLASS" tests/ --include="*.php" 2>/dev/null | grep -v vendor | tr '\n' ' ')
  TRANSITIVE_TESTS="$TRANSITIVE_TESTS $EXTRA"
done
# Deduplicate and remove already-tested files
TRANSITIVE_TESTS=$(echo "$TRANSITIVE_TESTS" | tr ' ' '\n' | sort -u | grep -v -F "$TEST_FILES" | tr '\n' ' ')
if [ -n "$TRANSITIVE_TESTS" ]; then
  echo "Running transitive tests: $TRANSITIVE_TESTS"
  vendor/bin/pest $TRANSITIVE_TESTS
fi
```

This catches test files that import/reference modified enums, services, or models but weren't directly modified — preventing silent regressions.

**If tests fail:**
1. Dispatch fix agents targeting the failures
2. Re-run tests
3. Max 2 fix rounds

**If tests still fail after 2 rounds:**
1. Dispatch one final comprehensive fix agent with ALL failure output and full file context
2. Re-run tests (round 3 — hard ceiling)

**If tests still fail after 3 rounds:**
1. Mark specific failures in the plan as blockers
2. Set `MERGE_READY=false`
3. Proceed to PR creation with warning
4. This is a last resort, not the normal path

**If all tests pass:**
1. Set `MERGE_READY=true`
2. Proceed to Stage 5 — PR is certified merge-ready

### 4d: Set Flags

After Quality Sweep + Green Gate complete:

```
MERGE_READY = (Green Gate passed) AND (Quality Sweep clean)
```

Both must be true for MERGE_READY=true. If either fails, the PR will be marked BLOCKED in final report.

## Stage 5: Create PR

Delegate to `/pr` with `--auto` flag:
- Determines base branch (homolog or main)
- Pushes feature branch
- Creates PR via `gh pr create`
- Captures PR URL and number

**Auto-merge chain (feat → homolog → main):**

When PR is created to `homolog`:

```bash
# Step 1: Merge feat → homolog
PR_NUM=$(gh pr view --json number -q '.number')
bravros merge-pr "$PR_NUM" --auto-resolve-planning

# Step 2: Create homolog → main PR
git checkout homolog && git pull origin homolog
MAIN_PR=$(gh pr create --base main --head homolog \
  --title "🔀 merge: homolog into main" \
  --body "Auto-merge from autonomous pipeline...")

# Step 3: Merge homolog → main
MAIN_PR_NUM=$(gh pr view --json number -q '.number')
bravros merge-pr "$MAIN_PR_NUM"

# Step 4: Return to feature branch
git checkout "$BRANCH"
```

**Error handling:**
- If PR already exists: find with `gh pr list` and merge it
- If merge conflicts: STOP and report — do NOT force merge
- If homolog→main PR already merged: skip silently

## Stage 6: Review Loop

Max 3 iterations of trigger → wait → fix → repeat.

### 6a: Trigger Initial Review

Immediately after PR creation, delegate to `/review` with `--auto` flag to trigger the GitHub Action review:

```bash
echo "🤖 [skill:6] triggering /review on PR"
# Delegate to /review skill — this triggers the @claude GitHub Action
# /review --auto $PR_NUM
```

This ensures every auto-pr run gets a proper code review triggered. If `/review` fails (e.g., no GitHub Action configured), log and continue — the PR is still valid without review.

### Pre-check: GitHub Action exists?

Before entering the review **loop** (fix cycles), verify the Action exists:

```bash
CLAUDE_ACTION=$(gh api repos/{owner}/{repo}/actions/workflows --jq '.workflows[] | select(.name | test("claude|Claude|CLAUDE")) | .id' 2>/dev/null)
if [ -z "$CLAUDE_ACTION" ]; then
  echo "🤖 [skill:6] skipping review loop — no @claude GitHub Action detected"
  # Jump directly to Stage 7
fi
```

If no Claude workflow found, skip the fix loop entirely. The initial `/review` trigger still runs (it will fail gracefully if no Action exists).

### For each iteration:

1. **Trigger review:**
   ```bash
   PR_NUM=$(gh pr view --json number -q '.number')
   gh pr comment "$PR_NUM" --body "@claude review this PR and check if we are able to merge. Analyze the code changes for any issues, security concerns, or improvements needed."
   ```

2. **Wait for review** (poll every 5 min, max 20 min):
   ```bash
   for i in $(seq 1 4); do
     sleep 300
     # Check for CHANGES_REQUESTED or bot comments
     gh api repos/{owner}/{repo}/pulls/$PR_NUM/reviews --jq '.[].state' | grep -q "CHANGES_REQUESTED" && break
     BOT_COMMENTS=$(gh api repos/{owner}/{repo}/issues/$PR_NUM/comments --jq '[.[] | select(.user.login == "claude[bot]")] | length')
     [ "$BOT_COMMENTS" -gt 0 ] && break
   done
   ```

3. **If no review after 20 min:** Skip review loop and proceed to Stage 7.

4. **If review comments exist:**
   - Fetch all comments: `~/.claude/bin/bravros pr-review $PR_NUM`
   - Categorize feedback (bugs, style, missing tests, etc.)
   - Dispatch fix agents (one per category)
   - Push fixes

5. **Check if issues remain:**
   - All addressed? Exit loop.
   - Iteration < 3? Loop back to trigger another review.
   - Iteration = 3? Exit loop, note remaining issues.

## Final Report and Auto-Deploy

### Update Project Context (optional)

If significant code was added (>10 files changed):

```bash
FILE_COUNT=$(git diff --name-only "$(git merge-base HEAD homolog 2>/dev/null || echo HEAD~5)" HEAD | wc -l)
if [ "$FILE_COUNT" -gt 10 ]; then
  echo "🤖 Updating project context (CLAUDE.md)"
  # Dispatch lightweight agent to run /context scan
fi
```

### Auto-Deploy to ~/.claude/ (claude config repo only)

Detect if this is the claude config repo:

```bash
PROJECT=$(~/.claude/bin/bravros meta 2>/dev/null | grep '^project:' | awk '{print $2}')
if [ "$PROJECT" = "claude" ]; then
  # Direct mirrors
  cp -rf "$REPO_DIR/skills/." ~/.claude/skills/ 2>/dev/null
  cp -rf "$REPO_DIR/hooks/." ~/.claude/hooks/ 2>/dev/null
  cp -rf "$REPO_DIR/scripts/." ~/.claude/scripts/ 2>/dev/null
  cp -rf "$REPO_DIR/templates/." ~/.claude/templates/ 2>/dev/null

  # Config files → root (skip mcp.json — machine-specific)
  cp -f "$REPO_DIR/config/settings.json" ~/.claude/settings.json 2>/dev/null
  cp -f "$REPO_DIR/config/statusline.sh" ~/.claude/statusline.sh 2>/dev/null

  # Root
  cp -f "$REPO_DIR/CLAUDE.md" ~/.claude/CLAUDE.md 2>/dev/null

  DEPLOY_COUNT=$(git diff --name-only "$(git merge-base HEAD "${BASE_BRANCH:-main}")" HEAD | grep -cE '^(skills|hooks|scripts|templates|config|CLAUDE\.md)' || echo "0")
  echo "✅ Deployed $DEPLOY_COUNT files to ~/.claude/"
fi
```

This is automatic in autonomous mode — NO AskUserQuestion.

### Final PR Comment

Post the final Opus 4.6 recommendation:

```bash
gh pr comment "$PR_NUM" --body "## 🤖 Autonomous Pipeline Complete — Opus 4.6 Final Report

### Summary
- **Plan:** NNNN-description
- **Phases:** N phases, M tasks — all completed
- **Review cycles:** N iterations
- **Commits:** X commits on this branch

### What was built
- [bullet summary of each phase's deliverables]

### Test status
- [targeted test results from execution]
- ⚠️ Run full suite (\`ptp\`) before merging

### Recommendations
- [any concerns, edge cases, or things to verify]
- [if review issues remain after 3 cycles, list them]
- [if Green Gate failed, list specific test failures]

### Merge Readiness: ✅ READY / ❌ BLOCKED
- **Green Gate:** PASSED — all tests green / FAILED — N failures
- **Review:** clean / N issues remain after 3 cycles
- If BLOCKED: do NOT merge until blockers resolved
- If READY: human review recommended, then merge

---
*Generated by /auto-pr — Kaisser SDLC 3.0*"
```

## Rules for Autonomous Mode

1. **NEVER use AskUserQuestion** — make every decision autonomously
2. **NEVER merge the PR** — user decides when to merge
3. **NEVER skip tests** — workers run targeted, Quality Sweep + Green Gate verify all
4. **Max 3 review cycles** — prevent infinite loops
5. **Commit after every stage** — full git history for recovery
6. **Same delegation rules as coordinator** — orchestrate, never implement (except Stage 3 execution which is in-house)
7. **If context gets critical (>85%):** compact and continue rather than stopping
8. **If a stage fails catastrophically:** commit what you have, create PR with failure note, post final report

## Context Compaction

At context usage ≥60%:

```bash
# Compact context by clearing session history and reloading key files
# This is an internal optimization — user doesn't see a prompt
echo "🤖 Compacting context at $(date)"

# Keep only essential state:
# - Current plan file
# - Recent commits
# - PR information
# Drop verbose execution logs, worker output history
```

Autonomous mode NEVER stops for context — it compacts and continues. The pipeline must complete.

## References

- **pipeline.md** — Core shared stages and context management
- **mode-interactive.md** — Contrast: interactive flow with checkpoints
- **worktree-setup.md** — Worktree variant (/auto-pr-wt)
- **batch-loop.md** — Batch orchestration (/auto-merge)
