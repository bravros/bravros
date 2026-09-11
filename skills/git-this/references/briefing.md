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
7. **Direct-to-main unless `WANT_HOMOLOG`.** A scaffolded repo has no staging branch unless the operator asked for one (step 2 below), so after `git init -b main` the police hook must be told main is not gated: run `bravros police direct-main on`, which writes `.bravros/config.json` with `police.direct_main: true`. Include that file in the initial commit. Skip this and the police hook blocks the very first `git push -u origin main` (observed 2026-09-10 on a freshly scaffolded personal site repo) — the operator is then stuck mid-flow with a wired-but-unpushed repo. With `WANT_HOMOLOG` the file carries `{"staging_branch": "homolog"}` instead of `direct_main`; the initial `git push -u origin main` is then the one and only direct push, and `homolog` is cut from that commit afterwards (step 6).

## Flow

1. **Preflight** (one bash call): `gh auth status`; owner; sanitize folder name to a repo slug (lowercase, `[a-z0-9_-]`, abort if empty); detect `HAS_GIT` / `IS_EMPTY`; abort if origin set; check `gh repo view "$OWNER/$NAME"` for a collision.
2. **One ask**: "Set up a `homolog` staging branch from day one?" — default **No**, most personal/scratch repos scaffolded here are main-only; the operator has asked for the homolog-from-day-one option once before, so offer it rather than assume. Record the answer as `WANT_HOMOLOG`.
3. **Collision** → announce (below), propose 3 free alternatives (suffix, year, or a name from package metadata / README H1), pick via ask_question; re-sanitize "Other" input; loop max 3.
4. **Create + wire**: `gh repo create "$OWNER/$NAME" --private`; `git init -b main` (or `git branch -m main`); `git remote add origin git@github.com:$OWNER/$NAME.git`; verify origin stuck. Then apply constraint 7: `WANT_HOMOLOG` unset/No → `bravros police direct-main on`; `WANT_HOMOLOG` yes → write `.bravros/config.json` with `{"staging_branch": "homolog"}` instead (no `direct_main`). Do NOT create `homolog` here — no commit exists yet, so `git checkout -b homolog && git push -u origin homolog` dies with `src refspec homolog does not match any`; the branch is cut in step 6.
5. **Scaffold** (Write tool, parallel calls): empty folder → `README.md` (`# {NAME}`) + `CLAUDE.md`; non-empty → `CLAUDE.md` only if missing. `.bravros/config.json` from step 4 is always present. CLAUDE.md template below — pick the branch-policy paragraph by `WANT_HOMOLOG`.
6. **Commit + push**: empty/new-git folder publishes everything; otherwise stage ONLY the scaffolded files plus `.bravros/config.json`. `bravros commit`, `git push -u origin main`. Then, only with `WANT_HOMOLOG`: `git branch homolog main && git push -u origin homolog` — stay on `main`, never `checkout` the new branch. Print repo URL summary. If the commit hook fails, don't roll back — repo and origin stay wired; the user pushes manually.

<!-- announce-template: "Nome do repositório já existe, aguardando sua escolha de alternativa. Ramo principal, projeto {PROJECT}." -->
```bash
bash ~/.agent_config/scripts/announce.sh --force "Nome do repositório já existe, aguardando sua escolha de alternativa. Ramo principal, projeto $(basename "$PWD")." studio || true
```

## CLAUDE.md template (substitute `{NAME}`; keep ONE branch-policy paragraph)

```markdown
# {NAME} — Claude Code Context

A private personal repo — no production deploy gate, no PR pipeline, no CI.

<!-- WANT_HOMOLOG = No (direct-main) -->
- **Default branch for daily work: `main`.** Commit and push directly; no feature branches, no `homolog`.
- The global "never push directly to `main`" rule does not apply here — it gates production repos.
<!-- WANT_HOMOLOG = Yes (staging branch) -->
- **Branch model: `feature/*` → `homolog` → `main`.** Cut feature branches from `origin/homolog`;
  `homolog` is directly pushable, `main` moves only through a PR from `homolog`.
<!-- both -->
- Emoji commit format and the no-AI-signature rule **still apply**.
```
