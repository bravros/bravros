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
7. **Direct-to-main unless `WANT_HOMOLOG`.** After `git init -b main`, run `bravros police direct-main on` (writes `.bravros/config.json` with `police.direct_main: true`) and include that file in the initial commit — skipped otherwise, the police hook blocks the first `git push -u origin main` (observed 2026-09-10 on a freshly scaffolded personal site repo). With `WANT_HOMOLOG` the file carries `{"staging_branch": "homolog"}` instead and the first push to `main` is the one and only direct push.

## Flow

1. **Preflight**: `gh auth status`; owner; sanitize folder name to repo slug; check `gh repo view "$OWNER/$NAME"` collision.
2. **One ask**: "Set up a `homolog` staging branch from day one?" (default **No** — most personal repos are main-only). Record `WANT_HOMOLOG`.
3. **Collision**: announce, propose 3 free alternatives, ask user via user prompt, loop max 3.
4. **Create + wire**: `gh repo create "$OWNER/$NAME" --private`; `git init -b main`; `git remote add origin git@github.com:$OWNER/$NAME.git`. Then, per constraint 7: no `WANT_HOMOLOG` → `bravros police direct-main on`; `WANT_HOMOLOG` → write `.bravros/config.json` with `{"staging_branch": "homolog"}` instead. Do NOT create `homolog` yet — there is no commit for it to point at, and `git push -u origin homolog` fails with `src refspec homolog does not match any`.
5. **Scaffold**: empty folder → `README.md` (`# {NAME}`) + `CLAUDE.md`; non-empty → `CLAUDE.md` only if missing. The CLAUDE.md branch-policy paragraph switches on `WANT_HOMOLOG` (direct-main text vs `feature/* → homolog → main`). `.bravros/config.json` from step 4 is always included.
6. **Commit + push**: `bravros commit` (stage `.bravros/config.json` alongside the scaffolded files), `git push -u origin main`. Then, only with `WANT_HOMOLOG`: `git branch homolog main && git push -u origin homolog` — stay on `main`. Print repo URL summary.
