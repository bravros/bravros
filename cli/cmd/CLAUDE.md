# cli/cmd — Cobra Command Authoring

> 📊 **Knowledge graph available** — query the graph via `graphify query "<question>"` (CLI, from the repo root) before broad searches in this folder. See [`../../CLAUDE.md`](../../CLAUDE.md#graphify-knowledge-graph--query-this-before-grepping).

Every `bravros` subcommand lives here as one `.go` file (+ `_test.go`). Flat structure, no subdirectories.

## Adding a command

1. **Create `cli/cmd/yourcmd.go`**:
   ```go
   package cmd

   import "github.com/spf13/cobra"

   var yourcmdCmd = &cobra.Command{
       Use:   "yourcmd",
       Short: "One-line description",
       RunE: func(cmd *cobra.Command, args []string) error {
           // implementation
           return nil
       },
   }

   func init() {
       rootCmd.AddCommand(yourcmdCmd)
   }
   ```
2. **Add `yourcmd_test.go`** — happy path + at least one error case. Required for every non-trivial command.
3. **Keep `cmd/` thin.** Non-trivial logic goes in `cli/internal/<domain>/`.
4. **Document** in `docs/CLI.md` (one-liner table) AND the correct `docs/cli/<group>.md` detail file (sdlc-core, git-project, audit-autopr-hooks, or integrations).

## Where logic goes

| Concern | Package |
|---|---|
| Git operations | `cli/internal/git` |
| GitHub API | `cli/internal/github` |
| Plan/backlog id + entity resolution | `cli/internal/plan` |
| Config / `.bravros.yml` | `cli/internal/config` |
| Stack detection (internal only — no `detect-stack` verb) | `cli/internal/stack` |
| Home Assistant | `cli/internal/ha` |

Reach across packages via exported functions, never via duplicated helpers.

## File naming

- Lowercase, no hyphens: `merge_lock.go`, NOT `merge-lock.go`
- One command = one file; tests adjacent: `autopr.go` + `autopr_test.go`
- Shared helpers that don't belong to one command → `cli/internal/`, not here

## Import path

Full module path for all imports:
```go
import "github.com/bravros/bravros/cli/internal/git"
```

Module name is `claude-cli` for backwards compat; the binary is `bravros`.

## Command Authoring Notes

### init

- **Precedence:** positional arg > `--portable-repo` flag > `$BRAVROS_PORTABLE_REPO` env > cwd
- **Bootstrap source:** always use the resolved path from the precedence chain, never cwd
- **Invalid path:** when `--portable-repo` is set but the path is not a valid claude config repo, exit non-zero with a clear stderr error (loud failure, not silent skip)

## Never

- Put business logic in `cmd/*.go` — belongs in `internal/`
- Reach outside `cli/` (into `skills/`, `.planning/`, etc.) — the CLI is self-contained
- Ship a new command without updating both `docs/CLI.md` AND the detail file under `docs/cli/`
- Skip the test file
