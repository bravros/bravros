# Setup runbook — /graphify-this-project

The full step sequence. Order matters where marked: the version pin (Step 3) must precede any extraction, and `.gitignore` surgery (Step 2) must precede any commit of `graphify-out/`.

## Step 0 — Resolve project root + state file

```bash
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"; cd "$REPO_ROOT"; mkdir -p graphify-out
```

Canonical name for `.graphify` / CLAUDE.md: prefer `.bravros.yml`'s `remote:` (`basename <remote> .git`), then `git config remote.origin.url`, then the directory basename. An existing `graphify-out/graph.json` is fine — extraction refreshes in place.

Write the git-tracked `.graphify` state file (if it exists: ensure `context_path` + hook-tuning keys are present, leave user edits intact):

```yaml
version: 1
opt_in: true
canonical_name: <canonical>
policy: auto                           # auto | manual | disabled
context_path: graphify-out/graph.json  # canonical field — rename a stale graph_path: to it

# Hook tuning (post-merge model): rebuild ONLY on post-merge to the autocommit branch.
ast_on_post_merge: true
ast_on_post_commit: false
pre_push_extension: true
autopush_context: false    # disables only the legacy ~/Sites/context mirror; the hook still pushes graphify-out/
```

AST run state (`last_ast_run`) lives in the **untracked** `.graphify-state` sibling, never in `.graphify`.

## Step 1 — Detect stack, pick ignore template

Detect from root manifests (`composer.json` + `laravel/framework` in the lock → Laravel template; anything else/mixed → generic):

| Stack | Template |
|---|---|
| php / laravel / livewire / pest | `references/graphifyignore-laravel.txt` |
| everything else | `references/graphifyignore-generic.txt` |

Existing `.graphifyignore` → ask before overwriting; default to appending missing patterns.

## Step 2 — `.graphifyignore` + `.gitignore` surgery

**2a.** Merge the project's `.gitignore` with the stack template inside the auto-marker block (`# >>> graphify: auto-synced …` / `# <<< …`); patterns outside the markers are user-owned. Opinion: exclude `tests/` — it pollutes community labels and inflates god-nodes.

**2b.** Make `graph.json` trackable — if `.gitignore` has a blanket `graphify-out/` line, **remove only that line**, then append this scratch block (idempotent on the `# graphify scratch` marker):

```gitignore
# graphify scratch (commit graph.json + community-labels.json + GRAPH_REPORT.md + swarm helpers; ignore the rest)
graphify-out/cache/
graphify-out/*.html
graphify-out/manifest.json
graphify-out/.graphify_*
graphify-out/groups/
graphify-out/label-batches/
graphify-out/memory/
.graphify-state
```

Then `find . -maxdepth 2 -type d` and call out anything massive/unusual before it goes to an LLM.

## Step 3 — Install + pin graphifyy (NEVER skip)

```bash
bash <skill-dir>/scripts/apply-dedup-fix.sh    # idempotent; installs the pin from references/.graphify-version with the [mcp] extra
```

- The `[mcp]` extra is required or the user-scoped `graphify-mcp` server dies on import.
- The old 0.8.1 dedup patch is a no-op now (upstream merged it in 0.9 #1504); the script gates it off — applying the vendored v0.7.10 copy onto 0.9.x would downgrade extraction.
- **Every machine must run the same pin.** 0.8.1 vs 0.9.x produce structurally different graphs from identical code (paylog: 26,240 vs 59,383 nodes) — mixed pins overwrite each other's committed graph on every refresh and `resync-labels.py` aborts (~7–19% node-identity match), leaving the graph unlabeled. Symptom: `graphify query` answers `Community NN`. Verify with `/graphify-status`.
- **Upgrading the pin invalidates every existing label** — budget one full relabel per project.

## Steps 4–5 — Extraction

**🥇 DEFAULT — AST + external labelling prompt (how paylog, afterpay, and claude were actually built):** free AST extract, then the Step 6 external-prompt labelling; zero API cost.

```bash
PY=$(find ~/.local/share/uv/tools/graphifyy -name python3 -type l | head -1)
"$PY" <skill-dir>/scripts/ast-only-extract.py .
```

Paid semantic alternatives (only when the user explicitly wants LLM-derived semantic *edges*; comparison in `references/llm-routing-table.md`):

- **DeepSeek headless:** `bash <skill-dir>/scripts/extract-deepseek.sh .` (~$0.50–2.00, ~10–20 min, comes out labelled — skip Step 6).
- **DeepSeek 10-terminal opencode swarm** (max quality, big repos): AST extract, then `sed -i.bak 's/MAX_PER_GROUP = 50/MAX_PER_GROUP = 200/' <skill-dir>/scripts/make-code-groups.py && uv run python <skill-dir>/scripts/make-code-groups.py .` (restore the `.bak`); HAND OFF — the operator opens 10 opencode terminals on DeepSeek, pastes `references/worker-prompt.md` as system context, dispatches one group per task line ("Process group `<name>`. Files in `graphify-out/groups/<name>.txt`. Write JSON to `graphify-out/.graphify_chunk_<name>.json`"), biggest first; then `bash graphify-out/finalize.sh`. Announce the handoff:
  <!-- announce-template: "Extração do grafo pronta, abra os terminais externos conforme as instruções. Projeto {PROJECT}." -->
  `bravros ha say --force "Extração do grafo pronta, abra os terminais externos conforme as instruções. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true`
- **Gemini:** `zsh -c 'source ~/.zshrc.local 2>/dev/null; opload GEMINI_API_KEY 2>/dev/null; graphify extract . --backend gemini'` (~5 min, ~$0.07).
- **Claude Haiku swarm** (last resort): AST extract + `make-code-groups.py` (50-file cap), confirm agent count with the user, one Agent per group (`model=haiku`, embed `references/worker-prompt.md`), then `bash graphify-out/finalize.sh`.
- **Kimi:** `graphify extract . --backend kimi` (keys are region-specific, `.ai` vs `.cn`).

Sanity: `graphify-out/graph.json` ≈ code-files × 5 nodes for Laravel. Then always:

```bash
uv run python <skill-dir>/scripts/strip-framework-verbs.py graphify-out/graph.json   # idempotent
```

## Step 6 — Community labelling (skip if a paid path already labelled)

Count unlabelled communities:

- **≤ 25 → label INLINE yourself** — read the sample node labels, name each (2–4 words, lowercase-hyphenated, English, unique vs existing `community-labels.json`), write the results JSON, merge. Never spawn a cold subagent for this.
- **> 25 → in-repo folder handoff to Google Antigravity (Gemini fast)** — the proven paylog/afterpay flow, zero API cost:
  1. Batch files at `graphify-out/labeling/batch-<i>.json` (~60 communities per batch, entries `{cid, size, samples, sample_files}` — `prep-label-batches.py` produces the shape; point its `/tmp/graphify-label/` output into `graphify-out/labeling/`).
  2. Write `graphify-out/labeling/PROMPT.md` yourself (reference: paylog's committed copy): domain paragraph; output contract (*for each `batch-<i>.json` write sibling `results-<i>.json`, flat `"cid" → "label"` map, IDs as strings, no wrapper/fences*); label rules (2–4 words, kebab-case, English — translate PT-BR concepts, keep proper nouns; `helpers`/`common` banned; distinct); PT→EN glossary when the code is PT-BR; expected total as the completion check.
  3. HAND OFF: operator opens the folder in Antigravity; its agents write the `results-<i>.json` siblings in place. Announce:
     <!-- announce-template: "Lote de rotulagem pronto para o Antigravity na pasta do projeto {PROJECT}." -->
     `bravros ha say --force "Lote de rotulagem pronto para o Antigravity na pasta do projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true`
  4. Collate + patch: merge `results-*.json` → `community-labels.json`, patch `graph.json` (`collate-labels.py` logic pointed at the folder).
  5. **Dedup pass — expect collisions**: parallel batches can't see each other (paylog round 1: 116 colliding labels / 508 communities, 68 named `order-service`). Group `dedup-batch-<i>.json` **by collision** + `DEDUP-PROMPT.md`, hand off again, merge back.
  6. Commit `graphify-out/labeling/` as the audit trail.

🚨 **`collate-labels.py` is full-relabel-only** — it stamps `Community NN` over every community it did not see. Gap-fills always use `merge-missing-labels.py` (`/graphify-status` drives that loop). Legacy in-Claude Haiku labelling swarm remains available when there's no Antigravity/Gemini quota — confirm agent count first.

## Step 6.5 — Swarm-helper copies

```bash
cp <skill-dir>/references/finalize.sh <skill-dir>/references/orchestrator-prompt.md <skill-dir>/references/worker-prompt.md graphify-out/
```

Viz only on explicit request (stays gitignored): `suppress-hyperedges-viz.py .` then `graphify tree --graph graphify-out/graph.json --output graphify-out/GRAPH_TREE.html`.

## Step 7 — Query surface

Nothing to wire per project — the **user-scoped MCP server** picks the graph up for sessions started in the repo (`project_path` selects it from anywhere). Verify registration exists (`claude mcp list | grep graphify`); on a new machine: `claude mcp add --scope user graphify -- ~/.local/bin/graphify-mcp` — ONCE per machine, never per project. CLI backup: `graphify query "…" --graph graphify-out/graph.json`. The `pretooluse-graph-nudge.sh` PreToolUse hook nudges agents graph-first.

## Step 8 — Tracked refresh hooks + union-merge driver (default-on; `--no-hooks` / `--no-merge-driver` skip)

Detect the repo's hook convention first: if `.githooks/post-commit` is gitignored → `machine-local` (e.g. paylog: write hooks but do NOT commit them); else `tracked` (default — commit `.githooks/` so the refresh travels via git pull).

**8a — refresh hooks:** copy canonical files from the deployed `~/.bravros/scripts/`, write three delegators, set `core.hooksPath`:

```bash
mkdir -p scripts/graphify .githooks
cp ~/.bravros/scripts/graphify-refresh-hook.sh          scripts/graphify-refresh-hook.sh
cp ~/.bravros/scripts/graphify/apply-labels.py          scripts/graphify/apply-labels.py
cp ~/.bravros/scripts/graphify/strip-framework-verbs.py scripts/graphify/strip-framework-verbs.py
chmod +x scripts/graphify-refresh-hook.sh
for hk in post-merge post-commit post-checkout; do
    printf '#!/bin/sh\n# Tracked delegator → scripts/graphify-refresh-hook.sh (requires: git config core.hooksPath .githooks)\nexec "$(git rev-parse --show-toplevel)/scripts/graphify-refresh-hook.sh" %s "$@"\n' "$hk" > ".githooks/$hk"
    chmod +x ".githooks/$hk"
done
git config core.hooksPath .githooks    # per-machine config, NOT a committed file
```

Only ask the user when another tool already owns a `.githooks/post-*` slot.

**8b — union-merge driver** (parallel branches touch `graph.json` without textual conflicts; the post-merge hook owns the rebuild):

```bash
git config merge.graphify.name   "graphify union merge"
git config merge.graphify.driver "graphify merge-driver %O %A %B"
grep -q 'graphify-out/graph.json merge=graphify' .gitattributes 2>/dev/null || cat >> .gitattributes <<'EOF'

# graphify in-project graph: regenerable artifact — union-merge, no textual conflicts.
#   git config merge.graphify.name   "graphify union merge"
#   git config merge.graphify.driver "graphify merge-driver %O %A %B"
graphify-out/graph.json merge=graphify
graphify-out/graph.json linguist-generated
graphify-out/GRAPH_REPORT.md linguist-generated
EOF
```

Report: hook convention (and whether hooks get committed), configs written. Integration branch ≠ `homolog` → remind about `GRAPHIFY_AUTOCOMMIT_BRANCH=<branch>`. Suggest `tail /tmp/graphify-*.log` after the next pull to confirm the refresh fired.

## Step 9 — (optional) CLAUDE.md

Ask first — announce that a decision is pending:
<!-- announce-template: "Configuração do grafo concluída, aguardando sua decisão sobre atualizar o contexto. Projeto {PROJECT}." -->
`bravros ha say --force "Configuração do grafo concluída, aguardando sua decisão sobre atualizar o contexto. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." studio >/dev/null 2>&1 || true` If yes: root section from `references/claude-md-section.md` filled with the ACTUAL node/edge/community counts and god nodes (never placeholders); inject a one-liner pointer into every nested `CLAUDE.md` lacking "Knowledge graph available". On re-run, replace old-format sections (mentions of `.mcp.json` or `~/Sites/context`) with the in-project template.

## Step 10 — Verify + commit set

```bash
graphify query "smoke test" --graph graphify-out/graph.json | head -20   # non-empty
test "$(git config --get core.hooksPath)" = ".githooks" || echo "⚠️ hooksPath"
git config --get merge.graphify.driver >/dev/null || echo "⚠️ merge driver"
git check-ignore -q graphify-out/graph.json && echo "⚠️ graph.json gitignored — fix .gitignore"
```

**Commit:** `graphify-out/{graph.json,community-labels.json,GRAPH_REPORT.md}`; the hook machinery (`.githooks/post-{commit,checkout,merge}`, `scripts/graphify-refresh-hook.sh`, `scripts/graphify/*.py`); `.graphify`, `.graphifyignore`, `.gitattributes`, `.gitignore`; swarm helpers + `graphify-out/labeling/` if generated. Drop `.githooks/post-commit` from the set in a machine-local repo. On-demand LLM refresh later: `bash ~/.bravros/skills/graphify-this-project/scripts/extract-deepseek.sh .`

## Idempotency

Safe to re-run — every step checks state before writing (existing graph → refresh in place; existing `.graphifyignore` → ask/merge; gitignore marker → skip; pinned install → skip; existing `.graphify` → ensure keys; hooks/driver → re-copy only on drift; CLAUDE.md section → skip, old-format → re-sync). From-scratch reset: remove `graphify-out/` and re-invoke.
