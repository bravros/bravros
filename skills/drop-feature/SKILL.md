---
name: drop-feature
description: >
  Clean removal of an entire feature — searches all references and removes everything.
  Use this skill whenever the user says "/drop-feature", "drop feature", "remove feature",
  "rip out", "delete the X feature", "clean up dead feature", "remove all traces of X",
  or any request to completely remove a feature from the codebase. Also triggers on
  "kill feature", "nuke feature", "feature cleanup", "remove dropped feature",
  "strip out feature", "gut the feature", "remove everything related to X".
---

# Drop Feature: Clean Removal of an Entire Feature

Find and remove ALL traces of a feature from the codebase — routes, controllers, models, views, services, tests, factories, notifications, sidebar links, config entries, and more.

```
/drop-feature <feature-name>
  → Discover all references → confirm scope → remove everything
  → Verify zero remaining refs → format → test → commit
```

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## Why This Exists

Dropped features leave cascading debris — routes, views, models, services, tests, migrations, User model methods, settings, factories, notifications. Multiple cleanup plans end up dealing with the same dropped feature. This skill finds and removes EVERYTHING in one shot, ensuring no orphaned code remains.

## Critical Rules

1. **NEVER delete migration files** — mark them with a comment `// Kept for history — feature dropped` for future squash.
2. **Always verify zero remaining references** after removal with a second discovery pass.
3. **Use AskUserQuestion before executing** — this is a destructive operation. The user must confirm scope.
4. **Run pint + targeted tests after removal** to catch broken references and fix formatting.
5. **If a file has mixed content** (feature + non-feature code), edit surgically — don't delete the whole file.
6. **Preserve git history** — use individual commits per category if the removal is large (>20 files).
7. **Stop on unexpected errors** — never force-delete without confirmation. If something looks wrong, ask.
8. **Never force-push or rewrite history** — all removals are forward-only commits.
9. **Check for related Eloquent relationships** — removing a model may break `hasMany`, `belongsTo`, etc. on other models.

---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 1/4: Parse & Discover

```bash
echo "🔧 drop-feature [1/4] discovering all references to feature"
```

Parse feature name from $ARGUMENTS. Derive naming variants:
- `snake_case`: `feature_name` (from argument)
- `PascalCase`: `FeatureName`
- `camelCase`: `featureName`
- `kebab-case`: `feature-name`
- `dot.case`: `feature.name`

Search ALL reference locations using Grep/Glob:

| Category | Search Pattern | Search Paths |
|----------|---------------|--------------|
| Routes | `feature_name`, `feature-name`, `FeatureName` | `routes/` |
| Controllers | `FeatureName` | `app/Http/Controllers/` |
| Models | `FeatureName` | `app/Models/` |
| Views & Components | `feature.name`, `feature-name`, `FeatureName` | `resources/views/`, `resources/views/components/` |
| Livewire | `FeatureName`, `feature-name` | `app/Livewire/`, `resources/views/livewire/` |
| Services | `FeatureName` | `app/Services/` |
| Tests | `FeatureName`, `feature_name` | `tests/` |
| Factories | `FeatureName` | `database/factories/` |
| Seeders | `FeatureName` | `database/seeders/` |
| Migrations | `feature_name`, `FeatureName` | `database/migrations/` |
| Notifications | `FeatureName` | `app/Notifications/` |
| Enums | `FeatureName`, `feature_name` | `app/Enums/` |
| Config | `feature_name` | `config/` |
| Navigation/Sidebar | `feature.name`, `feature-name` | `resources/views/components/`, `resources/views/layouts/` |
| Middleware | `FeatureName`, `feature` | `app/Http/Middleware/` |
| Jobs | `FeatureName` | `app/Jobs/` |
| Events | `FeatureName` | `app/Events/` |
| Listeners | `FeatureName` | `app/Listeners/` |
| Policies | `FeatureName` | `app/Policies/` |
| Form Requests | `FeatureName` | `app/Http/Requests/` |
| Actions | `FeatureName` | `app/Actions/` |
| Service Providers | `feature_name`, `FeatureName` | `bootstrap/providers.php`, `config/app.php` |
| Language Files | `feature.name`, `feature-name` | `lang/` |

Display discovery results as a categorized summary:
```
🔥 Feature Discovery: <feature-name>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Routes:          3 references in 2 files
Controllers:     1 file (dedicated)
Models:          1 file (dedicated) + 2 relationship refs
Views:           4 files (3 dedicated, 1 shared)
Services:        1 file (dedicated)
Tests:           6 files (all dedicated)
Factories:       1 file (dedicated)
Migrations:      2 files (will be marked, not deleted)
Navigation:      1 reference in sidebar
Config:          1 reference in config/app.php
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total: 23 references across 11 categories
```

## Step 2/4: Generate Removal Plan

```bash
echo "🔧 drop-feature [2/4] generating removal plan"
```

Use `AskUserQuestion` to confirm the scope:

**Question:** "Found X references across Y categories. Review the removal scope?"

Options:
1. "Remove all — proceed"
2. "Show me the full list first" — display every file path and line number
3. "Exclude some categories" — let user specify which categories to skip (e.g., keep migrations untouched, skip config)

Build the removal plan — classify each file as:
- **DELETE**: entire file is feature-specific (e.g., dedicated controller, model, test)
- **EDIT**: file has mixed content, only specific lines/methods/routes to remove (e.g., sidebar link, route group, relationship method on another model)
- **MARK**: migration files — add comment but don't delete

## Step 3/4: Execute Removal

```bash
echo "🔧 drop-feature [3/4] executing removal"
```

```bash
echo "🔥 [drop-feature:3] executing removal"
```

Dispatch parallel subagents by category for efficiency. Always set `model: "sonnet"` on these agent calls:

### Agent A: Routes + Controllers
- Remove dedicated route files or route groups from shared files
- Delete dedicated controllers
- Remove controller imports from route files

### Agent B: Models + Migrations
- Delete dedicated model files
- Remove relationship methods (e.g., `hasMany(FeatureName::class)`) from OTHER models
- Mark migration files with comment: `// Kept for history — <feature-name> feature dropped`
- Remove model imports from other files

### Agent C: Views + Components + Navigation
- Delete dedicated Blade views and Livewire components
- Remove sidebar/navigation links from layout files
- Delete dedicated Livewire component classes
- Remove `@livewire` or `<livewire:>` tags from shared views

### Agent D: Services + Jobs + Events + Listeners + Notifications
- Delete dedicated service classes
- Delete dedicated jobs, events, listeners, notifications
- Remove references from `EventServiceProvider` or other providers
- Remove service injection from constructors of other classes

### Agent E: Tests + Factories + Seeders
- Delete dedicated test files
- Delete dedicated factories
- Remove factory references from seeders
- Remove feature-specific seeder entries

Each agent follows the same rule: **delete files where the entire file is feature-specific, edit files where only specific lines/methods reference the feature.**

## Step 4/4: Verify & Report

```bash
echo "🔧 drop-feature [4/4] verifying and reporting"
```

```bash
echo "🔥 [drop-feature:4] verifying clean removal"
```

### 4a. Re-run Discovery
Re-run the exact same search from Step 1 to confirm zero remaining references.

If references remain:
- Display the stragglers
- Dispatch a cleanup agent to handle them
- Re-verify until clean

### 4b. Format & Test
```bash
vendor/bin/pint --dirty
```

Run targeted tests to catch broken references:
```bash
vendor/bin/pest --filter="<related-test-patterns>"
```

If tests fail due to missing classes/routes (expected after removal), verify the failures are all related to the dropped feature. If unrelated tests break, STOP and report.

### 4c. Commit

If the removal is small (<20 files total):
```bash
# Single commit
git add -A
git commit -m "🔥 remove: drop <feature-name> feature — N files deleted, M files edited"
```

If the removal is large (>=20 files):
```bash
# Commit per category
git add routes/ app/Http/Controllers/
git commit -m "🔥 remove: drop <feature-name> routes and controllers"

git add app/Models/ database/migrations/
git commit -m "🔥 remove: drop <feature-name> models, mark migrations"

git add resources/
git commit -m "🔥 remove: drop <feature-name> views and components"

git add app/Services/ app/Jobs/ app/Events/ app/Listeners/ app/Notifications/
git commit -m "🔥 remove: drop <feature-name> services, jobs, events, notifications"

git add tests/ database/factories/ database/seeders/
git commit -m "🔥 remove: drop <feature-name> tests, factories, seeders"
```

### 4d. Final Report

```
🔥 Feature Dropped: <feature-name>
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Files deleted:    15
Files edited:     8
Routes removed:   4
Tests removed:    6
Migrations:       Kept (marked for squash)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Remaining refs:   0 ✅
```

If any references could not be removed:
```
Remaining refs:   2 ⚠️
  - config/services.php:45 — manual review needed
  - app/Providers/AppServiceProvider.php:12 — conditional logic
```
