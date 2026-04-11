---
name: push
description: >
  Push the current branch to remote with branch safety checks.
  Use this skill whenever the user says "/push", "push this", "push to remote",
  "push branch", or any request to push code to the remote repository.
  Also triggers on "push my code", "push the branch", or "send it to remote".
  Does NOT trigger on "push my changes" — use /ship if uncommitted changes exist.
  Blocks pushes to main/master — only homolog and feature branches are allowed.
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Push: Push Branch to Remote

Push the current branch to remote.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## Pre-flight Checks

```bash
BRANCH=$(git branch --show-current)

# Block pushes to main/master
if [ "$BRANCH" = "main" ] || [ "$BRANCH" = "master" ]; then
    echo "ERROR: Cannot push directly to '$BRANCH'. Use a PR from homolog → $BRANCH."
    exit 1
fi

# Warn if working tree is dirty
if [ -n "$(git status --porcelain)" ]; then
    echo "⚠️ Working tree has uncommitted changes. Use /ship to commit and push, or /commit first."
    exit 1
fi
```

`homolog` is pushable directly (plan commits, hotfixes). Only `main` requires a PR.

## Push

```bash
git push -u origin $(git branch --show-current)
```

If push is rejected due to remote changes:
```
❌ Push rejected — remote has new commits. Pull or rebase first:
git pull --rebase origin <branch>
```

## Rules
- Do NOT commit anything — push only.
- Do NOT create a PR — push only.
- Do NOT force push unless the user explicitly requests it.
- Keep output minimal — just confirm the push result:
  ```
  ✅ Pushed to origin/<branch>
  ```