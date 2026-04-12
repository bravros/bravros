# Bravros

**SDLC pipeline for Claude Code** — plan, build, test, deploy with confidence.

> **This is the distribution repository.** It contains the installer script, pre-built binaries, and release assets only. There is no source code here to clone or build — if you're looking for that, it lives in a private repo.

## Install

```bash
curl -fsSL https://install.bravros.dev | sh
```

Or via Homebrew:

```bash
brew install bravros/tap/bravros
```

No `git clone` required. The installer downloads the correct binary for your platform, places it in your PATH, and sets up Claude Code integration.

## Activation

After installing, activate with your license key:

```bash
bravros activate <your-license-key>
```

Don't have a license key? Purchase or manage your subscription at **[bravros.dev](https://bravros.dev)**.

## How skills work

Skills (slash commands like `/plan`, `/auto-pr`, `/commit`) are **not bundled in this repo**. They are fetched by the CLI from `app.bravros.dev` at activation time and kept up to date automatically on each Claude Code session start.

This means:

- No skill files in this repo — nothing to leak or reverse-engineer from a clone.
- Skills update silently when you're online, without re-running the installer.
- Your license key controls which skills you have access to.

## What this repo contains

| File / Dir | Purpose |
|---|---|
| `install.sh` | Installer script (synced from private on each release) |
| `config/` | MCP server configuration (`mcp.json`) and Claude Code settings |
| `.goreleaser.yml` | GoReleaser stub (build runs in private, releases publish here) |
| `LICENSE` | License terms |
| `README.md` | This file |

Releases on this repo contain signed binary tarballs and checksums. The installer verifies signatures before placing any binary on your system.

## Verify an install

The installer checks signatures automatically. For manual verification, see the signature files attached to each release and the public key published at [bravros.dev/security](https://bravros.dev/security).

## Bug reports & support

> **Issue tracker:** TBD — please contact support@bravros.dev in the meantime.

This repo does not track development issues. For bugs in the CLI or skills, reach the team via the contact above.

## License

Proprietary — see [LICENSE](LICENSE).
