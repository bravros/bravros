# /git-this — bootstrap a private GitHub repo from the current folder

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

1. **Preflight** (one bash call): `gh auth status`; owner; sanitize folder name to a repo slug (lowercase, `[a-z0-9_-]`, abort if empty); detect `HAS_GIT` / `IS_EMPTY`; abort if origin set; check `gh repo view "$OWNER/$NAME"` for a collision.
2. **Collision** → announce (below), propose 3 free alternatives (suffix, year, or a name from package metadata / README H1), pick via ask_question; re-sanitize "Other" input; loop max 3.
3. **Create + wire**: `gh repo create "$OWNER/$NAME" --private`; `git init -b main` (or `git branch -m main`); `git remote add origin git@github.com:$OWNER/$NAME.git`; verify origin stuck.
4. **Scaffold** (Write tool, parallel calls): empty folder → `README.md` (`# {NAME}`) + `CLAUDE.md`; non-empty → `CLAUDE.md` only if missing. CLAUDE.md template below.
5. **Commit + push**: empty/new-git folder publishes everything; otherwise stage ONLY the scaffolded files. `bravros commit`, `git push -u origin main`, print repo URL summary. If the commit hook fails, don't roll back — repo and origin stay wired; the user pushes manually.

<!-- announce-template: "Nome do repositório já existe, aguardando sua escolha de alternativa. Projeto {PROJECT}." -->
```bash
bash ~/.bravros/scripts/announce.sh "Nome do repositório já existe, aguardando sua escolha de alternativa. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```

## CLAUDE.md template (substitute `{NAME}`)

```markdown
# {NAME} — Claude Code Context

A private personal repo — no production deploy gate, no PR pipeline, no CI.

- **Default branch for daily work: `main`.** Commit and push directly; no feature branches, no `homolog`.
- The global "never push directly to `main`" rule does not apply here — it gates production repos.
- Emoji commit format and the no-AI-signature rule **still apply**.
```
