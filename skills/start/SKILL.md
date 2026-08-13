---
name: start
core: true
description: EXPLICIT-INVOCATION ONLY — trigger only when the user types /start. Initializes a new project with stack-aware CLAUDE.md, .bravros.yml, .gitignore, and base structure. Do NOT trigger on natural-language phrases like init or setup without the slash.
---

# /start — initialize or refresh project workflow files

Requires a git repo. **Update mode** if `.githooks/` or `.github/workflows/claude.yml`
exists; else **Init mode**. Report the detected mode. Init: `cp -n` everywhere, never
overwrite. Update: NEVER touch an existing CLAUDE.md; refresh `claude.yml` only.

## Steps

1. **Detect stack** from project markers (composer.json+laravel/framework → laravel; package.json "next" → nextjs; "react-native"/"expo" → expo; other package.json → nodejs; go.mod → go; requirements.txt/pyproject.toml → python; else generic). **Cache it in `.bravros.yml`** (`stack:` block) — that file is the project's stack cache; later sessions and skills read it instead of re-detecting.
2. **CLAUDE.md** (Init only). Laravel fast path: `cp -n ~/.agent_config/templates/CLAUDE.md CLAUDE.md`, fill its placeholders — do not modify that template. Other stacks: generate from `references/claudemd-templates.md`. Never use the Laravel template as a base for non-Laravel projects.
3. **sync-db.sh** (relational-DB projects only): `cp -n ~/.agent_config/templates/sync-db.sh` + `.db-sync.env.example`, `chmod +x`, `mkdir -p database/backups`. Non-Laravel: swap the post-restore command (Prisma → `npx prisma migrate deploy`, Drizzle → `npx drizzle-kit push`). Gitignore `.db-sync.env` and `database/backups/`.
4. **Hooks + planning dir**: `git config core.hooksPath .githooks`; `mkdir -p .planning`. **Update mode — don't clobber graphify's hooks:** if the repo has `.graphify` or `graphify-out/graph.json`, the `post-{merge,commit,checkout}` slots are graphify refresh delegators — preserve them.
5. **`.bravros.yml` staging branch.** Legacy `.bravros.yml` → `git mv` to `.bravros.yml`. If the file is missing, announce (below), then ask_question: "What is your staging/integration branch name?" (default `homolog`); write `staging_branch: <answer>` with the Write tool.
6. **Homolog branch before workflows.** If neither `refs/heads/homolog` nor `origin/homolog` exists: `git checkout -b homolog && git push -u origin homolog` (no origin is fine), then switch back.
7. **GitHub Actions** (only for homolog→main repos): write `claude.yml` + `tests.yml` per `references/github-workflows.md` — its GitHub gotchas are hard-won, do not deviate. Starter-kit workflow cleanup: fresh-init repos (≤1 commit) remove other workflows automatically; brownfield repos require explicit ask_question approval — never delete silently.
8. **graphify section**: if a graph exists and CLAUDE.md lacks a `## graphify` heading, append the section from `~/.agent_config/skills/graphify-this-project/references/claude-md-section.md`, filling real counts/labels — never ship placeholders.
9. **Report** created/skipped files and next steps. Don't commit automatically — the user reviews first.

<!-- announce-template: "Aguardando o nome do ramo de homologação para configurar o projeto. Projeto {PROJECT}." -->
```bash
bash ~/.agent_config/scripts/announce.sh "Aguardando o nome do ramo de homologação para configurar o projeto. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true
```

Use $ARGUMENTS for any additional context.
