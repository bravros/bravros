---
name: start
description: >
  Initialize a new project with GitHub Actions, git hooks, planning structure, and stack-aware CLAUDE.md.
  Detects tech stack (Laravel, Next.js, Node.js, Python, Go, Expo) and dynamically generates CLAUDE.md.
  Use this skill whenever the user says "/start", "initialize project", "setup project",
  "start new project", or any request to set up the standard development workflow in a repo.
  Also triggers on "project init", "bootstrap project", "setup hooks", or "initialize repo".
  Sets up everything: claude.yml action, commit hooks, .planning/, stack-aware CLAUDE.md, sync-db.sh, homolog branch.
  Do NOT trigger for updating existing hooks (/update-hooks) or syncing workflow files (/workflow-sync).
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Start: Initialize Project

Initialize a new project with GitHub Actions, git hooks, planning structure, CLAUDE.md, and branch setup.

No questions asked — installs the standard setup every time.

## Model Requirement

**Sonnet 4.6** — this skill performs mechanical/scripted operations that don't require deep reasoning.

## What It Sets Up

1. **`.githooks/`** — commit-msg (validates emoji format, blocks AI signatures) + pre-push (blocks main)
2. **`.planning/`** — Directory for local planning files
3. **`CLAUDE.md`** — Project-level instructions (auto-detected for stack)
4. **`sync-db.sh`** — Production database sync script (MySQL/PostgreSQL projects only)
5. **`.gitignore` entries** — .db-sync.env, database/backups/
6. **`homolog` branch** — Staging branch if missing (created BEFORE workflows)
7. **`.github/workflows/claude.yml`** — GitHub Action for @claude PR review mentions (only if homolog branch exists)
7b. **`.github/workflows/tests.yml`** — On-demand test runner triggered by `@tests` comment on PRs (only if homolog branch exists)

## Process

## Step 1/5: Verify Git Repository

```bash
echo "✨ start [1/5] Verifying git repository"
git rev-parse --is-inside-work-tree || { echo "Not a git repository"; exit 1; }
```

## Step 2/5: Detect Tech Stack

Before copying templates, detect the project's tech stack to customize CLAUDE.md, sync-db.sh, and tests.yml appropriately.

**Detection logic:**
```
if composer.json exists AND has "laravel/framework" → Laravel (TALL stack)
if package.json has "next" in dependencies → Next.js
if package.json has "nuxt" in dependencies → Nuxt
if package.json has "react-native" or "expo" → React Native / Expo
if package.json exists (no framework match) → Generic Node.js
if go.mod exists → Go
if requirements.txt or pyproject.toml exists → Python
else → Generic (minimal CLAUDE.md)
```

Read the relevant package manager file (`composer.json`, `package.json`, `go.mod`, etc.) to determine:
- **Framework**: Laravel, Next.js, Nuxt, Expo, etc.
- **Test runner**: Pest, PHPUnit, Jest, Vitest, pytest, Go test, etc.
- **Database ORM**: Eloquent, Prisma, Drizzle, etc.
- **Asset bundler**: Vite, Webpack, Turbopack, etc.
- **Local URL pattern**: `*.test` (Herd), `localhost:3000` (Node), etc.

Store these values — they'll be used in steps 3, 4, 6, and 8.

```bash
echo "✨ start [2/5] Detecting tech stack"
```

Set `DETECTED_STACK` to one of: `laravel`, `nextjs`, `expo`, `nodejs`, `python`, `go`, `generic`. This variable is used in Step 3 to select the correct CLAUDE.md template.

Detection priority for `package.json` projects:
- `"next"` in dependencies → `nextjs`
- `"react-native"` or `"expo"` in dependencies → `expo`
- `"nuxt"` in dependencies → `nodejs` (uses similar Node.js patterns; Nuxt-specific CLAUDE.md can be added later if needed)
- No framework match → `nodejs`

For full detection patterns (file markers, lock file parsing, framework version detection), refer to `~/.claude/skills/context/references/stack-detection.md`.

## Step 3/5: Copy Templates + Generate Stack-Aware CLAUDE.md

```bash
bravros init
```

**CLAUDE.md generation** — Use `DETECTED_STACK` (set in Step 2) to choose the correct approach:

The global CLAUDE.md (~/.claude/CLAUDE.md) contains universal rules. The project-level CLAUDE.md generated here contains stack-specific commands and patterns.

- **Laravel** (`DETECTED_STACK=laravel`): Fast path — copy `~/.claude/templates/CLAUDE.md` and replace placeholders. This is backward compatible and unchanged.
- **All other stacks** (nextjs, nodejs, python, go, expo, generic): Read `~/.claude/skills/start/references/claudemd-templates.md` and use the section matching `DETECTED_STACK` as the basis for generating CLAUDE.md. Do NOT copy `~/.claude/templates/CLAUDE.md` for non-Laravel projects — it will produce incorrect framework names, test commands, and URLs.

> Note: `~/.claude/templates/CLAUDE.md` remains the Laravel-specific fast path and must not be modified. Non-Laravel CLAUDE.md files are always generated from scratch using the reference templates.

```bash
echo "✨ start [3/5] Copying templates and generating CLAUDE.md"
```

```bash
# For Laravel projects only (DETECTED_STACK=laravel):
cp -n ~/.claude/templates/CLAUDE.md CLAUDE.md 2>/dev/null || true
PROJECT_DIR=$(basename "$(pwd)")
if [[ "$OSTYPE" == "darwin"* ]]; then
  sed -i '' "s/\[Project Name\]/$PROJECT_DIR/g; s/\[project-folder\]/$PROJECT_DIR/g" CLAUDE.md 2>/dev/null || true
else
  sed -i "s/\[Project Name\]/$PROJECT_DIR/g; s/\[project-folder\]/$PROJECT_DIR/g" CLAUDE.md 2>/dev/null || true
fi

# For non-Laravel projects (DETECTED_STACK=nextjs|nodejs|python|go|expo|generic):
# 1. Read the matching section from ~/.claude/skills/start/references/claudemd-templates.md
# 2. Fill in [brackets] with detected values (framework version, DB driver, test runner, port, etc.)
# 3. Write the result directly to CLAUDE.md — do NOT use the Laravel template as a base.
```

## Step 4/5: Configure Hooks, Directories, and Branches

```bash
echo "✨ start [4/5] Configuring git hooks, directories, and branches"
```

Only copy `sync-db.sh` if the project uses a relational database that benefits from production sync:

```bash
# Laravel (Eloquent + MySQL) — copy as-is
cp -n ~/.claude/templates/sync-db.sh sync-db.sh 2>/dev/null || true
test -f sync-db.sh && chmod +x sync-db.sh
cp -n ~/.claude/templates/.db-sync.env.example .db-sync.env.example 2>/dev/null || true
mkdir -p database/backups
```

**For non-Laravel projects with a DB:**
- If Prisma → copy sync-db.sh but replace the post-restore command: `herd php artisan migrate --force` → `npx prisma migrate deploy`
- If Drizzle → replace with `npx drizzle-kit push`
- Remove `herd php artisan optimize:clear` line for non-Laravel projects

**For projects without a relational DB** (e.g., Expo with AsyncStorage, static sites): skip sync-db.sh entirely.

### 5. Configure Git Hooks
```bash
git config core.hooksPath .githooks
```

### 6. Initialize Planning Directory
```bash
mkdir -p .planning/backlog/archive
```

### 6b. Obsidian Integration (optional)

If the user has the Obsidian vault set up, create a symlink so this project's
`.planning/` directory appears inside the vault's `coding/` folder.

```bash
VAULT="$HOME/Library/Mobile Documents/com~apple~CloudDocs/Sync/obsidian"
PROJECT=$(basename "$(pwd)")

if [ -d "$VAULT" ]; then
    # Run /obsidian-setup add <project> to create the vault symlink
    # This is idempotent — safe to call even if symlink already exists
    /obsidian-setup add "$PROJECT" 2>/dev/null || true
else
    # Vault not found — skip silently
    true
fi
```

> If `/obsidian-setup` is not available or the vault doesn't exist, this step
> is skipped silently. The rest of the setup continues normally.

### 6c. Create `.bravros.yml` (per-project SDLC config)

If `.bravros.yml` doesn't exist, use `AskUserQuestion` to ask the user:
- **Question:** "What is your staging/integration branch name?" (default: "homolog")

Then create the file:

```bash
# Only if .bravros.yml doesn't exist
if [ ! -f .bravros.yml ]; then
    echo "staging_branch: ${STAGING_BRANCH:-homolog}" > .bravros.yml
fi
```

This config is read by `bravros audit` to dynamically detect the staging branch for push/merge enforcement rules.

### 7. Update .gitignore
```bash
for entry in ".db-sync.env" "database/backups/"; do
    grep -qxF "$entry" .gitignore || echo "$entry" >> .gitignore
done
```

### 8. Create Homolog Branch (if missing) — MUST happen BEFORE workflows

This step MUST run before creating GitHub workflows (step 9). The audit hook blocks workflow file creation if the homolog branch doesn't exist yet.

```bash
if ! git show-ref --verify --quiet refs/heads/homolog && ! git show-ref --verify --quiet refs/remotes/origin/homolog; then
    git checkout -b homolog
    git push -u origin homolog 2>/dev/null || echo "No origin — local only"
    git checkout main 2>/dev/null || git checkout master
fi
```

## Step 5/5: Create GitHub Workflows and Report

```bash
echo "✨ start [5/5] Creating GitHub workflows and reporting results"
```

Only create this workflow if the project has or will have a `homolog` branch (i.e., it uses the homolog → main PR flow). If the project doesn't use homolog, skip this step and step 3b entirely — no workflows needed.

Do NOT use `cp` for claude.yml. Instead, write it directly to ensure it is always functional.
If `.github/workflows/claude.yml` already exists, skip this step (no clobber).

If it does not exist, create `.github/workflows/claude.yml` with this exact content:

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

This guarantees the action uses OAuth (not API key), triggers on all `@claude` mentions (PR comments, review comments, issues, PR reviews), and includes proper checkout and permissions — matching the template exactly.

After creating claude.yml, remove any starter kit workflow files (keep only our managed ones):

```bash
find .github/workflows -name '*.yml' ! -name 'claude.yml' ! -name 'tests.yml' -delete 2>/dev/null
```

### 9b. Create tests.yml GitHub Action (only for projects with homolog branch)

Only create this workflow if the project has or will have a `homolog` branch (i.e., it uses the homolog → main PR flow). If the project doesn't use homolog, skip this step entirely.

If `.github/workflows/tests.yml` already exists, skip this step (no clobber).

This workflow runs tests on demand — triggered by commenting `@tests` on a PR. It must mirror the project's local test setup exactly so CI matches local results.

**IMPORTANT lessons learned:**
- The workflow file MUST exist on the `main` branch to trigger via `issue_comment` — GitHub runs `issue_comment` workflows from the default branch, not the PR branch
- The `permissions` block is REQUIRED at the job or workflow level — without `issues: write` and `pull-requests: write`, the report step fails with "Resource not accessible by integration"
- PR checkout MUST use `refs/pull/${{ github.event.issue.number }}/head` — not a conditional format expression
- If the project uses Vite/webpack, you MUST run `npm ci && npm run build` before tests — otherwise views that use `@vite()` will fail with `ViteManifestNotFoundException`

**Detection steps — read the project before generating the workflow:**

1. **Language/runtime:** Check for `composer.json` (PHP), `package.json` (Node/JS), `Gemfile` (Ruby), `requirements.txt`/`pyproject.toml` (Python), `go.mod` (Go)
2. **PHP version:** From `composer.json` `require.php` field (e.g., `^8.4` → use `8.4`)
3. **Node version:** If `package.json` exists, check `.nvmrc` or `engines.node` — default to `22` if not specified
4. **Test runner:** Check what's installed — `vendor/bin/pest` (Pest), `vendor/bin/phpunit` (PHPUnit), `jest`/`vitest` in package.json, `pytest` (Python), etc.
5. **Test command:** If Pest, use `./vendor/bin/pest --parallel --processes=10`. If PHPUnit, use `./vendor/bin/phpunit`. If Jest, use `npx jest`. Match what the project uses locally.
6. **Database:** Read `phpunit.xml` or `.env.testing` for DB config (SQLite in-memory, MySQL, etc.)
7. **Asset build:** If `vite.config.js`/`webpack.mix.js` exists AND tests render views, include `npm ci && npm run build`
8. **Environment:** Check for `.env.example` and framework-specific setup (`php artisan key:generate`, etc.)

**Workflow structure — fixed parts + dynamic steps:**

The trigger, permissions, PR checkout, and report step are always the same. Only the middle steps (setup, install, build, test) change per project. Generate the full workflow with all steps filled in.

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

      # === DYNAMIC STEPS — generate based on detected stack ===
      #
      # LARAVEL (Pest) example:
      #   - uses: shivammathur/setup-php@v2
      #     with: { php-version: '8.4', coverage: none }
      #   - run: composer install --no-interaction --prefer-dist
      #   - run: cp .env.example .env && php artisan key:generate
      #   - run: npm ci && npm run build    # if Vite is used
      #   - run: ./vendor/bin/pest --parallel --processes=10
      #     env: { DB_CONNECTION: sqlite, DB_DATABASE: ':memory:' }
      #
      # NEXT.JS (Jest/Vitest) example:
      #   - uses: actions/setup-node@v4
      #     with: { node-version: '22' }    # or read from .nvmrc
      #   - run: npm ci
      #   - run: npx jest --ci              # or: npx vitest run
      #     env: { DATABASE_URL: 'file:./test.db' }  # if Prisma
      #
      # NEXT.JS (Playwright) example:
      #   - uses: actions/setup-node@v4
      #     with: { node-version: '22' }
      #   - run: npm ci
      #   - run: npx playwright install --with-deps
      #   - run: npx playwright test
      #
      # PYTHON (pytest) example:
      #   - uses: actions/setup-python@v5
      #     with: { python-version: '3.12' }
      #   - run: pip install -r requirements.txt
      #   - run: pytest
      #
      # GO example:
      #   - uses: actions/setup-go@v5
      #     with: { go-version-file: 'go.mod' }
      #   - run: go test ./...

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

Replace `<TEST_COMMAND>` with the actual test command (e.g., `pest --parallel --processes=10`) and `<RUNTIME_INFO>` with detected runtime details (e.g., `PHP 8.4 · SQLite in-memory`).

### 10. Output
Report what was created/skipped and next steps.

## Safety
- Use `cp -n` (no clobber) to avoid overwriting existing files
- Don't commit automatically — let user review first
- Only create homolog if it doesn't exist
- All user interactions MUST use `AskUserQuestion` tool, never plain text questions

Use $ARGUMENTS for any additional context.