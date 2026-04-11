# Bravros CLI — Go Project Instructions

## Overview

`bravros` is a Go CLI binary that powers the Bravros SDLC 5.0. It provides audit enforcement, git context, PR helpers, Home Assistant integration, and more.

- **Module:** `github.com/bravros/private`
- **Go version:** See `go.mod`
- **Framework:** [cobra](https://github.com/spf13/cobra) for commands

## Project Structure

```
cli/
├── main.go              # Entry point
├── cmd/                 # Cobra commands (audit, sdlc, pr, ha, statusline, etc.)
├── internal/
│   ├── audit/           # Pre-commit/pre-push enforcement rules (23 rules)
│   ├── backup/          # Backup utilities
│   ├── git/             # Git context helpers (branch, diff, PR info)
│   ├── github/          # GitHub API helpers (PR, repo)
│   ├── ha/              # Home Assistant API client
│   ├── plan/            # Plan file parsing
│   └── unifi/           # UniFi network API client
├── testdata/            # Test fixtures
├── go.mod / go.sum
└── README.md
```

## Development Commands

```bash
# Build (local, for testing)
cd cli && go build ./...

# Build with version (local install)
cd cli && go build -ldflags="-s -w -X github.com/bravros/private/cmd.Version=v1.9.5" -o ../bin/bravros .
cp ../bin/bravros ~/.claude/bin/bravros

# Run tests
cd cli && go test ./...

# Run specific test
cd cli && go test -run TestName ./internal/audit/

# Vet
cd cli && go vet ./...
```

## Local Build Rules

- **Always use `-ldflags` with version** when building for `bin/` or `~/.claude/bin/` — never `go build -o` without it
- **Version tag format**: `-X github.com/bravros/private/cmd.Version=v1.9.5` — the `v` prefix is stripped at print time, so always include it for consistency with git tags
- **Always copy to both locations**: `bin/bravros` (for install.sh) AND `~/.claude/bin/bravros` (live)
- **After building locally, commit `bin/bravros`** so `install.sh` picks up the new version on other machines

## Release & Tagging

**NEVER build binaries locally for releases.** The repo has `.github/workflows/release.yml` that handles everything.

### Release process:
1. Commit code changes to `main`
2. Create annotated tag: `git tag -a vX.Y.Z -m "description"`
3. Push: `git push origin main --tags`
4. Done — the Action cross-compiles all 3 binaries and creates a GitHub Release

### What the Action does:
- Triggers on `v*` tag push
- Builds 3 binaries: `darwin-arm64`, `darwin-amd64`, `linux-amd64`
- Uses `-ldflags="-s -w -X github.com/bravros/private/cmd.Version=${VERSION}"` (stripped, with version)
- Creates GitHub Release with auto-generated release notes
- Attaches binaries as release assets

### Verify a release:
```bash
gh run list --workflow=release.yml --limit=1
gh release view vX.Y.Z
```

## Audit Rules (23 rules in `internal/audit/rules.go`)

All rules live in a single file. Do NOT split into multiple files — keep them together for grep-ability and simplicity.

1. Skill read before frontend edits (taste-skill alias satisfies)
2. Track SKILL.md and reference file reads
3. Team vs Subagent compliance
4. AskUserQuestion call tracking
5. Command checkpoint tracking and prerequisites
6. Plan template read before new plan creation (excludes backlog/, reports/, user-reports/)
7. Block GitHub workflows without homolog branch
8. Full test suite must use `--parallel` (Laravel-only via .skaisser.yml)
9. Plan task deletion detection
10. Block dangerous commands (migrate:fresh, push to main, AI signatures)
11. @claude review prompt must be comprehensive
12. Plan-check skip detection — BLOCK in SDLC flows, WARN in ad-hoc
13. Unchecked acceptance criteria — BLOCK in SDLC flows, WARN in ad-hoc
14. auto-pr step enforcement (mandatory pipeline steps)
15. Backlog CLI enforcement (blocks manual file parsing)
16. Planning mv check — require `git mv` for tracked .planning/ files (skips untracked)
17. Block merge to main — autonomous pipelines DENIED for main merges
18. Project config detection
19. Agent model enforcement — [H]/[S]/[O] markers must match model parameter
20. Bash redirect bypass — block cat/echo/tee redirects to .planning/
21. Auto skill gate — track autonomous skill invocations
21b. Lock file tamper protection — block rm/truncate of .auto-pr-lock
21c. Autonomous mode sticky tracking — was-autonomous survives lock deletion
22. Read-only skill enforcement — block file edits during /debug
23. Block writes to ~/.claude/ deployed dir (exempts hooks/logs, settings.json, bin/bravros, projects/, verify-install)

## Key Conventions

- Import paths must use full module: `github.com/bravros/private/internal/...`
- Autonomous pipeline skills: `auto-pr`, `auto-pr-wt`, `auto-merge` (not the old names flow-auto, batch-flow)
- Checkpoint echo format: `echo "🤖 [skill-name:N] description"`
- The `bin/` directory in the repo root contains local copies for `install.sh` — these are separate from release assets
