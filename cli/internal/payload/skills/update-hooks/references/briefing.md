# Update Hooks: Refresh Git Hooks

Update git hooks in an existing project to the latest version from ~/.bravros/templates.

## Model Requirement

**Sonnet** — this skill performs mechanical/scripted operations that don't require deep reasoning.

## Process

### 1. Check Prerequisites
- Verify we're in a git repository
- Check if `.bravros/hooks/` exists (if not, suggest `/start`)

### 2. Update Hooks
```bash
bravros init
```

**Preserve graphify delegators.** If the repo has `.graphify` or `graphify-out/graph.json`, the `post-{merge,commit,checkout}` hooks are graphify refresh delegators — back them up first (`cp` to a temp dir) and restore them after the refresh; never overwrite them.

### 3. Output
```
Hooks updated!

Updated:
  .bravros/hooks/commit-msg - Latest commit validation rules
  .bravros/hooks/pre-push   - Prevents direct push to main

Git hooks path: core.hooksPath = .bravros/hooks
```

## Rules
- Overwrite bravros hook files (intentional update) — but never graphify's `post-*` delegators
- Do NOT touch .github/workflows or .planning
- Do NOT commit automatically — let user review

Use $ARGUMENTS for any additional context.
