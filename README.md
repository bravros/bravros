<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://bravros.dev/logo-wide-dark.svg">
  <img src="https://bravros.dev/logo-wide.svg" alt="Bravros" width="360">
</picture>

### SDLC pipeline for Claude Code
**Plan it. Build it. Test it. Ship it. Without leaving your terminal.**

[![Latest release](https://img.shields.io/github/v/release/bravros/bravros?label=release&color=2b78e4&style=for-the-badge)](https://github.com/bravros/bravros/releases/latest)
[![Platforms](https://img.shields.io/badge/macOS-arm64%20%7C%20amd64-blue?style=for-the-badge&logo=apple)](#install)
[![Linux](https://img.shields.io/badge/Linux-amd64-blue?style=for-the-badge&logo=linux&logoColor=white)](#install)
[![Signed](https://img.shields.io/badge/releases-minisign%20signed-2ea44f?style=for-the-badge)](#-verify-the-trust-chain)
[![Built for Claude Code](https://img.shields.io/badge/built%20for-Claude%20Code-9b59ff?style=for-the-badge)](https://claude.com/code)

[**Get started →**](https://app.bravros.dev) &nbsp;·&nbsp; [Docs](https://bravros.dev/docs) &nbsp;·&nbsp; [Releases](https://github.com/bravros/bravros/releases) &nbsp;·&nbsp; [Security](https://bravros.dev/security)

</div>

---

## 🎁 Free Pro during the beta

> Sign up at **[app.bravros.dev](https://app.bravros.dev)** and you get a **Pro license activated immediately — completely free for the entire beta period.** No payment, no trial expiry, full feature access. New beta users keep their Pro entitlement for as long as the beta runs.

---

## ⚡ Install

One-line install (macOS arm64/amd64, Linux amd64):

```bash
curl -fsSL https://install.bravros.dev | sh
```

Or via Homebrew:

```bash
brew install bravros/tap/bravros
```

Then activate with the license key from your dashboard:

```bash
bravros activate XXXX-XXXX-XXXX-XXXX
```

That's it. The CLI installs itself to `~/.claude/bin/bravros`, wires up Claude Code's hooks + statusline, and silently keeps your skills up to date on each session start.

---

## ✨ What you get

|  |  |
|---|---|
| 🧠 **Structured planning** | `/plan` → `/plan-review` → `/plan-approved` turns rough ideas into phased implementation plans with acceptance criteria. No more stream-of-consciousness coding. |
| 🤖 **Autonomous pipeline** | `/auto-pr` runs the full SDLC loop — plan → review → implement → test → PR — with zero intervention. Stops at PR-ready for your human approval. |
| 🧪 **Test-first by default** | Built-in coverage gates, parallel test execution, framework-aware (Pest, Vitest, Jest, pytest, Go test). Tests are written, not skipped. |
| 🔀 **Git-aware everywhere** | `/commit`, `/branch`, `/pr`, `/finish`, `/merge-chain` — emoji-formatted, conventional, audited by hooks. No more "wip" commits. |
| 📦 **70+ slash commands** | From `/audit` and `/debug` to `/firecrawl`, `/brand-generator`, `/notebooklm`, `/cf-pages-deploy`, `/remotion-video`, `/ralph-loop` and more. Pick what you need, ignore the rest. |
| 🔒 **Signed binaries** | Every release is signed with minisign. Pinned public key in the installer means no MITM, no tampered binaries. |
| 🌍 **Fully multilingual** | Both the CLI and the dashboard are translated end-to-end into English, Português (BR), and Español. Locale is auto-detected from `LANG` / your dashboard preference. |
| 🛡️ **No telemetry** | Zero analytics, zero crash reporting, zero usage tracking — see [Privacy & data](#-privacy--data) below for the exhaustive list of network calls the CLI makes. |

---

## 🚀 Quick tour

```bash
# Plan a feature in seconds
/plan add user notification preferences

# Review the plan, mark complexity, generate an execution strategy
/plan-review

# Execute it — coordinator delegates to subagents in parallel
/plan-approved

# Audit implementation against the plan
/plan-check

# Open a PR with the right base branch + structured description
/pr

# Trigger an automated review
/review

# Address feedback + push fixes
/address-pr

# Merge + close out
/finish
```

Or skip the steps and run the whole thing autonomously:

```bash
/auto-pr fix the timezone bug in the order summary
```

---

## 🏗️ How it works

Bravros is a single Go binary that lives at `~/.claude/bin/bravros`. No Node, no Python, no runtime to manage — it just runs.

When you activate, it talks to `app.bravros.dev` once to verify your license and pull down the skills you have access to. After that, almost everything happens **on your machine**:

- The CLI runs an **on-device audit** every time Claude Code is about to do something touchy — committing, pushing, calling a hook, executing a slash command. The audit lives entirely on your laptop. It enforces SDLC discipline (no AI-generated commit signatures, no unwanted writes to deployed config, plans staying on-path) and acts as a guardrail against the kind of hallucinations that creep in when an agent starts improvising.
- **License + updates** are the only things that go over the network — a quick refresh of your cached license token and a check for newer skill versions (capped at once every 6 hours).
- **Skills** download from Cloudflare R2 via short-lived signed URLs the moment you have new ones queued, then sit in `~/.claude/skills/` until you remove them.
- **License token** is cached at `~/.claude/.bravros-auth` (mode `0600`) and is good for 30 days offline. Lose your network for a week — bravros keeps working.
- **Source code** of the CLI lives in a private repo. Only signed binaries are published here, and the installer verifies the signature before placing anything on your system.

---

## 🔒 Privacy & data

**We do not collect usage data.** No analytics, no crash reports, no command history, no file paths, no telemetry of any kind ships from your machine to us. The bravros binary contains zero third-party tracking SDKs (no Sentry, PostHog, Mixpanel, Amplitude, Segment, Datadog, Bugsnag — verified, you can grep the binary yourself).

The CLI makes outbound network calls **only** in these specific cases, and **only** sends the data listed:

| Triggered by | Destination | What's sent | Why |
|---|---|---|---|
| `bravros activate <key>` | `app.bravros.dev` | license key, hostname, OS, architecture, machine fingerprint (SHA-256 of hostname + MAC + OS + arch) | Activate license + populate your dashboard |
| Claude Code session start (every 6 h max) | `app.bravros.dev` | cached JWT only | Refresh license, drain pending skill installs |
| Skill download | Cloudflare R2 (via presigned URL) | nothing — URL is pre-signed | Fetch skill tarball |
| Self-update check | `app.bravros.dev` then GitHub Releases as fallback | OS + architecture | Fetch the right binary for your platform |

**What we do NOT do:**

- ❌ No analytics platforms in the binary
- ❌ No crash reporting
- ❌ No command-history or invocation tracking
- ❌ No file path or file content collection
- ❌ No "phone home" beyond the license + skill-sync calls listed above

**One thing we DO collect that you should know about:** your machine's hostname (e.g. `MacBook-Pro.local`) is sent on `bravros activate` and stored against your license so the [machines dashboard](https://app.bravros.dev/dashboard/machines) can show you which devices are activated. If you'd prefer this to be opaque, contact [support@bravros.dev](mailto:support@bravros.dev) — an `--anonymous` activate flag is on the roadmap.

You can verify all of this yourself by running the CLI behind any HTTP proxy (mitmproxy, Charles, Wireshark) and watching exactly what goes over the wire.

---

## 🔐 Verify the trust chain

Every release is signed. To verify a download yourself:

```bash
PUBKEY="RWQqHlahq4RjNnCasO/8yMsgtLGfdHejILKMxxpsulIs1rII6IgMO26G"

curl -LO https://github.com/bravros/bravros/releases/latest/download/checksums.txt
curl -LO https://github.com/bravros/bravros/releases/latest/download/checksums.txt.minisig

minisign -Vm checksums.txt -P "$PUBKEY"
# expected: "Signature and comment signature verified / Trusted comment: bravros release"
```

The pinned key is also published at [**bravros.dev/security**](https://bravros.dev/security). The installer script does this verification automatically before placing any binary on your system.

---

## 📦 What's in this repo

This is the **distribution repository.** The actual Go source for the CLI lives in a private repository — only signed pre-built binaries land here. The "Source code" tarballs auto-generated by GitHub on the [releases page](https://github.com/bravros/bravros/releases) contain only the public assets below — they cannot be used to compile the CLI.

| File / Dir | Purpose |
|---|---|
| `install.sh` | Public installer with embedded minisign verification |
| `config/settings.json` | Default Claude Code settings (hooks, statusline, plugins) |
| `config/mcp.json` | Default MCP server registry (sequential-thinking, context7, chrome-devtools, browsermcp) |
| `.goreleaser.yml` | Public release config — build runs in private, artifacts published here |
| `LICENSE` | Proprietary license terms |

---

## 🆘 Support

| Channel | Where |
|---|---|
| 📧 Email | [support@bravros.dev](mailto:support@bravros.dev) |
| 🌐 Web | [bravros.dev](https://bravros.dev) |
| 🔐 Security | [bravros.dev/security](https://bravros.dev/security) |
| 💬 Community | *coming soon* |

This repo does **not** track development issues — please reach the team via email.

---

## 📜 License

Proprietary — see [LICENSE](LICENSE).

Beta-period Pro licenses are issued free of charge but remain subject to the proprietary terms in `LICENSE`.

---

<div align="center">

<sub>Built with ☕ in Brazil</sub>

**[Get your free Pro license →](https://app.bravros.dev)**

</div>
