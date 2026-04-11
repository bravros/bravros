---
name: hotfix
description: >
  Emergency hotfix deployment: commit, push to homolog, create PR to main, and merge.
  Use this skill when the user says "/hotfix", "emergency push", "hotfix to production",
  "push to homolog and merge to main", "urgent deploy", "fast push to main",
  "deploy hotfix", or any request for an emergency/urgent deployment bypassing the normal
  /plan → /pr → /finish workflow. This is the fast lane: commit → push → PR → merge.
---

# Hotfix Push: Emergency Deploy to Main

Fast-lane deployment for urgent fixes. Bypasses normal plan workflow.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

**Flow:** commit → push to homolog → PR homolog→main → merge

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## When to Use

- Production bugs that need immediate fix
- Urgent security patches
- Quick important changes that can't wait for full plan cycle
- User explicitly asks for emergency/hotfix deploy

## Process

### Step 1/7: Pre-flight Checks

```bash
echo "🔀 hotfix [1/7] starting emergency hotfix"
BRANCH=$(git branch --show-current)
BASE=$(~/.claude/bin/bravros meta --field base_branch)
```

**Verify:**
- Current branch is NOT main or master (block if so)
- There are changes to commit (staged or unstaged)
- Tests pass for changed files (run targeted tests only)

### Step 2/7: Commit Changes

Detect stack language, run the appropriate formatter on staged files, then commit:

```bash
echo "🔀 hotfix [2/7] committing changes"
# Stage all changes (review what's being added)
git add -A
# Verify no unrelated files are staged — if unrelated files appear, unstage them

# Stack-aware formatter detection
LANGUAGE=$(~/.claude/bin/bravros meta --field stack.language 2>/dev/null)
case "$LANGUAGE" in
  php) FORMATTER="vendor/bin/pint --dirty" ;;
  node) FORMATTER="npx prettier --write ." ;;
  python) FORMATTER="ruff format ." ;;
  go) FORMATTER="gofmt -w ." ;;
  *) FORMATTER="" ;;
esac

# Run formatter on staged files
if [ -n "$FORMATTER" ]; then
    $FORMATTER 2>/dev/null && git add -u
fi

# Commit with hotfix emoji
git commit -m "🩹 hotfix: $DESCRIPTION"
```

### Step 3/7: Push to Homolog

```bash
echo "🔀 hotfix [3/7] pushing to homolog"
# If on feature branch, push there first
git push origin "$BRANCH"

# If branch is not homolog, merge into homolog
if [ "$BRANCH" != "homolog" ]; then
    git checkout homolog
    git pull origin homolog
    git merge "$BRANCH" -m "🔀 merge: $BRANCH into homolog (hotfix)"
    git push origin homolog
    git checkout "$BRANCH"
fi
```

### Step 4/7: Create PR: homolog → main

**GitHub Issue Detection:** If `$ARGUMENTS` contains a GitHub issue reference (e.g., `/hotfix #42 fix login timeout`, `/hotfix issue 42 fix crash`), parse the issue number and include `Closes #N` in the PR body.

```bash
echo "🔀 hotfix [4/7] creating PR homolog → main"
# Parse issue number from $ARGUMENTS if present
ISSUE_REF=""
ISSUE_NUM=$(echo "$ARGUMENTS" | grep -oE '(#|issue ?)([0-9]+)' | grep -oE '[0-9]+' | head -1)
if [ -n "$ISSUE_NUM" ]; then
    ISSUE_REF="Closes #$ISSUE_NUM"
fi

# Create PR
gh pr create \
    --base main \
    --head homolog \
    --title "🩹 hotfix: $DESCRIPTION" \
    --body "## Emergency Hotfix

**What:** $DESCRIPTION
**Why:** Urgent fix requiring immediate deployment
**Branch:** $BRANCH → homolog → main

### Changes
$(git log main..homolog --oneline)

### Verification
- [ ] Targeted tests pass
- [ ] Manual smoke test completed

### References
${ISSUE_REF}
"
```

### Step 5/7: Merge PR

```bash
echo "🔀 hotfix [5/7] merging PR"
PR_NUMBER=$(gh pr view homolog --json number -q .number)
bravros merge-pr "$PR_NUMBER"
```

The CLI reads `.bravros.yml` for permanent branches and never deletes homolog/main/staging/develop. Returns JSON: `{"pr", "state", "branch_deleted", "conflicts_resolved"}`.

### Step 6/7: Sync Homolog from Main

After the PR merges to main, pull those changes back into homolog to prevent merge conflicts on the next homolog→main merge:

```bash
echo "🔀 hotfix [6/7] syncing homolog from main"
git checkout homolog
git pull origin homolog
git merge main -m "🔀 merge: sync hotfix from main"
# Clean orphan -todo.md files left by squash-merge losing rename history
~/.claude/bin/bravros clean-todos
git add .planning/ 2>/dev/null
git diff --cached --quiet || git commit -m "🧹 chore: remove orphan plan todo files after branch sync"
git push origin homolog
git checkout "$BRANCH"
```

This keeps homolog and main in sync so the next feature branch PR won't have conflicts from the hotfix commits.

### Step 7/7: Commit Plan Update (if applicable)

```bash
echo "🔀 hotfix [7/7] committing plan update"
git add .planning/ && git commit -m "🩹 hotfix: plan update for $DESCRIPTION" 2>/dev/null || true
```

## Rules

- **ALWAYS review staged files** — unstage anything unrelated to the hotfix before committing
- **ALWAYS run targeted tests before pushing** — even hotfixes get tested
- **NEVER skip the PR** — main is protected, always go through homolog → main PR
- **NEVER delete the homolog branch** after merge
- **FULLY AUTOMATIC** — push → PR → merge, NO questions asked. This is the emergency lane.
- If tests fail, STOP and use `AskUserQuestion` — don't push broken code
- Use `🩹 hotfix:` commit format
- All user interactions MUST use `AskUserQuestion` tool, never plain text questions

## Arguments

Use $ARGUMENTS as the hotfix description. If empty, ask the user what the hotfix is for.
