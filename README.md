# Bravros

<p align="center">
  <img src="docs/catalog/logo.jpg" alt="Bravros Logo" width="220" style="border-radius: 24px; box-shadow: 0 10px 30px rgba(0,0,0,0.3);" />
</p>

<p align="center">
  <strong>Host-Neutral Software Development Lifecycle (SDLC) Toolkit for AI Agents</strong>
</p>

<p align="center">
  <a href="https://github.com/bravros/bravros/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=for-the-badge&logo=github" alt="License" /></a>
  <img src="https://img.shields.io/badge/skills-33-3B82F6?style=for-the-badge&logo=anthropic&logoColor=white" alt="Skills" />
  <img src="https://img.shields.io/badge/bravros_cli-v0.1.0-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go CLI" />
  <img src="https://img.shields.io/badge/Security-Signed-8B5CF6?style=for-the-badge&logo=shield" alt="Minisign Security" />
</p>

---

Bravros is a free, public, MIT-licensed, host-neutral agent toolkit. It integrates a set of **33 workflow skills** with a compiled Go CLI kernel to enforce structured, atomic, and secure software development lifecycles directly within AI agents (like Claude Code, Gemini CLI, Cursor, and more).

Unlike toolkits that rely on heavy centralized servers, Bravros is **100% serverless, zero-touch, and has zero phone-home**. All skills are loaded dynamically from this public Git repository, and release binaries are signed cryptographically for absolute security.

---

## ⚠️ Repository Reset & Decommission (v2.x → v0.1.0)

Bravros was originally developed as a commercial, paid SaaS platform with license validation, Clerk auth, Turso databases, and Next.js dashboards. 

We have sunsetted the commercial model:
- **100% Free & Open Source:** The licensing API, Clerk, Turso, Stripe integration, and dashboard have been decommissioned.
- **Git as the Transport:** Skills update dynamically via the hosts' native marketplaces/Git endpoints.
- **Breaking Change for v2.x Users:** The old client binaries and dashboard are deprecated. Run the new open-source installer below to migrate to the `v0.1.0` release.

---

## 🚀 Key Features

*   **Host-Neutral Skills:** Write skill conventions once and run them across multiple agent hosts (Claude Code, Gemini, Cursor).
*   **Decoupled CLI Kernel:** Verbs are reserved only for operations a LLM prompt cannot perform on its own (atomic filesystem locks, out-of-band tokens, safety backups).
*   **Out-of-Band Security Tokens:** Dangerous operations (like `/promote` merges or `/destructive` deletions) are gated behind out-of-band tokens generated in a separate terminal. The agent cannot self-authorize a merge or deletion.
*   **Preserve-before-Delete:** Commands like `discard`, `clean-untracked`, and `trash` store snapshots in a tombstone folder before executing, preventing accidental loss of uncommitted work.
*   **Cryptographic Verification:** Every release includes signatures verified by `install.sh` against our pinned Minisign public key.

---

## 📦 Installation & Setup

Install Bravros directly to your machine:

```bash
curl -fsSL https://install.bravros.dev | sh
```

Or via Homebrew:

```bash
brew install bravros/tap/bravros
```

Initialize your project workspace:

```bash
bravros init
```

This command detects your project's technology stack, writes `.bravros/config.json`, registers git hooks under `.bravros/hooks/`, and configures the plan structure.

---

## 🛠️ The CLI Kernel Verbs

The `bravros` binary is written in Go, exposing subcommands designed to enforce workflow primitives:

| Category | Verbs | Description / Why it is a CLI Verb |
|---|---|---|
| **Commit & IDs** | `commit`, `nextid` | Format checks (emojis/conventions); atomic ID reservation across multiple worktrees. |
| **Locks & Tokens** | `merge-lock`, `promote`, `destructive`, `pr-review` | Cross-session merge locks and out-of-band human confirmation checks. |
| **Safety Snapshots** | `discard`, `trash`, `discard` | Snaps files to temporary tombstones before performing modifications. |
| **Git / Workspace** | `branch`, `worktree`, `config` | Automates worktree setups, PR queries, and `.bravros/` configuration parsing. |
| **Installation** | `install`, `deploy`, `selfupdate`, `init`, `doctor`, `hooks` | Handles updates, verifies environment state, and manages Git hook linking. |
| **Secrets** | `secrets`, `sa-token` | Keyring integration and 1Password secret injection (`op://` URLs). |

---

## 📜 License

MIT License — see [LICENSE](LICENSE) for details.

---

## 🧠 What Bravros Does: Visual & Metaphorical Guide

If you want to design alternative logo variations, here are the core concepts and visual metaphors that define Bravros:

### 1. The Interconnected Graph (Workflow Traversal)
Bravros is driven by **knowledge graphs** and structural connectivity. Using tools like `graphify`, the toolkit maps AST code components, COMMUNITY structures, and directories into a logical graph database.
*   *Visual elements:* Connected nodes, networks, structural grids, branching lines, constellation formations.

### 2. The Shield / Gates (Security & Constraints)
Security is a non-negotiable pillar. The CLI acts as a validator that checks formatting, enforces commit templates, and requires out-of-band tokens (cryptographic locks) to open a merge gate.
*   *Visual elements:* Shields, locks, keys, rings, gates, protective bounding boxes, concentric loops.

### 3. The Swarm (Multi-Agent Cooperation)
Bravros orchestrates autonomous subagents working in parallel (e.g. `SkillPorter`, `Orchestrator`, `leaf workers`). It represents parallel streams of execution merging into a single trunk.
*   *Visual elements:* Concentrated waves, arrows converging, hexagons, swarms, overlapping layers, gears.

### 4. The Loop / Pipeline (Automated SDLC)
The lifecycle runs in a continuous circle: `/backlog` ➔ `/plan` ➔ `/orchestrate` ➔ `/pr` ➔ `/pr-review` ➔ `/finish`.
*   *Visual elements:* Infinity symbols, dynamic circular arrows, progress trackers, linear segments wrapping into a circle.
