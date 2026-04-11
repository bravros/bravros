---
name: sync-upstream
description: >
  Pull upstream changes into any tenant/fork project while protecting project-specific
  assets (CSS, logos, images, colors, configs). Works across all projects — not tied to
  any single upstream repo. Use this skill whenever the user says "/sync-upstream",
  "sync from upstream", "pull upstream changes", "merge upstream", "update from upstream",
  or any request to sync a fork/tenant from its upstream source. Also triggers on
  "upstream sync", "tenant sync", "pull from parent repo", or "update from base".
  Handles protected file detection, dry run, conflict resolution, testing, and merge.
---

# Sync Upstream: Pull Changes into Tenant/Fork Projects

Pull upstream changes into the current project while protecting project-specific assets — CSS, logos, images, brand colors, configs. Works across any project with an upstream remote.

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## When to Use

- Upstream repo has new features, fixes, or dependencies
- Periodic sync to stay current with base project
- After upstream releases a new version
- Any fork/tenant that tracks a parent repo

## Critical Rules

- **NEVER overwrite project-specific files** without explicit confirmation
- **ALWAYS sync in a dedicated branch** — never directly on homolog or main
- **ALWAYS test after sync** before merging to homolog
- **Detect protected files automatically** — don't rely on hardcoded lists
- ALL git messages MUST use emoji format — NEVER include AI signatures

## Lessons Learned (Common Gotchas)

### 1. Unrelated Histories
If the tenant project was initialized independently (e.g., `laravel new` then added upstream remote), the first sync will fail with `fatal: refusing to merge unrelated histories`. Always add `--allow-unrelated-histories` on the first sync. This causes many add/add conflicts — resolve them all by accepting upstream (`git checkout --theirs`), then restore protected files from backup.

### 2. Check Which Upstream Branch is Most Updated
Don't assume `upstream/main` is the most updated. Compare `upstream/main` vs `upstream/homolog` commit counts and dates. Often `upstream/homolog` is ahead. Use the more updated branch.

### 3. Multiple Upstream Candidates
If the user has multiple potential upstream repos (e.g., scafold + afterpay both as forks), compare them before syncing. Check total commits, last commit date, and whether one is a fork of the other. Always sync from the most complete/updated source.

### 4. Locale Mismatch Causes Massive Test Failures
If upstream uses `pt_BR` translations and the tenant `.env` has `APP_LOCALE=en`, Blade `__()` calls may return arrays instead of strings, causing `htmlspecialchars(): Argument #1 must be of type string, array given` errors in EVERY view. After sync, check `.env` locale matches upstream's expected locale (check `config/app.php` defaults). This alone can cause 50+ test failures that look like view/component bugs.

### 5. Modified Initial Migrations Require migrate:fresh
If upstream modified the initial migration files (e.g., added columns to `create_users_table`), a regular `php artisan migrate` will NOT re-run them. The user MUST run `php artisan migrate:fresh --seed` in a separate terminal. Always check if initial migrations were changed in the diff.
> **macOS with Herd:** Prefix with `herd` (e.g., `herd php artisan migrate:fresh --seed`)

### 6. Always Clear All Caches After Sync
Stale compiled views and cached config cause phantom errors. Always run:
```bash
php artisan optimize:clear && php artisan view:clear
```
> **macOS with Herd:** `herd php artisan optimize:clear && herd php artisan view:clear`

### 7. Default ExampleTest.php
Laravel's default `tests/Feature/ExampleTest.php` expects `GET /` to return 200. If upstream adds auth (Fortify), `/` redirects to login (302). Delete this file if it exists and upstream doesn't have it.

---

## Step 1/10: Project Detection & Setup

```bash
echo "✨ sync-upstream [1/10] detecting project and upstream"
```

**Detect upstream remote:**
```bash
git remote -v | grep upstream
```
If no `upstream` remote → ask user for the upstream URL and add it:
```bash
git remote add upstream <url>
```

**Detect project identity:**
```bash
PROJECT_NAME=$(basename "$PWD")
CURRENT_BRANCH=$(git branch --show-current)
BASE_BRANCH=$(git config --get init.defaultBranch 2>/dev/null || echo "main")
```

**Check working tree:**
```bash
git status --porcelain
```
If dirty → STOP. Ask user to commit or stash first.

## Step 2/10: Detect Protected Files ⚠️ MANDATORY

```bash
echo "✨ sync-upstream [2/10] detecting project-specific files to protect"
```

Protected files are project-specific assets that should NEVER be overwritten by upstream. Detect them automatically:

### Auto-Detection Rules

**1. CSS & Styling** — project brand colors, custom themes:
```bash
# Find project-specific CSS (not from packages)
find resources/css resources/sass public/css -name "*.css" -o -name "*.scss" -o -name "*.less" 2>/dev/null | grep -v vendor | grep -v node_modules
# Look for tailwind config with custom colors
cat tailwind.config.js 2>/dev/null | grep -A20 "colors"
```

**2. Logos & Images** — project branding assets:
```bash
# Brand images (logos, favicons, og-images)
find public/images public/img resources/images -name "logo*" -o -name "favicon*" -o -name "og-*" -o -name "brand*" -o -name "icon*" 2>/dev/null
# Also check for tenant-specific images
find public -name "*.png" -o -name "*.svg" -o -name "*.ico" 2>/dev/null | head -30
```

**3. Configuration** — project-specific settings:
```bash
# Environment and config files
ls .env .env.* config/app.php config/mail.php config/services.php 2>/dev/null
# Package manifests with project-specific versions
FRAMEWORK=$(bravros meta --field stack.framework)
```

**4. Project-specific code** — helpers, overrides, custom implementations:
```bash
# Tenant-specific helpers or overrides
find app -name "helpers.php" -o -name "*Override*" -o -name "*Tenant*" -o -name "*Custom*" 2>/dev/null
# Check for a .upstream-protect file (explicit protection list)
cat .upstream-protect 2>/dev/null
```

**5. Check for `.upstream-protect` convention:**
If the project has a `.upstream-protect` file in the root, it contains a list of paths (one per line, glob patterns supported) that should always be protected during upstream syncs. This is the authoritative source — respect it above auto-detection.

Example `.upstream-protect`:
```
resources/css/tenant.css
resources/css/colors.css
public/images/logo*
public/favicon.ico
config/app.php
app/Helpers/helpers.php
tailwind.config.js
```

### Build Protection Manifest

Compile a list of all protected files and show the user:

```
🛡️  Protected Files (will NOT be overwritten by upstream):

CSS & Styling:
  - resources/css/tenant.css
  - resources/css/colors.css
  - tailwind.config.js (custom colors section)

Logos & Images:
  - public/images/logo.svg
  - public/images/logo-dark.svg
  - public/favicon.ico
  - public/images/og-image.png

Configuration:
  - .env (always protected)
  - config/app.php (app name, url, timezone)

Project Code:
  - app/Helpers/helpers.php

Source: .upstream-protect + auto-detected
```

Use `AskUserQuestion`:
- **"Looks correct — proceed"** → Continue
- **"Add more files"** → User lists additional files to protect
- **"Remove some"** → User indicates files that CAN be overwritten

## Step 3/10: Fetch & Preview (Dry Run)

```bash
echo "✨ sync-upstream [3/10] fetching upstream and previewing changes"
```

**Create sync branch:**
```bash
SYNC_BRANCH="sync/upstream-$(date +%Y%m%d)"
git fetch upstream
git checkout -b "$SYNC_BRANCH" origin/homolog 2>/dev/null || git checkout -b "$SYNC_BRANCH" "origin/$BASE_BRANCH"
```

**Determine which upstream branch is most updated:**
```bash
# Compare upstream branches — homolog is often ahead of main
for branch in upstream/main upstream/homolog upstream/master; do
    COUNT=$(git rev-list --count HEAD.."$branch" 2>/dev/null) && \
    DATE=$(git log -1 --format=%ci "$branch" 2>/dev/null) && \
    echo "$branch: $COUNT commits ahead (last: $DATE)"
done
```
Use the branch with the most commits ahead. Ask user if unclear.

**Preview changes:**
```bash
# Show what upstream has that we don't (use the selected branch)
git log --oneline HEAD..$UPSTREAM_BRANCH
# Show file-level diff
git diff --stat HEAD..$UPSTREAM_BRANCH
```

**Check for protected file conflicts:**
```bash
# Get list of files changed upstream
UPSTREAM_BRANCH=$(git branch -r | grep "upstream/" | head -1 | tr -d ' ')
CHANGED_FILES=$(git diff --name-only HEAD.."$UPSTREAM_BRANCH")

# Cross-reference with protected files
echo "$CHANGED_FILES" | while read file; do
    # Check against protection list
    grep -q "$file" .upstream-protect 2>/dev/null && echo "⚠️  PROTECTED: $file"
done
```

Show summary:
```
📋 Upstream Changes Preview:
━━━━━━━━━━━━━━━━━━━━━━━━━━━

Upstream commits: 23 new commits
Files changed: 47 files

⚠️  Protected file conflicts (will need manual resolution):
  - tailwind.config.js (upstream changed colors section)
  - app/Helpers/helpers.php (upstream added new functions)

Safe changes (auto-merge):
  - 45 other files with no protection conflicts
```

Use `AskUserQuestion`:
- **"Proceed with merge"** → Continue to Step 4
- **"Show me the protected file diffs"** → Show detailed diffs for conflicting protected files
- **"Abort"** → Delete sync branch and stop

## Step 4/10: Merge with Protection

```bash
echo "✨ sync-upstream [4/10] merging upstream with protection"
```

**Backup protected files:**
```bash
BACKUP_DIR="/tmp/upstream-protect-$(date +%s)"
mkdir -p "$BACKUP_DIR"
# Copy each protected file
while IFS= read -r file; do
    if [ -f "$file" ]; then
        mkdir -p "$BACKUP_DIR/$(dirname "$file")"
        cp "$file" "$BACKUP_DIR/$file"
    fi
done < <(cat .upstream-protect 2>/dev/null; echo "")
```

**Merge upstream:**
```bash
UPSTREAM_BRANCH=$(git branch -r | grep "upstream/main" && echo "upstream/main" || echo "upstream/master")
git merge "$UPSTREAM_BRANCH" -m "🔀 merge: sync upstream $(date +%Y-%m-%d)" || true
```

**If merge fails with `refusing to merge unrelated histories`:**
This happens on first sync when repos were independently initialized. Re-run with:
```bash
git merge "$UPSTREAM_BRANCH" --allow-unrelated-histories -m "🔀 merge: sync upstream $(date +%Y-%m-%d)" || true
```
This will cause many add/add conflicts. For non-protected files, resolve all by accepting upstream:
```bash
git diff --name-only --diff-filter=U | while read file; do
    git checkout --theirs "$file"
    git add "$file"
done
```
Then restore protected files from backup (see below).

**Handle conflicts:**

If merge conflicts occur:

1. **Protected files** — restore from backup, keep ours:
   ```bash
   while IFS= read -r file; do
       if [ -f "$BACKUP_DIR/$file" ]; then
           cp "$BACKUP_DIR/$file" "$file"
           git add "$file"
       fi
   done < <(cat .upstream-protect 2>/dev/null)
   ```

2. **Non-protected files** — show conflicts and help resolve:
   ```bash
   git diff --name-only --diff-filter=U
   ```
   For each conflicted file:
   - Show the conflict markers
   - Suggest resolution (usually accept upstream for non-protected files)
   - Let user decide

3. **Special case: helpers.php or similar shared files:**
   If a protected file has BOTH upstream additions AND tenant-specific code → **manual merge required**:
   - Show upstream additions (new functions/methods)
   - Show tenant-specific code (custom functions)
   - Merge both together — keep tenant code, add upstream additions
   - Use `AskUserQuestion` to confirm the merged result

4. **Complete merge:**
   ```bash
   git merge --continue
   ```

**Restore any remaining protected files that were silently overwritten:**
```bash
while IFS= read -r file; do
    if [ -f "$BACKUP_DIR/$file" ]; then
        if ! diff -q "$file" "$BACKUP_DIR/$file" > /dev/null 2>&1; then
            echo "🛡️ Restoring protected: $file"
            cp "$BACKUP_DIR/$file" "$file"
            git add "$file"
        fi
    fi
done < <(cat .upstream-protect 2>/dev/null)
git diff --cached --quiet || git commit -m "🛡️ chore: restore protected tenant files after upstream sync"
```

## Step 5/10: Post-Merge Setup

```bash
echo "✨ sync-upstream [5/10] running post-merge setup"
```

**Dependency updates:**
```bash
# PHP dependencies
composer install 2>/dev/null
# Node dependencies
npm install
# Build assets
npm run build
# Clear ALL caches (stale compiled views cause phantom errors)
php artisan optimize:clear && php artisan view:clear
```
> **macOS with Herd:** Use `herd composer install` and `herd php artisan optimize:clear && herd php artisan view:clear`

**Check locale matches upstream:**
```bash
# Check what locale upstream expects (config/app.php defaults)
grep "locale" config/app.php | head -3
# Check current .env locale
grep "APP_LOCALE" .env
```
If upstream defaults to `pt_BR` but `.env` has `en`, update `.env`:
```
APP_LOCALE=pt_BR
APP_FALLBACK_LOCALE=pt_BR
APP_FAKER_LOCALE=pt_BR
```
**WARNING:** Locale mismatch is the #1 cause of mass test failures after upstream sync. `__()` calls return arrays instead of strings with wrong locale, causing `htmlspecialchars()` errors everywhere.

**Database — check if initial migrations were modified:**
```bash
# If upstream changed the initial create_users_table or similar early migrations
git diff --name-only HEAD~1 | grep "0001_01_01"
```
If initial migrations were modified → ask user to run `herd php artisan migrate:fresh --seed` in a separate terminal (regular `migrate` won't re-run already-executed migrations).

Otherwise run normal migrate:
```bash
php artisan migrate --no-interaction
```
> **macOS with Herd:** `herd php artisan migrate --no-interaction`

**Delete stale default tests:**
```bash
# ExampleTest.php expects GET / → 200, but auth apps redirect to login (302)
[ -f tests/Feature/ExampleTest.php ] && rm tests/Feature/ExampleTest.php && git add tests/Feature/ExampleTest.php
```

**Verify protected files are intact:**
```bash
echo "🛡️ Verifying protected files..."
ISSUES=0
while IFS= read -r file; do
    if [ -f "$BACKUP_DIR/$file" ] && [ -f "$file" ]; then
        if ! diff -q "$file" "$BACKUP_DIR/$file" > /dev/null 2>&1; then
            echo "❌ CHANGED: $file"
            ISSUES=$((ISSUES + 1))
        else
            echo "✅ OK: $file"
        fi
    fi
done < <(cat .upstream-protect 2>/dev/null)

if [ "$ISSUES" -gt 0 ]; then
    echo "⚠️  $ISSUES protected file(s) were modified — review required"
fi
```

## Step 6/10: Test ⚠️ MANDATORY

```bash
echo "✨ sync-upstream [6/10] testing after sync"
```

**Run targeted tests first** (fast feedback):
```bash
# Find what test files were affected by the merge
CHANGED_TEST_FILES=$(git diff --name-only HEAD~2 | grep -E "tests/.*Test\.php$")
if [ -n "$CHANGED_TEST_FILES" ]; then
    echo "$CHANGED_TEST_FILES" | xargs vendor/bin/pest --parallel --processes=10
fi
```

**Then run full suite:**
```bash
./vendor/bin/pest --parallel --processes=10
```

If tests fail:
1. Check if failure is in tenant-specific code (our problem) or upstream code (upstream's problem)
2. If tenant-specific: fix and commit with `🐛 fix: resolve upstream sync conflict in <area>`
3. If upstream: note it, decide whether to proceed or revert

**Visual check** — use `AskUserQuestion`:
- "Run `npm run dev` and check the frontend. Do colors, logos, and layout look correct?"
- Let user verify brand identity wasn't broken

## Step 7/10: Push Sync Branch

```bash
echo "✨ sync-upstream [7/10] pushing sync branch"
git push origin "$SYNC_BRANCH"
```

## Step 8/10: Merge to Homolog

```bash
echo "✨ sync-upstream [8/10] merging to homolog"
```

Use `AskUserQuestion`:
- **"Tests pass, merge to homolog"** → Create PR or direct merge:
  ```bash
  git checkout homolog
  git merge "$SYNC_BRANCH" -m "🔀 merge: upstream sync $(date +%Y-%m-%d) into homolog"
  git push origin homolog
  git branch -d "$SYNC_BRANCH"
  ```
- **"Need more testing"** → Keep sync branch, user tests manually
- **"Abort sync"** → Delete sync branch:
  ```bash
  git checkout homolog
  git branch -D "$SYNC_BRANCH"
  ```

## Step 9/10: Merge to Main (Optional)

```bash
echo "✨ sync-upstream [9/10] merging to main (optional)"
```

Use `AskUserQuestion`:
- **"Also merge homolog → main"** → Create PR from homolog to main and merge
- **"Not yet"** → Stop here, user deploys homolog first

## Step 10/10: Commit Sync Record

```bash
echo "✨ sync-upstream [10/10] committing sync record"
git add .planning/ && git commit -m "🔀 sync: upstream $(date +%Y-%m-%d) — N commits merged, M conflicts resolved"
```

---

## `.upstream-protect` File

Projects should maintain a `.upstream-protect` file in the repo root listing all files that must never be overwritten during upstream syncs. One path per line, comments with `#`:

```
# Brand & Visual Identity
resources/css/tenant.css
resources/css/colors.css
resources/sass/_variables.scss
tailwind.config.js
public/images/logo*
public/images/brand/*
public/favicon.ico
public/images/og-image.png

# Project Configuration
config/app.php
config/mail.php

# Tenant-Specific Code
app/Helpers/helpers.php
app/Providers/TenantServiceProvider.php

# Environment (always protected, listed for clarity)
.env
.env.*
```

**Tip:** Run `/sync-upstream --init` to auto-generate this file from detected project assets.

## Flags

- `--dry-run` / `-d`: Preview only — show what upstream has, don't merge
- `--init`: Generate `.upstream-protect` file from auto-detected project assets
- `--force` / `-f`: Skip confirmations (still protects files, just doesn't ask)
- `--no-test`: Skip test step (use when you'll test manually)
- `--branch <name>`: Use specific upstream branch (default: upstream/main or upstream/master)

Use $ARGUMENTS for flags or additional context about the sync.
