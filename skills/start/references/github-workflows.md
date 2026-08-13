# GitHub workflow templates — claude.yml + tests.yml

## Hard-won GitHub gotchas — do not deviate

- `issue_comment` workflows run from the **default branch**, not the PR branch — the file must exist on `main` before the trigger works.
- tests.yml needs `permissions: issues: write, pull-requests: write` or the report step fails with "Resource not accessible by integration".
- PR checkout MUST use `ref: refs/pull/${{ github.event.issue.number }}/head`.
- Vite/webpack projects whose tests render views need `npm ci && npm run build` before tests (else `ViteManifestNotFoundException`).

## claude.yml — @claude trigger

Write directly (never `cp`). Init mode: skip if it exists. Update mode: always overwrite with:

```yaml
name: Claude Code

on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]
  issues:
    types: [opened, assigned]
  pull_request_review:
    types: [submitted]

jobs:
  claude:
    if: |
      (github.event_name == 'issue_comment' && contains(github.event.comment.body, '@claude')) ||
      (github.event_name == 'pull_request_review_comment' && contains(github.event.comment.body, '@claude')) ||
      (github.event_name == 'pull_request_review' && contains(github.event.review.body, '@claude')) ||
      (github.event_name == 'issues' && (contains(github.event.issue.body, '@claude') || contains(github.event.issue.title, '@claude')))
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: read
      issues: read
      id-token: write
      actions: read
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4
        with:
          fetch-depth: 1

      - name: Run Claude Code
        id: claude
        uses: anthropics/claude-code-action@v1
        with:
          claude_code_oauth_token: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}
          additional_permissions: |
            actions: read
```

## tests.yml — @tests trigger (skip if it already exists)

On-demand test runner fired by commenting `@tests` on a PR — must mirror the project's
local test setup exactly. Trigger, permissions, checkout, and report are fixed; generate
the middle setup/install/build/test steps from the detected stack (read versions from
composer.json/.nvmrc/go.mod, DB config from phpunit.xml/.env.testing):

```yaml
name: Tests

on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]

permissions:
  contents: read
  pull-requests: write
  issues: write

jobs:
  tests:
    if: |
      (github.event_name == 'issue_comment' && contains(github.event.comment.body, '@tests')) ||
      (github.event_name == 'pull_request_review_comment' && contains(github.event.comment.body, '@tests'))
    runs-on: ubuntu-latest
    steps:
      - name: Checkout PR branch
        uses: actions/checkout@v4
        with:
          ref: refs/pull/${{ github.event.issue.number }}/head
          fetch-depth: 1

      # === DYNAMIC STEPS — generate from detected stack ===

      - name: Report result
        if: always()
        uses: actions/github-script@v7
        with:
          script: |
            const status = '${{ job.status }}' === 'success' ? '✅' : '❌';
            const message = `${status} **Tests ${('${{ job.status }}').toUpperCase()}** — <TEST_COMMAND>\n\n<RUNTIME_INFO> · ${new Date().toISOString()}`;
            const issueNumber = context.issue.number;
            await github.rest.issues.createComment({
              owner: context.repo.owner,
              repo: context.repo.repo,
              issue_number: issueNumber,
              body: message
            });
```

Replace `<TEST_COMMAND>` with the real test command and `<RUNTIME_INFO>` with runtime
details (e.g. `PHP 8.4 · SQLite in-memory`).
