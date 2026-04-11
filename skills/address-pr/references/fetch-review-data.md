# Fetching PR Review Data

## Primary Method (use this)

```bash
~/.claude/bin/bravros pr-review [PR_NUMBER]
```

Single command that fetches ALL review sources at once:
- PR info + diff stat
- Formal reviews (human reviewers — approved/changes requested)
- Inline code comments (with diff context line)
- Claude bot review (issue comments from `claude[bot]`)
- Human issue comments
- PR checks status

Auto-detects PR number from current branch if not provided.

## Fallback: Manual `gh` Commands

Use these only if `bravros pr-review` fails or you need a specific source.

### Get Repo Info
```bash
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
```

### Formal PR Reviews
```bash
gh pr view "$PR_NUMBER" --json reviews --jq '.reviews[] | {author: .author.login, state: .state, body: .body}'
```

### Inline Code Comments
```bash
gh api "repos/$REPO/pulls/$PR_NUMBER/comments" --jq '.[] | {author: .user.login, path: .path, line: .line, body: .body, diff_hunk: .diff_hunk}'
```

### Claude Bot Review
```bash
gh api "repos/$REPO/issues/$PR_NUMBER/comments" --jq '.[] | select(.user.login == "claude[bot]") | .body'
```

### Human Comments
```bash
gh api "repos/$REPO/issues/$PR_NUMBER/comments" --jq '.[] | select(.user.login != "claude[bot]") | {author: .user.login, body: .body}'
```

### PR Checks
```bash
gh pr checks "$PR_NUMBER" 2>&1 || true
```

## Categorizing Feedback

Process in this priority order (fix blockers first):

| Priority | Category | Description | Action |
|----------|----------|-------------|--------|
| 1 | **Blockers** | Changes requested, failing checks, security concerns | Must fix |
| 2 | **Code Issues** | Bugs, logic errors, missing validation, edge cases | Must fix |
| 3 | **Style/Convention** | Naming, formatting, pattern adherence | Fix |
| 4 | **Suggestions** | Nice-to-have improvements, optional refactors | User decides |
| 5 | **Questions** | Reviewer questions needing clarification | Respond on PR |

## Tips

- Always parse ALL inline comments regardless of approval status — approved reviews can still have inline comments that must be addressed
- If claude[bot] and a human reviewer conflict, the human reviewer's feedback takes priority
- Inline comments (on specific lines) are more actionable than general comments
- Always read the affected file before fixing — understand the reviewer's concern in context
