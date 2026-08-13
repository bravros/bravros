# claude-cli

Unified Go binary replacing 8 Python/Bash scripts. Pre-compiled binaries shipped in repo — no Go needed on target machines.

| Command | Go | Python | Speedup |
|---|---|---|---|
| `audit` (every tool call) | **18ms** | 111ms | **6x** |
| `sdlc meta` | **9ms** | 224ms | **25x** |
| `sdlc context --skip-pr` | **103ms** | 223ms | **2x** |

Full documentation: **[docs/CLI.md](../docs/CLI.md)**

## Quick Reference

```bash
claude-cli sdlc meta|context|sync|full|commit|nextid       # SDLC workflow
claude-cli merge-lock {acquire,release,status}             # POSIX flock-based merge coordination primitive (replaces removed merge-pr)
claude-cli audit                                            # Pre-tool-use hook (stdin JSON)
claude-cli pr-review [PR] [--latest]                        # PR review data
claude-cli ha say|lights|desk|state|toggle|...              # Home Assistant
claude-cli repos check                                      # Multi-repo scanner
```

## Building (only needed for changes)

```bash
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o claude-cli-darwin-arm64 .
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o claude-cli-darwin-amd64 .
GOOS=linux  GOARCH=amd64 go build -ldflags="-s -w" -o claude-cli-linux-amd64  .
```
