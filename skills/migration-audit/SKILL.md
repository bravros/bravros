---
name: migration-audit
description: Audit migration files for ordering issues, FK dependencies, timestamp collisions, and rollback coverage. Framework-agnostic with .deployed awareness.
trigger:
  - /migration-audit
  - audit migrations
  - check migration ordering
  - verify migrations
---

# Migration Audit: Verify Migration Health

Audit migration files across frameworks for ordering issues, FK dependency violations, timestamp collisions, missing indexes, and rollback coverage. Respects the `.deployed` convention to switch between auto-fix and report-only modes.

## Model Requirement

**Sonnet 4.6** — this skill performs mechanical/scripted operations that don't require deep reasoning.

## Overview

This skill inspects migration files at the source code level (no database connection required). It detects common issues that cause `migrate:fresh` failures or silent data corruption, then either fixes them automatically (greenfield projects) or reports them with suggested remediations (deployed projects).

Use `$ARGUMENTS` for project path, or the skill will default to the current working directory.

---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 1/5: Detect Framework

```bash
echo "🔧 migration-audit [1/5] detecting framework"
```

Check project root for these markers (first match wins):

- `artisan` file → **Laravel** (PHP, timestamp-prefixed migrations in `database/migrations/`)
- `manage.py` → **Django** (Python, numbered migrations in `*/migrations/`)
- `prisma/migrations/` directory → **Prisma** (TypeScript/JS)
- `db/migrate/` directory → **Rails** (Ruby, timestamp-prefixed)
- `knexfile.js` or `knexfile.ts` → **Knex** (JS, timestamp-prefixed in `migrations/`)
- `alembic/` directory → **Alembic/SQLAlchemy** (Python)

Report detected framework or "Unknown — manual audit required".

---

## Step 2/5: Check Deployment Status

```bash
echo "🔧 migration-audit [2/5] checking deployment status"
```

Check for `.deployed` file at project root:

- **File exists** → `PRODUCTION` mode (report-only, no auto-fixes)
- **File absent** → `GREENFIELD` mode (auto-fix allowed)

Report mode prominently at the start of output.

---

## Step 3/5: Run Checks

```bash
echo "🔧 migration-audit [3/5] running checks"
```

### Check 1: Timestamp/Number Collisions

Scan migration directory for files with identical numeric prefixes.

- **Laravel/Rails:** `ls database/migrations/ | awk -F_ '{print $1"_"$2"_"$3"_"$4}' | sort | uniq -d`
- **Django:** check for duplicate `0001_`, `0002_` etc. within the same app directory
- **Prisma/Knex:** check for duplicate numeric prefixes in `migrations/`

Severity: **HIGH** — causes unpredictable execution order

---

### Check 2: Foreign Key Ordering

For each migration file that creates a foreign key:

- **Laravel:** grep for `foreignId(`, `foreign(`, `references(`
- **Django:** grep for `ForeignKey(`
- **Prisma:** grep for `@relation`
- **Rails:** grep for `add_foreign_key`, `references`
- **Knex:** grep for `.references(`, `.foreign(`

Extract the parent table name from the FK reference. Verify the parent table's migration has an **earlier** timestamp/number than the child table's migration.

Severity: **HIGH** — causes `migrate:fresh` failure

---

### Check 3: Missing FK Indexes

- **Laravel:** `foreignId()` auto-creates an index, but `$table->unsignedBigInteger('x')` + `foreign('x')` does NOT — flag the latter if no matching `->index()` call exists in the same migration
- **Django:** `ForeignKey` with `db_index=False` explicitly set
- **Prisma:** `@relation` fields without `@@index` covering the FK column
- **Rails:** `add_foreign_key` without a corresponding `add_index`

Severity: **MEDIUM** — performance issue, not a crash

---

### Check 4: Duplicate Definitions

Scan all migration files for:

- Duplicate `Schema::create('table_name')` or `CREATE TABLE table_name` calls across files
- Duplicate column definitions (e.g., `$table->string('email')`) for the same table across separate migrations where no `Schema::drop` precedes the re-create

Severity: **HIGH** — migration will fail on second run

---

### Check 5: Rollback Coverage

- **Laravel:** check each migration's `down()` method — flag if empty or missing
- **Django:** check for `RunSQL` operations without `reverse_sql` argument
- **Rails:** `change` method is auto-reversible (OK); flag `up` method without a corresponding `down` method
- **Knex:** flag migrations without a `exports.down` function
- **Alembic:** flag `upgrade()` functions without a corresponding `downgrade()` body

Severity: **LOW** — doesn't break forward migrations, but blocks rollback

---

## Step 4/5: Auto-Fix (GREENFIELD mode only)

```bash
echo "🔧 migration-audit [4/5] auto-fixing migrations"
```

> **Only runs when `.deployed` is ABSENT.**

### Fix 1: Timestamp Reordering

If Check 2 found FK ordering issues:

1. Identify the correct order (parent table migration must come before child table migration)
2. Rename migration files to fix timestamp ordering — increment the child's timestamp by 1 second
3. Report each rename: `"Renamed: 2024_01_01_000000_create_links.php → 2024_01_01_000001_create_links.php"`

### Fix 2: Collision Resolution

If Check 1 found timestamp collisions:

1. Identify all files sharing the same timestamp prefix
2. Spread their timestamps: increment by 1 second per file
3. Ensure FK ordering is still valid after renaming (re-run Check 2 logic)

### Fix 3: Verify with migrate:fresh

After fixes, suggest: "Run `php artisan migrate:fresh --seed` to verify all migrations execute in order without errors."

### What auto-fix does NOT do

- Does not add missing indexes (create a new migration instead)
- Does not add rollback methods (manual task — requires understanding original intent)
- Does not merge duplicate definitions (manual task — may require data migration)
- Does not modify Django numbered migrations (renaming breaks Django's dependency graph)

---

## Step 5/5: Report-Only (PRODUCTION mode)

```bash
echo "🔧 migration-audit [5/5] generating report"
```

> **Runs when `.deployed` IS present. No files are modified.**

For each issue found, suggest the fix as a **new migration** (never modify existing ones):

- **FK ordering:** "Create a new migration that drops and recreates the FK constraint — ensure it runs after both parent and child tables exist"
- **Missing index:** "Create a new migration: `$table->index('column_name')`"
- **Duplicate definition:** "Review manually — may require a data migration to consolidate"
- **Missing rollback:** "Add a corresponding `down()` method manually — understand the original intent before writing it"

Output prominently: `"⚠️  PRODUCTION MODE — no files modified. Create new migrations to fix issues."`

---

## Output Format

```
Migration Audit Report
======================
Framework: Laravel | Mode: GREENFIELD (no .deployed)
Directory: database/migrations/ (47 files)

| # | Check | Severity | File | Issue | Fix |
|---|-------|----------|------|-------|-----|
| 1 | FK Order | HIGH | 2024_01_01_000000_create_links.php | References `users` table, but `users` migration has same timestamp | Rename to later timestamp |
| 2 | Collision | HIGH | 2024_01_01_000000_*.php | 3 files share timestamp | Spread timestamps |
| 3 | Rollback | LOW | 2024_03_15_create_logs.php | Empty down() method | Add rollback logic |

Summary: 2 HIGH, 0 MEDIUM, 1 LOW issues found.
[GREENFIELD] Auto-fixed 1 issue. Run `migrate:fresh --seed` to verify.
```

If no issues are found:

```
Migration Audit Report
======================
Framework: Laravel | Mode: PRODUCTION (.deployed present)
Directory: database/migrations/ (47 files)

All checks passed. No issues found.
```

---

## .deployed Convention

The `.deployed` file is a zero-byte marker at the project root that signals the project has been deployed to production at least once.

### How to create it
```bash
touch .deployed
git add .deployed && git commit -m "🚀 deploy: mark project as deployed to production"
```

### What it means
When `.deployed` exists, the project's database schema has been applied in production. Migration files that have already run CANNOT be safely modified.

### What it controls
See the tables below for which operations are blocked vs allowed.

### Operations BLOCKED by `.deployed`
| Operation | Why blocked |
|-----------|------------|
| `migrate:fresh` | Destroys all data — production databases cannot be wiped |
| Migration file renaming | Changes execution order — already-run migrations won't re-run |
| Migration file deletion | Breaks rollback chain — `migrate:rollback` will fail |
| `/squash-migrations` | Consolidation would skip already-executed individual migrations |
| `/migration-audit` auto-fix mode | File modifications unsafe on deployed schemas |

### Operations ALLOWED with `.deployed`
| Operation | Notes |
|-----------|-------|
| Creating new migrations | Always safe — appends to the migration chain |
| `/migration-audit` report mode | Read-only analysis, no file changes |
| Adding indexes via new migration | Safe — `$table->index('col')` in a new file |
| Adding columns via new migration | Safe — `$table->addColumn()` in a new file |
| Data migrations | Safe if idempotent |
