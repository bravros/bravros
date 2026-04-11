---
name: context
description: >
  Scan project and generate/audit CLAUDE.md files with stack auto-detection and Context7 docs.
  Use this skill whenever the user says "/context", "generate context", "audit CLAUDE.md",
  "scan project", or any request to create or update CLAUDE.md documentation files.
  Also triggers on "update context docs", "context scan", "check CLAUDE.md", "onboard project",
  "brownfield", "onboard existing project", "stale documentation", or "refresh project docs".
  Runs parallel Sonnet workers per directory. Also audits README.md for staleness and checks
  laravel/boost installation.
---

# Context: Generate & Audit CLAUDE.md Files

Scan the project, auto-detect the tech stack, query framework documentation via Context7, and generate a tree of lean, focused CLAUDE.md files. Runs multiple subagents in parallel — one per directory cluster.

## When to Run

- After `/start` on an existing codebase (brownfield onboarding)
- When onboarding to a project for the first time
- After adding major new packages or refactoring a directory
- When a CLAUDE.md feels stale or has incorrect info

## Critical Rules

- NEVER overwrite existing CLAUDE.md without `--force` — but ALWAYS audit for staleness
- NEVER run full test suite — targeted tests only
- Read actual code — never guess patterns
- Dispatch ALL directory workers in ONE message — parallel, not sequential
- Leader NEVER writes CLAUDE.md files directly — always delegates to workers
- Use `AskUserQuestion` for ALL user interactions — never ask questions in plain text
- Do NOT auto-commit — let the user review generated files

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Step 1/9: Detect Stack — MANDATORY

```bash
echo "✨ context [1/9] detecting project stack"
```

**Read `references/stack-detection.md` (bundled with this skill) before starting.** It contains the full detection tables for languages, frameworks, test runners, asset pipelines, and databases (sections 1a–1f).

Run the enriched detect-stack command and capture the full JSON for use in later steps:

```bash
STACK_JSON=$(~/.claude/bin/bravros detect-stack --versions 2>/dev/null)
```

This provides exact framework + version data (e.g. `versions.laravel`, `versions.livewire`, `versions.tailwindcss`, `project_type`, etc.) for use in Context7 queries and CLAUDE.md generation.

Follow sections 1a–1f from that reference, then echo the output summary as shown in section 1f.

## Step 2/9: Framework-Specific Setup Check

```bash
echo "✨ context [2/9] checking framework-specific setup"
```

### Laravel Projects
Check laravel/boost. If not installed, ask about installing. If installed, run `php artisan boost:update`.

### Expo / React Native / Next.js Projects
Extract versions and key dependencies.

## Step 3/9: Query Context7 for Framework Docs

```bash
echo "✨ context [3/9] querying framework documentation"
```

For the primary detected framework, use Context7 MCP tools:

### 3a. Resolve Library ID

Call `mcp__context7__resolve-library-id` with the framework name and exact version from `STACK_JSON` (detected in Step 1). Use the version to form a precise query (e.g. `"laravel 13.2.0"` instead of just `"laravel"`):
- Laravel → `"laravel <versions.laravel>"`
- Next.js → `"nextjs <versions.nextjs>"`
- Django → `"django <versions.django>"`
- Rails → `"ruby on rails <versions.rails>"`
- React → `"react <versions.react>"`
- Vue → `"vue <versions.vue>"`
- FastAPI → `"fastapi <versions.fastapi>"`
- Gin → `"gin golang <versions.gin>"`
- etc.

If the version field is empty or unavailable, fall back to the bare framework name.

### 3b. Query Documentation

Using the resolved library ID, call `mcp__context7__query-docs` for:

1. **Directory structure** — query: `"project directory structure conventions"`
2. **Key patterns** — query: `"best practices and common patterns"`
3. **Testing** — query: `"testing conventions and patterns"`

Use these results to inform:
- Which subdirectory CLAUDE.md files to generate
- What conventions to include in each file
- Framework-specific patterns and anti-patterns

### 3c. Context7 Fallback

If Context7 MCP tools are not available (tools not found, server not running):
1. Log: `echo "✨ context [3/9] Context7 unavailable — using built-in conventions"`
2. Fall back to common conventions for the detected framework
3. Continue without error — Context7 is optional enrichment, not required

## Step 4/9: Scan Project Structure

```bash
echo "✨ context [4/9] scanning project structure"
```


### 4a. Find Existing CLAUDE.md Files

```bash
find . -name "CLAUDE.md" -not -path "*/vendor/*" -not -path "*/node_modules/*" -not -path "*/.git/*" 2>/dev/null
```

### 4b. Map Project Directories

Identify directories that benefit from CLAUDE.md (5+ files with shared patterns, non-obvious rules, complex flows, critical gotchas, external API integrations).

### 4c. Determine Mode

- **No existing CLAUDE.md files** → Full generation mode
- **Existing CLAUDE.md files found** → Audit mode (see Step 7)

## Step 5/9: Dispatch Parallel Workers

```bash
echo "✨ context [5/9] dispatching parallel workers"
```

Run: `echo "✨ context [5/9] dispatching parallel workers"`

Dispatch ALL workers in ONE message. Group directories into logical clusters.

Each worker: reads files, audits existing CLAUDE.md, generates new ones where needed. Workers use `general-purpose` subagent type with `model: "sonnet"`. Max 200 lines per CLAUDE.md. Document the NON-OBVIOUS.

**For testing conventions:** Reference `~/.claude/skills/context/references/test-runners.md` to include framework-specific test running commands and patterns in generated CLAUDE.md files.

### Framework-Specific Directory Templates

Generate CLAUDE.md files based on detected framework. Only create for directories that **actually exist**.

#### Laravel

| Directory | Key Content |
|-----------|-------------|
| `app/Models/` | Relationships, factories, casts, scopes, never raw SQL |
| `app/Http/Controllers/` | Single responsibility, use Form Requests, resource controllers |
| `app/Http/Requests/` | Validation rules, authorize method, custom messages |
| `app/Services/` | Business logic lives here, not in controllers |
| `app/Livewire/` | Component patterns, wire:model, events, lifecycle |
| `database/migrations/` | Never migrate:fresh, always add new migrations |
| `database/factories/` | Factory patterns, states, relationships |
| `tests/` | Pest patterns, factories over fixtures, no mocking DB |
| `resources/views/` | Blade/Livewire patterns, component library (DaisyUI if detected) |
| `routes/` | Route naming, middleware, group patterns |
| `config/` | Never hardcode — use env(), config caching |

#### Next.js / React

| Directory | Key Content |
|-----------|-------------|
| `app/` | App Router conventions, server vs client components, layouts |
| `components/` | Component naming, props patterns, composition |
| `lib/` | Utility functions, API clients, shared logic |
| `tests/` or `__tests__/` | Testing framework, component testing patterns |

#### React Native / Expo

| Directory | Key Content |
|-----------|-------------|
| `app/` | Expo Router, screen patterns, layouts |
| `components/` | Component patterns, StyleSheet conventions |
| `hooks/` | Custom hooks, state management |
| `services/` or `lib/` | API clients, storage, utilities |
| `__tests__/` | Jest + React Native Testing Library patterns |

#### Go

| Directory | Key Content |
|-----------|-------------|
| `cmd/` | Entry points, CLI structure, flag parsing |
| `internal/` | Package patterns, interfaces, dependency injection |
| `pkg/` | Public packages, API stability |

## Step 6/9: Root CLAUDE.md

```bash
echo "✨ context [6/9] generating root CLAUDE.md"
```

Generate or audit root CLAUDE.md based on project structure and conventions found during scanning:
- **Tech Stack**: Auto-detected language, framework, test runner, assets, database
- **Testing Patterns**: How tests are organized (e.g., "Uses Pest with parallel, FormRequest validation tests")
- **Architecture Decisions**: Patterns that emerged (e.g., "Repository pattern for services, Action classes")
- **Known Gotchas**: Problems discovered that future sessions should know about

Keep it concise — under 60 lines. The root file is a MAP, not an encyclopedia.

## Step 7/9: Audit Mode (Existing CLAUDE.md Files)

```bash
echo "✨ context [7/9] auditing existing CLAUDE.md files"
```

Run: `echo "✨ context [7/9] auditing existing CLAUDE.md files"`

If CLAUDE.md files already exist:

### 7a. Check for Staleness

- Wrong framework version mentioned
- Outdated patterns or deprecated APIs
- Missing conventions for newly added directories
- References to files/directories that no longer exist
- Incorrect testing patterns

### 7b. Report and Suggest

Use `AskUserQuestion` to present findings. Let user approve or reject each change. Do NOT overwrite without explicit confirmation.

### 7c. Generate Missing Files

If new directories exist that should have CLAUDE.md but don't, generate following Step 5 rules.

## Step 8/9: Audit README.md — MANDATORY

```bash
echo "✨ context [8/9] auditing README.md"
```

Run: `echo "✨ context [8/9] auditing README.md"`

Cross-reference README.md against what was learned during the scan. Check versions, integrations, structure tree, commands, missing patterns. Use `AskUserQuestion` to offer updates.

## Step 9/9: Report

```bash
echo "✨ context [9/9] generating report"
```

Show summary:
- **Stack detected**: language, framework, test runner, assets, DB
- **Context7**: whether it was used, which docs were queried
- **Created**: list of new CLAUDE.md files generated
- **Updated**: list of existing CLAUDE.md files updated
- **Unchanged**: list of CLAUDE.md files still current
- **Skipped**: directories that don't need CLAUDE.md
- **README.md**: staleness findings if any

## Flags

- `--force` / `-f` — Regenerate all CLAUDE.md files
- `--dry-run` / `-d` — Show what would be created/updated
- `--root` / `-r` — Root CLAUDE.md only
- `--audit` / `-a` — Audit only, no new generation
- `--no-context7` — Skip Context7 queries, use built-in conventions only

Use $ARGUMENTS as a specific directory path or flag.
