# Bravros

Bravros is a free, public, MIT-licensed, host-neutral agent toolkit designed to manage and run skills across different AI agent hosts (such as Claude Code, Gemini CLI, and others).

## ⚠️ Repository Reset & Decommission (v2.x → v0.1.0)

Bravros was originally developed as a commercial/paid product with licensing gates (Clerk, Stripe, Turso, Cloudflare R2, and a Next.js server). 

We have decided to sunset the commercial model and make Bravros **100% free, MIT-licensed, and fully open source**. As part of this transition:
- The commercial SaaS stack (Clerk, Stripe, Turso, and the licensing API) has been decommissioned.
- No license keys or registration are required.
- End-user auto-update now pulls skills directly from this public repository.
- **Breaking Change for v2.x Users:** The old client binaries, dashboard, and license activation commands are deprecated and non-functional. If you have a legacy `v2.x` binary installed (e.g. via Homebrew), you should uninstall it and follow the new installation instructions below to transition to the open-source `v0.1.0` release.

---

## 🚀 Features

- **Host-Neutral Skills:** Write skill conventions once (`skills/`) and run them natively on Claude Code, Gemini CLI, and other supported platforms.
- **Zero Phone-Home:** With licensing layers removed, the CLI tool communicates only with GitHub to fetch public releases and updates.
- **Background Auto-Updates:** Uses the native auto-update features of hosts (like Claude Code plugin marketplaces) to propagate skill updates to your machine instantly with no user intervention.
- **Signed Distribution:** Re-uses Goreleaser + Minisign signing to deliver verifiable, secure binaries.

---

## 📦 Installation

To install Bravros, run the installer:

```bash
curl -fsSL https://install.bravros.dev | sh
```

Or via Homebrew:

```bash
brew install bravros/tap/bravros
```

Then initialize:

```bash
bravros init
```

For platform-specific details and manual steps for configuring git hooks, please refer to [docs/](docs/).

## 📜 License

MIT License — see [LICENSE](LICENSE) for details.
