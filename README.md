<p align="center">
  <img src="docs/catalog/logo.jpg" alt="Bravros" width="200" style="border-radius: 24px;" />
</p>

<h1 align="center">Bravros</h1>

<p align="center">
  <strong>A host-neutral SDLC toolkit for coding agents.</strong><br />
  Free, MIT, no account, no server, no telemetry.
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-22C55E?style=for-the-badge" alt="MIT" /></a>
  <img src="https://img.shields.io/badge/skills-35-3B82F6?style=for-the-badge" alt="35 skills" />
  <img src="https://img.shields.io/badge/cli-Go-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go CLI" />
  <img src="https://img.shields.io/badge/releases-minisign-8B5CF6?style=for-the-badge&logo=letsencrypt&logoColor=white" alt="Signed" />
  <img src="https://img.shields.io/badge/telemetry-none-64748B?style=for-the-badge" alt="No telemetry" />
</p>

---

Coding agents are good at writing code and bad at remembering how *your* team ships it. Bravros
supplies the missing half: a set of **35 workflow skills** that give the agent a repeatable
lifecycle, and a small **Go kernel** for the handful of operations a prompt must never be trusted
to improvise — atomic locks, human-presence gates, and preserve-before-delete.

Skills are authored in this public repository and ship **embedded in the binary**, so installing
is one signed download and refreshing them needs no network at all. Plugin hosts that prefer to
own the skill tree themselves still can. There is no dashboard, no licence check, and nothing to
log into.

---

## ⚡ Install

The binary carries the skills and templates **inside it**. One download, no clone, no GitHub
token, no account — and every byte is minisign-verified before it lands. See
[Verify the trust chain](#-verify-the-trust-chain).

### macOS and Linux

```bash
bash -c "$(curl -fsSL https://install.bravros.dev)"
```

The installer downloads and verifies the binary, puts it in `~/.claude/bin`, adds that directory
to your `PATH`, writes an uninstaller next to it, and then hands off to `bravros setup` — the
component wizard, which is where every question is asked.

The older piped form still works and still reaches the wizard:

```bash
curl -fsSL https://install.bravros.dev | sh
```

Both are supported on purpose. Under `curl … | sh` the pipe *is* stdin, so nothing in the script
could ever prompt; `bash -c "$(…)"` uses command substitution, which leaves stdin attached to
your terminal. The advertised form is therefore `bash -c "$(…)"`, and the script additionally
runs its own body with `/dev/tty` bound as stdin when stdin is not a terminal, so the bookmarked
pipe keeps working. On a CI runner with no controllable terminal both forms fall through non-interactively,
install the binary, and print the command to finish setup by hand.

Or with Homebrew:

```bash
brew install bravros/tap/bravros
```

A Homebrew-managed install is detected: the script **aborts instead of shadowing it**, because
two `bravros` binaries on `PATH` means `brew upgrade` moves one and the installer moves the
other, and which one runs depends on `PATH` order you never see.

### Windows — WSL is the recommended path

Install [WSL](https://learn.microsoft.com/windows/wsl/install), then run the Linux one-liner
above inside it. WSL is a first-class tier; native Windows is not.

<details>
<summary><strong>Native Windows (supported, but a documented degraded tier)</strong></summary>

```powershell
irm https://install.bravros.dev/install.ps1 | iex
```

> **Prerequisite / known issue.** This one-liner depends on `install.bravros.dev` routing
> `/install.ps1` to the release asset. That route is an outstanding infrastructure task and is
> **not confirmed working** — if it 404s, download `install.ps1` from the
> [latest release](https://github.com/bravros/bravros/releases/latest) and run it locally. The
> asset itself ships correctly on every release and is covered by `checksums.txt`.

`install.ps1` is a near line-by-line mirror of `install.sh` — same trust chain, same install
layout (`%USERPROFILE%\.claude\bin`, mirroring the POSIX `~/.claude/bin`), same hand-off to
`bravros setup`. It targets PowerShell 5.1, which ships in the box. A scoop- or winget-managed
install is detected and refused rather than shadowed, exactly like Homebrew on POSIX.

What is specifically worse than the POSIX path — the three known items, not a vague warning:

1. **The SessionStart hook has no desktop-app guard.** On macOS and Linux the hook is
   `sh -c 'case "$__CFBundleIdentifier" in com.anthropic.*) exit 0;; esac; exec $HOME/.claude/bin/bravros …'`
   so it is a no-op inside the Claude desktop app. `cmd.exe` cannot parse a shell `case`, and the
   `__CFBundleIdentifier` condition is macOS-only, so the Windows hook is the bare command
   `%USERPROFILE%\.claude\bin\bravros.exe …` with no guard
   (`cli/internal/managed/settings_windows.go`).
2. **`bravros update` leaves a `.old-*` file behind, and has a crash window POSIX does not.** On
   POSIX, `rename(2)` atomically replaces the running executable and the process keeps its
   unlinked inode — there is never an instant with no binary on disk. Windows locks a running
   image against replacement, so the update must rename the current binary aside first, install
   over the freed name, and then fail to delete the sideline (a running image cannot delete its
   own file). The leftover `bravros.exe.old-<rand>` is swept on a later run, and a crash between
   the two renames is a real, if narrow, window (`cli/internal/selfupdate/binary.go`).
3. **minisign is bootstrapped by download.** POSIX gets minisign from your package manager. On
   Windows the installer fetches a **pinned** `minisign.exe` from jedisct1's official win64
   release and checks it against a SHA-256 pinned in the script before using it. It fails closed
   — a stale pin aborts rather than degrading to no verification — but it is one more download
   than the POSIX path needs, and the pin must be bumped on each minisign release.

</details>

### Download the archive directly

Releases publish **6 archives** — `.tar.gz` for macOS and Linux, `.zip` for Windows. The asset
names stay machine-shaped so that the installer, GoReleaser and the Homebrew formula can all
compute them; the friendly names below are for humans only.

| You have | Download |
|---|---|
| **Mac (M series)** | `bravros-darwin-arm64.tar.gz` |
| **Mac (Intel)** | `bravros-darwin-amd64.tar.gz` |
| **Linux** | `bravros-linux-amd64.tar.gz` |
| **Linux (ARM)** | `bravros-linux-arm64.tar.gz` — **newly supported** |
| **Windows** | `bravros-windows-amd64.zip` |
| **Windows (ARM)** | `bravros-windows-arm64.zip` |

All six live under
[`releases/latest/download/`](https://github.com/bravros/bravros/releases/latest), alongside
`install.sh`, `install.ps1`, `checksums.txt` and `checksums.txt.minisig`. Extract the binary to
`~/.claude/bin` (`%USERPROFILE%\.claude\bin` on Windows), then run `bravros setup`.

### What you actually get

`bravros setup` is the wizard the installer hands off to. It writes into `~/.claude` from the
payload **embedded in the binary** — no network, no source checkout — and asks you which of four
components you want:

| Component | Goes to | What it is |
|---|---|---|
| `cli` | `~/.claude/bin` | The binary itself. Required; the installer already placed it. |
| `claude-skills` | `~/.claude/skills` | The agent skills, at scope `core` (default, 18 skills) or `all` (35). |
| `claude-templates` | `~/.claude/templates` | Git hooks and project templates used by `bravros init` and the commit-msg gate. |
| `claude-settings` | `~/.claude/settings.json` | The managed settings block (the SessionStart hook), deep-merged into any existing file. |

There is one selection axis — components — and no plugin-category picker. `core` is the default
skill scope on purpose: an always-on skill list has a context budget, and blowing it silently
hides your least-used skills. `--skills=all` opts into the long tail.

Non-interactive forms, for CI and for dotfile scripts:

```bash
bravros setup --all --yes                                          # everything, skills scope=all
bravros setup --components=claude-skills,claude-settings --yes     # a subset
bravros setup --skills=all --yes                                   # every skill, default components
BRAVROS_COMPONENTS=claude-skills bravros setup --yes               # same, via the environment
```

Re-running `setup` is idempotent and **never destructively overwrites**: a file that already
exists and differs is left exactly as it is, and the payload's version is written beside it as
`<name>.new` and reported. `settings.json` is deep-merged entry by entry, never replaced. Your
choice is recorded in `~/.claude/state/setup.json`, which is what the SessionStart refresh reads
later. A plugin-managed Claude Code install is **detected and warned about** — bravros never
writes into a directory a plugin host owns.

Then, once per repository:

```bash
bravros init
```

That detects your stack, writes `.bravros/config.json`, and installs the `commit-msg` and
`pre-push` hooks under `.bravros/hooks/` via `core.hooksPath`. No global state, nothing outside
the repo.

### Keeping it current

Two verbs, deliberately split — full detail in [**Skill Delivery**](docs/skill-delivery.md):

| | What it does | Network? |
|---|---|---|
| `bravros selfupdate` | Runs from the SessionStart hook. Refreshes your components from the payload **embedded in the binary you already have**, and at most once a day prints a one-line "a newer version exists" notice. | No (except that notice) |
| `bravros update` | You run it. Resolves the newest release, downloads it, verifies the minisign signature, replaces the running binary, then refreshes components from the new embedded payload. | Yes |

`bravros update` refuses when a package manager owns the binary and names the right command
instead (`brew upgrade bravros`). `BRAVROS_NO_UPDATE_CHECK=1` turns off the passive notice.

### Other hosts

Claude Code's plugin marketplace remains supported, and is the right choice if you would rather
Claude Code own the skill tree:

```
/plugin marketplace add bravros/bravros
/plugin install bravros
```

Gemini CLI has its own extension system:

```bash
gemini extensions install https://github.com/bravros/bravros --auto-update
```

Never run both for the same skills — the CLI detects a plugin-managed install and refuses to
write there, but pointing two updaters at one tree is a conflict, not redundancy.

---

## 🚀 The loop

```
/recon  ➔  /orchestrate  ➔  /pr  ➔  /finish
```

```bash
/recon the checkout total is wrong for orders with a coupon
```

`/recon` takes a symptom, a screenshot, a stack trace, or a feature idea and produces **one
dossier folder** — `.planning/P-NNNN-<slug>/` — carrying the brief, the constraints that must not
change, the closed decisions, the traps, and phased tasks with per-phase verify commands.

If it's a defect, `/recon` hands the hunt to `/scout`, which queries the code graph, traces the
real execution path, and **certifies** the cause with runtime proof before anything is written
down. No certification, no diagnosis — it reports `UNCERTIFIED` rather than shipping a guess.

```bash
/orchestrate .planning/P-0001-coupon-total/
```

`/orchestrate` executes that folder: it dispatches phases to subagents by complexity marker,
verifies each phase against its own `Verify:` command, and commits as it goes. `/pr` opens the
pull request; `/finish` merges it and records the outcome.

Nothing about the loop is mandatory. `/quick` exists for a two-line fix, and `/backlog` for an
idea you are not ready to act on.

---

## 🧰 What you can do with it

**Ship a change end to end**

| | |
|---|---|
| `/recon` | Turn a bug report, screenshot, or feature idea into a reviewed dossier with phases and acceptance criteria |
| `/scout` | Find the culprit and certify it with runtime proof — never edits code |
| `/orchestrate` | Execute a dossier phase by phase, verifying each one before moving on |
| `/quick` | Small contained fix, no ceremony |
| `/commit` `/ship` `/push` | Formatted commits, enforced by hook rather than hope |
| `/pr` `/pr-review` `/address-pr` `/local-review` | Open, review, and answer review feedback |
| `/finish` `/promote` `/after-merge` | Merge, release to production behind a human gate, and run the post-deploy checklist |

**Keep a repo healthy**

| | |
|---|---|
| `/backlog` | Park ideas with enough structure to judge later |
| `/triage-sweep` | Drain a stale issue and backlog queue, verifying every close against live code |
| `/batch-merge-prs` `/prune-merged` | Land a queue of PRs, then clean up the branches |
| `/doctor-plus` `/verify-install` | Health-check the toolkit and the workspace |
| `/context` | Generate or audit `CLAUDE.md` / `AGENTS.md` from the actual code |
| `/worktree` | Isolated worktrees with real provisioning |

**Understand a codebase**

| | |
|---|---|
| `/graphify-this-project` | Build a queryable knowledge graph of the code, committed and refreshed on merge |
| `/graphify-status` | Report label coverage across every graph on the machine |
| `/interview-me` | Stress-test a plan round by round until nothing is silently assumed |
| `/advise-project-approach` | Stack and architecture advice grounded in real comparables |

Add the category plugins for the rest, or ignore them. `bravros --help` lists the kernel verbs.

---

## 🛠️ Why there is a CLI at all

Most of the toolkit is prose, because a 2026 model sequences work better than a step list does.
The Go binary exists only for the things a prompt genuinely cannot do:

| Category | Verbs | Why it must be code |
|---|---|---|
| **Format & identity** | `commit`, `nextid` | Commit format is enforced by a hook, not a suggestion. IDs are reserved atomically across worktrees. |
| **Human presence** | `promote`, `destructive`, `pr-review` | The session must be **unable** to mint its own authority. Tokens come from a separate terminal you control. |
| **Atomicity** | `merge-lock` | Two sessions must not merge at once. |
| **Preserve before delete** | `discard`, `trash`, `clean-untracked` | Content git has never seen is copied to `.trash/` before anything removes it. |
| **Provisioning** | `worktree`, `branch`, `config`, `init`, `hooks` | Real filesystem and git work. |
| **Distribution** | `setup`, `update`, `selfupdate`, `deploy`, `install` | Installer machinery: the component wizard, the signed self-replace, and the embedded-payload refresh. |
| **Secrets** | `secrets set`, `secrets sa-token` | Keyring and `op://` resolution — values never touch a prompt. |

If a rule can be enforced by code, it lives in the binary. If it can't, it's stated once as a
hard constraint in a skill and nowhere else.

---

## 🔒 Safety

**The agent cannot authorize its own dangerous actions.** That's the design, not a policy note.

- **Production merges need you.** `main` is protected by GitHub branch rules and a `pre-push` hook
  that refuses `refs/heads/main`. That pair is the control that actually holds, because branch
  protection lives on a server the agent does not run on. Layered on top, `bravros promote unlock`
  refuses to mint a token when it detects an agent session, so a session cannot casually
  self-authorize. **Know the limit:** the token is an unsigned JSON file on disk — a guardrail
  against accidents and drive-by self-approval, not a cryptographic barrier. Branch protection is
  what stands between an agent and production; the token is what stops it happening by mistake.
- **Deletion preserves first.** `discard`, `trash`, and `clean-untracked` copy into `.trash/`
  before removing anything, and are reversible. Permanently destroying content that git has never
  seen requires a single-use token, again minted out of band.
- **Commit hygiene is enforced, not requested.** The `commit-msg` hook rejects malformed subjects
  and strips AI attribution trailers. A skill that says "never do X" without a mechanism behind it
  is labelled as lore, not presented as protection.
- **Secrets stay out of context.** `secrets` and `sa-token` resolve from your keyring or 1Password
  at the point of use; values are never pasted into a prompt or committed.
- **Installers are the highest-risk code path**, so they behave accordingly: settings files are
  merged, never overwritten, and a backup is written before any change.

## 🕵️ Privacy

**Bravros makes no network calls to us, because there is no "us" to call.** There is no server, no
account, no licence check, no dashboard.

- ❌ No analytics, no crash reporting, no usage or command tracking
- ❌ No file paths, file contents, or prompts leave your machine
- ❌ No third-party SDKs in the binary — grep it yourself
- ✅ The only outbound traffic is **GitHub releases**: fetching a release archive when you run
  `bravros update`, and one "is there a newer version" check at most once a day (off with
  `BRAVROS_NO_UPDATE_CHECK=1`). Skills come from the binary you already have, so refreshing them
  is a local file copy. All of it is watchable on the wire.

Verify it the same way you would verify anyone's claim — run the CLI behind mitmproxy, Charles, or
Wireshark and watch the wire.

---

## 🔐 Verify the trust chain

Every release is signed with minisign. `install.sh`, `install.ps1` and `bravros update` all check
that signature before anything is written to disk, against a public key pinned in the script and
compiled into the binary. To verify by hand:

```bash
PUBKEY="RWQqHlahq4RjNnCasO/8yMsgtLGfdHejILKMxxpsulIs1rII6IgMO26G"

curl -LO https://github.com/bravros/bravros/releases/latest/download/checksums.txt
curl -LO https://github.com/bravros/bravros/releases/latest/download/checksums.txt.minisig

minisign -Vm checksums.txt -P "$PUBKEY"
# expected: Signature and comment signature verified
#           Trusted comment: Bravros release v2.9.0
```

The same key is published at [bravros.dev/security](https://bravros.dev/security). Because the
source is public, you can also just build it yourself: `cd cli && go build .`

---

## 📦 What's in this repo

| Path | What it is |
|---|---|
| `skills/` | The 35 skills — source of truth. Each is a `SKILL.md` plus `references/` loaded on demand. 18 carry `core: true`. |
| `cli/` | The Go kernel. `cli/internal/payload/` holds the synced copy of `skills/` + `templates/` that gets embedded into the binary. |
| `plugins/` | Generated per-category plugin trees. Do not edit — `tools/skillgen` rewrites them. |
| `tools/skillgen` | Generates the per-host always-on files and lints skills for host-specific tokens. |
| `tools/cataloggen` | Builds `docs/catalog/catalog.json` from skill frontmatter. |
| `.claude-plugin/` | Claude Code marketplace + plugin manifests. |
| `gemini-extension.json` | Gemini CLI extension manifest. |
| `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.cursorrules` | Generated per-host contracts. Edit the source, not these. |
| `install.sh`, `install.ps1`, `.goreleaser.yml` | Signed release and install path — POSIX and native Windows. |

Skills are authored **host-neutral**: no harness-specific tool names, no absolute paths. CI fails
the build if one slips in, which is the only reason the same skill runs unchanged on four hosts.

---

## 📜 License

MIT — see [LICENSE](LICENSE). Contributions welcome via issues and pull requests; the skill catalog
is curated, so open an issue before a large addition.
