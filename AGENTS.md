# Bravros — Agent Developer Contract

This file defines the rules and conventions for developing skills and tools in the Bravros ecosystem. Every agent executing inside this repository must follow these rules without exception.

## 1. Skill Formatting & Structure

- All skills reside in the `skills/` directory as `skills/<name>/SKILL.md`.
- Every skill must have a YAML frontmatter at the top:
  ```yaml
  ---
  name: <kebab-case-name>
  category: <sdlc|design|web|deploy|content|tools>
  description: <short-description>
  ---
  ```
- Keep skill instructions **≤40 lines above the fold** (before the first `##` section or reference links).
- Detailed examples, instructions, or templates must go into a `references/` subdirectory under the skill (e.g. `skills/<name>/references/`).
- Shell scripts or python scripts used by a skill must reside in a `scripts/` subdirectory under the skill (e.g. `skills/<name>/scripts/`).

## 2. Host-Neutrality Constraints

To ensure skills run on any agent host (Claude Code, Gemini CLI, Cursor, Codex, etc.), skills must be host-neutral:
- **NO Harness-Specific APIs:** Never use harness-specific tokens/calls like `AskUserQuestion`, `Agent(`, `mcp__`, `PromptUser`, or `DispatchSubagent`.
- **NO Host-Specific Paths:** Never hardcode host paths like `~/.claude` or `~/.gemini`.
- **Use the CLI for actions:** Route all atomic actions through `bravros` command line verbs (e.g. `bravros commit`, `bravros worktree`), which are implemented portably in Go.
- **Instruct the operator:** When human intervention is required, instruct the model to "ask the operator" rather than calling interactive APIs.

## 3. Repository Conventions

- **English Only:** All code, comments, identifiers, and commit messages must be in English.
- **Emoji Commits:** Commits must follow the `<emoji> <type>: <subject>` format.
  - Subject must be **50 characters or fewer**, present tense, lowercase.
  - Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `plan`, `security`, `deps`, `deploy`, `ci`.
  - Body (optional) goes after a blank line. Never add AI-attribution trailers.
- **Safety:** Never commit secrets, `.env` files, or host/device identifiers to the public repository.

## 4. Verification

- All skill changes must be verified against the host-neutrality linter in CI.
- Generated output files and platform configs are validated in CI against their expected golden versions.
