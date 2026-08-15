# Skill Delivery — How skills reach your agent

Skills reach your machine through two different paths depending on your host. This document explains how each works and when to expect updates.

## Quick reference: Install and update for each host

| Host | Path | Install | Updates |
|---|---|---|---|
| **Claude Code** | A | `/plugin marketplace add bravros/bravros` then `/plugin install bravros` | Automatic background refresh |
| **Gemini CLI** | A | `gemini extensions install https://github.com/bravros/bravros --auto-update` | Automatic on launch |
| **Cursor** | B | `curl -fsSL https://install.bravros.dev \| sh` | Automatic, every session |
| **Codex** | B | `curl -fsSL https://install.bravros.dev \| sh` | Automatic, every session |
| **Copilot** | B | `curl -fsSL https://install.bravros.dev \| sh` | Automatic, every session |
| **Aider** | B | `curl -fsSL https://install.bravros.dev \| sh` | Automatic, every session |

---

## Path A — plugin marketplace (Claude Code, Gemini CLI)

Claude Code and Gemini CLI have built-in plugin or extension systems that fetch skills directly from this repository. You do not need a local clone.

**Claude Code**

```
/plugin marketplace add bravros/bravros
/plugin install bravros
```

Updates happen automatically in the background. You may see a refresh notification, but no action is required — Claude Code handles it entirely.

**Gemini CLI**

```
gemini extensions install https://github.com/bravros/bravros --auto-update
```

The extension is fetched when you launch Gemini CLI, and Gemini automatically checks for updates on each launch.

**How it works:** Claude Code and Gemini CLI pull skills from the public GitHub repository through their own mechanisms. The bravros binary never writes into their plugin directories (`~/.claude/plugins/`, `.claude-plugin/`, `~/.gemini/extensions/`). If the binary ever tried, a guard (`deploy.IsPluginManaged`) fails the operation loudly — two writers on the same skills is a conflict, not redundancy.

---

## Path B — `bravros selfupdate` (Cursor, Codex, Copilot, Aider)

Cursor, Codex, Copilot, Aider, and other hosts that lack a built-in plugin system get skills through `bravros selfupdate`, which runs automatically from a SessionStart hook that `install.sh` registers in `~/.claude/settings.json`.

### Installation

```bash
curl -fsSL https://install.bravros.dev | sh
```

This one command:
- Downloads and signature-verifies the bravros CLI binary into `~/.claude/bin/`
- Registers a SessionStart hook that runs `bravros selfupdate` automatically

It does **not** install skills. Those arrive on the first session afterwards, when the
SessionStart hook fires and `selfupdate` fetches them. No GitHub token is required. No clone is
made.

### How it works

When a new session starts:

1. **Check for an update** — `bravros selfupdate` resolves the newest published release tag using the redirect on `https://github.com/bravros/bravros/releases/latest`. No GitHub API call, no token, no rate-limit budget.

2. **Fetch if behind** — If the payload on disk is behind the published release, download:
   - `bravros-payload.tar.gz` (~300 KB — the `skills/` and `templates/` trees, nothing else)
   - `checksums.txt` (SHA-256 hashes)
   - `checksums.txt.minisig` (minisign signature)

3. **Verify before trusting** — The minisign signature over `checksums.txt` is verified against a key pinned in the binary. The same key `install.sh` pins during initial installation. Only after signature verification passes do we compare the payload's SHA-256. Only then is anything extracted.

4. **Land on disk** — Skills and templates are deployed to `~/.claude/skills/` and `~/.claude/templates/`, with pruning disabled. Hand-installed hooks, agents, and skills are never removed.

5. **Continue** — Your session proceeds unchanged. If the fetch failed for any reason (offline, GitHub temporarily down, corrupted download), the previous skill tree is left exactly as it was, and the session continues using the already-installed skills.

### Cadence

Updates are **automatic and silent** on every session start — but two rate limits prevent excessive remote requests:

- **Check cache:** 6 hours (`BRAVROS_SELFUPDATE_TTL`, set to `0` to disable)
- **Remote request minimum:** 1 hour between checks (`BRAVROS_REMOTE_CHECK_TTL`, set to `0` to disable)

A second run within those windows makes no remote request and prints nothing at all.

### Forcing an update

To check for an update immediately without waiting for the next session:

```bash
bravros update   # or: bravros selfupdate --force
```

To deploy an already-fetched payload by hand:

```bash
bravros deploy --source ~/.claude/payload
```

### Offline behavior

If the check fails (no network, GitHub is down), the session continues without error:

- No error message
- No blocking
- Exit code 0
- Skills already on disk keep working unchanged
- Nothing is retried until the next session

This is intentional — a network hiccup or temporary outage does not interrupt your work.

### Timing: payload availability

**Important:** The `bravros-payload.tar.gz` asset ships **only on the first release cut after this mechanism is published**. Releases published before that date carry no payload, and `bravros selfupdate` correctly does nothing against those releases and stays silent. Once the asset is published, every subsequent release includes it automatically.

You can check if a release has the payload by visiting `https://github.com/bravros/bravros/releases/tag/vX.Y.Z` and looking for the `bravros-payload.tar.gz` asset. If it's not there, the release predates this feature.

---

## Trust and security

Path B's trust chain is explicit, and it is the same chain `install.sh` uses for the binary:

1. The minisign public key is **compiled into the binary as a constant** — the identical key
   literal that `install.sh` pins. There is no key file on disk to tamper with, and no key is
   ever downloaded.
2. Each release signs `checksums.txt` with the corresponding private key, held only in CI. The
   payload is covered because its SHA-256 line lives inside that signed file.
3. On fetch, `bravros selfupdate` verifies that signature **before trusting a single byte of
   `checksums.txt`** — only then does it compare the payload's SHA-256, and only then does it
   extract anything.
4. A tampered, truncated, or unsigned payload is refused. Extraction happens into a staging
   directory and is swapped into place atomically, so a failure at any step leaves the previous
   skill tree exactly as it was.
5. Archive entries are bounded and sanitised: path traversal, absolute paths, and symlink entries
   are rejected outright.

No API key, no account, no rate-limit budget. GitHub is the only outbound host.

You can verify the pinned key yourself against the one published at
[bravros.dev/security](https://bravros.dev/security) — it appears in `install.sh` and in
`cli/internal/fetch/fetch.go`, and the two must match.

---

## Why two paths?

Path A (plugin marketplace) is the most seamless: Claude Code and Gemini CLI do all the work for you. You install once and never think about updates.

Path B (self-update fetch) is necessary for hosts that lack a built-in plugin system. Fetching from a released tarball is more efficient and more secure than cloning the entire repo on every update, and it works completely offline if the skills are already cached.

Both paths are automatic. Both require zero ongoing maintenance. Both are designed to stay in the background and never interrupt your work.
