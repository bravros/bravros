---
name: git-this
category: tools
description: Bootstrap a private GitHub repo for the current folder and wire the origin remote. Invoke via /git-this.
---

# /git-this — bootstrap a private GitHub repo from the current folder

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

Creates `<owner>/<folder>` (private), wires `origin`, scaffolds `CLAUDE.md` declaring the
direct-main policy for personal/scratch repos. Owner: `gh api user -q .login`.

## Hard constraints

1. **Refuse if `origin` already exists** or `gh auth status` fails — never clobber a wired repo.
2. **Never overwrite** an existing `CLAUDE.md`. Create only if missing.
3. Commit via `bravros commit "✨ feat: initial commit"` — never raw `git commit`; no AI signatures.
4. **Use the Write tool, not bash heredocs**, for templates — keeps generated files out of bash quoting.
5. Each Bash call is a fresh shell — variables do NOT persist between steps; substitute literal values from earlier output.
6. `git rev-parse` fails when the folder isn't a repo yet — that is the normal case; fall back to `basename "$PWD"`.

## Flow

1. **Preflight**: `gh auth status`; owner; sanitize folder name to repo slug; check `gh repo view "$OWNER/$NAME"` collision.
2. **Collision**: announce, propose 3 free alternatives, ask user via user prompt, loop max 3.
3. **Create + wire**: `gh repo create "$OWNER/$NAME" --private`; `git init -b main`; `git remote add origin git@github.com:$OWNER/$NAME.git`.
4. **Scaffold**: empty folder → `README.md` (`# {NAME}`) + `CLAUDE.md`; non-empty → `CLAUDE.md` only if missing.
5. **Commit + push**: `bravros commit`, `git push -u origin main`, print repo URL summary.
