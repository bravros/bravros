# skills — Authoring Guide

> 📊 **Knowledge graph available** — query the graph via `graphify query "<question>"` (CLI, from the repo root) before broad searches in this folder. See [`../CLAUDE.md`](../CLAUDE.md#graphify-knowledge-graph--query-this-before-grepping).

Every skill is one subdirectory here. This file is loaded only when Claude reads something under `skills/` — keep it focused on "if you are about to add or edit a skill, read this first."

## File anatomy

```
skills/<name>/
├── SKILL.md             # required — frontmatter + instructions
├── references/          # optional — long context loaded on demand
│   └── *.md
└── *                    # optional templates, fixtures, scripts
```

Skills run from the host runtime skills directory, NOT from this repo. After editing, deploy with:

```bash
bravros deploy          # fast, no health checks
# or
bash install.sh         # full install with checks
```

## SKILL.md frontmatter

```yaml
---
name: skill-name
description: One short clause that names what the skill does + the trigger. 25–40 words target, 50 hard ceiling. Strong verb first; one slash-command form; one natural-language variant if it adds signal; nothing else.
---
```

### Description discipline (hard rule)

Descriptions sit permanently in Claude Code's metadata layer. When that layer overflows the budget, Claude Code **silently drops** descriptions on startup — your skill stops triggering with no warning except a startup banner like *"63 skill descriptions dropped · /doctor for details"*.

Hard rules for every new skill:

- **Single physical line only.** `description:` must be a plain scalar — never a YAML block scalar (`description: >` or `description: |`). Multi-line folded forms break metadata parsing and inflate the budget unpredictably.
- **25–40 words target, 50 hard ceiling.** No exceptions. If you can't fit, the skill is doing too much — split it.
- **Lead with what the skill does** in one clause. Strong verb first.
- **Name the trigger.** Slash command form (e.g. `/foo`), key phrase (e.g. "make a podcast"), or both — pick the one that fires most reliably.
- **Drop everything else** into the body: proactive triggers, "also use when…" rambles, justifications, lists of synonyms, internal mechanics, multiple example phrases.
- **Preserve domain specificity.** `graphify-this-project` names graphify; `why-blocked-order` names the paylog pedido. Removing what makes a skill identifiable defeats the purpose.

Run `/skill-hygiene` (Pass A) to retrofit existing skills if the metadata layer is already over-budget. The skill lives at `.claude/skills/skill-hygiene/`.

### Skill names — avoid Claude Code reserved/bundled identifiers + built-in concepts

Two severities of conflict to watch for:

**Hard conflicts** — built-in slash commands and bundled skills (e.g. `/clear`, `/compact`, `/resume`, `/agents`, `/fork`, `/branch`, `/debug`, `/simplify`, `/loop`, `/init`, `/review`, `/security-review`, …) **shadow** any user skill of the same name. Typing `/<name>` invokes the built-in, not yours, with no warning. **Always rename.**

**Soft conflicts** — names that overlap with a built-in **concept** (subagent / permission mode / tool) but aren't in the reserved slash-command table. The reference example is `plan` — overlapping with the **Plan subagent**, **plan permission mode**, and the **Plan tool**. The slash invocation still routes to your skill, but users get confused: "does `/plan` invoke my skill or Plan mode?". Community convention (e.g., the Superpowers skill collection's `/write-plan`) is to use a disambiguating prefix: `/write-plan`, `/sdlc-plan`, `/plan-design`. **Rename when the blast radius is small; document the overlap when it's large.**

Before naming a new skill:

1. Read `.claude/skills/skill-hygiene/references/reserved-commands.json` for the canonical list. Check three sets: `slash_commands.*.names`, `bundled_skills.names`, and `built_in_concepts_to_avoid.names`.
2. If the candidate name appears in any of those sets, pick a disambiguated form (prefix `bravros-`, `sdlc-`, `repo-`, `write-`, or pull a domain word from the body).
3. Run `/skill-hygiene` (Pass B) to audit existing skills for collisions and propose renames.

When Anthropic ships new built-ins or new soft-conflict concepts surface, update `reserved-commands.json` (single source of truth), not this CLAUDE.md.

## Writing the body

- **Model requirement** — state upfront if the skill must run on Opus/Sonnet/Haiku and why.
- **Critical rules** — numbered, terse. Claude reads top-to-bottom.
- **Steps** — sequential headings (`## Step 1: …`). Steps build on each other.
- **Offload long context** to `references/<topic>.md` and Read on demand — keeps the skill itself small.
- **Background by default** — long-running commands background by default (`run_in_background`/`--bg`); grep before big reads.

## Orchestration: Workflow vs `/loop` vs single Agent

When a skill needs to do more than one thing at once, pick the right primitive — they are not
interchangeable. Full guide + authoring conventions in [`../docs/WORKFLOWS.md`](../docs/WORKFLOWS.md).

| Reach for… | When the work is… | Exemplar |
|---|---|---|
| **single `Subagent`** | one self-contained read/task — you want the conclusion, not file dumps | `scout` |
| **in-plan parallel subagents** | 2+ independent plan *phases*, each writes code | `/orchestrate` |
| **Workflow tool** ("ultracode") | N independent items, same analysis, collected as **structured data** (optionally adversarially verified) | `batch-merge-prs` (`verify-prs.js`), `triage-sweep`, `context` (`context-authors.js`) |
| **`/loop`** | the same step repeated over *time* until a condition holds (poll / iterate-until / drain-queue) | polling a long-running deploy or CI run |

Rules of thumb:
- If you are parsing free-form agent text to drive a decision, you want a **Workflow with a schema**, not raw `Subagent` calls.
- Workflow parallelizes across **space** (N items, now); `/loop` iterates across **time** (one thing, repeatedly). A skill can want both.
- A Workflow-based skill ships its script at `skills/<name>/scripts/<workflow>.js` and **materializes it into the project's `.claude/workflows/` at Step 0**. Copy `verify-prs.js`'s structure (pure-literal `meta`, `parallel()`, schema'd returns, `.filter(Boolean)`).
- Dispatch-prompt rules: root `CLAUDE.md` § Subagent & worker hygiene.

## Complexity markers (plans only)

Every plan **phase heading** carries one tier marker. This is a plan-authoring convention read
by `/orchestrate`'s dispatcher and assigned by `/plan`'s inline review — it is no longer
mechanically enforced by an audit hook
(the standalone audit engine was deleted in P-0187):

| Marker | Model | When |
|---|---|---|
| `[H]` | Haiku | CRUD, styling, config, migrations, trivial tests |
| `[S]` | Sonnet | Business logic, services, integrations, complex tests |
| `[O]` | Opus | Architecture, cross-system coordination (rare) |

**Marker IS the model.** An Agent call dispatching an `[S]` phase MUST set `model: "sonnet"`.
The tier lives on the phase heading — `### Phase N: Name [S]` — not on individual tasks.

## No progress echoes

Skills do not emit `🏁`/`🤖` echo checkpoints — pure ceremony at runtime, and they slowed flows
(retired 04/08/2026). Do not add them to new skills.

## Shared references

Shared skill content lives in `skills/shared/`. Skills reference it by relative path (`../shared/<file>.md`) and Read it on demand. A consumer may instead replace its own `references/*.md` with a symlink into `skills/shared/` — then edit only `skills/shared/<file>.md` and `bravros deploy` materializes a copy at install time via `cp -L`. No symlinked consumers remain as of P-0187 (the last two, `plan-review` and `plan-approved`, retired with the planning-chain collapse).

`skills/shared/` **is deployed** to `~/.config/bravros/skills/shared/` as a plain payload directory — not a skill (no `SKILL.md`, never in `.deploy-manifest.json`, unaffected by `skills.enabled`), and refreshed wholesale on every deploy. It was source-only until 2026-08-13, which was correct while every consumer symlinked into it, but P-0187 retired the last symlink and left the prose `../shared/*.md` links pointing at nothing in the runtime — so six core skills (`finish`, `promote`, `hotfix`, `batch-merge-prs`, `plan`, `auto-pr`) silently lost the shared merge gates at merge time. **A prose link into `../shared/` is a runtime dependency**: when you add one, the file must exist in the deployed runtime, and a safety-critical gate should also be restated in the consumer's own `references/` so it never depends on a cross-skill Read succeeding.

Note: `auto-pr/references/worktree-mode.md` covers the `--worktree` flag for `auto-pr`.

## Model Requirements in Skills

Use this section when authoring or auditing `model:` frontmatter and prose model references.

### (a) Frontmatter `model:` — use sparingly, never in hook skills

The `model:` frontmatter key tells the Skill tool to switch the active model before loading the skill. This has a hidden cost: if the parent session's accumulated context (system prompt + CLAUDE.md stack + memory + tool schemas) already exceeds the target model's standard window, the runtime attempts the 1M-context variant — which requires its own **extra-usage grant separate from the parent session's grant**. Result: `API Error: Extra usage is required for 1M context` on every invocation.

Rules:
- **Never** add `model:` frontmatter to skills that auto-fire via hooks (e.g., `SessionStart`, `PostToolUse`). These fire inside an already-large session and will reliably trip the 1M-context gate.
- **Only** use explicit `model:` when deterministic model selection is genuinely load-bearing (currently: none identified). Inheriting the parent session's model is almost always correct.
- If you do need to constrain a skill to a specific tier, document the reasoning inline.

### (b) Prose model references — prefer ungrounded tier names

In prose sections (e.g., "## Model Requirement"), use the tier name without a version suffix:

| Prefer | Avoid |
|--------|-------|
| **Sonnet** | ~~Sonnet 4.6~~ |
| **Opus** | ~~Opus 4.6~~, ~~Opus 4.7~~ |
| **Haiku** | ~~Haiku 3.5~~ |

Version-locked names decay when the next point release ships. Ungrounded tier names stay valid across releases.

### (c) Canonical model list — single source of truth

Do not hard-code current model identifiers in individual skill files. The canonical current models (e.g., `claude-sonnet-4-6`, `claude-opus-4-7`) are tracked in `~/.config/bravros/CLAUDE.md` under "Execution & Model Tiers". Reference that file rather than duplicating version strings here.

## Skill Targeting — retired (P-0187)

Skills are **host-neutral now** — Claude Code is the only supported host, the multi-harness
adapters (Codex/OpenCode/Pi) and the per-host skills compiler (`bravros skills build --host`)
were retired. Do not add a `targets:` frontmatter field to new skills, and don't leave it on
existing ones — there is no build step left that reads it.

Keep descriptions concise regardless: primary trigger + 2-3 natural-language variants,
target ≤300 chars.

## Out-of-Source Skills (opt-in via `skills.preserve`)

Some skills live outside the bravros source repo by design — they are not in `skills/` and are never deployed by `bravros deploy` or `bash install.sh`.

Users who want such skills to survive deploy and selfupdate cycles add them to `.bravros.yml`:

```yaml
skills:
  preserve: [my-custom-skill]
```

This adds the named directories to the `PreserveSkills` allowlist. The following operations will skip pruning any skill listed here:
- `bravros deploy`
- `bravros init` (copy-mode fallback)
- `bash install.sh --legacy` (legacy prune block)

**Graphify note (P-0133 Phase 5):** Graphify is no longer a shipped skill in the source `skills/` directory and won't be re-added. The toolkit no longer actively removes graphify config from user projects — the legacy scrub migrations were deactivated in P-0133. If you keep graphify locally, the toolkit will not fight you. Use the `preserve` allowlist above if you want graphify to survive deploy/selfupdate cycles.

---

## Per-project Skill Allowlist (opt-in via `skills.enabled`)

By default `bravros deploy` copies **all** skills from the source repo to `~/.config/bravros/skills/`. On project machines where homelab or domain-specific skills are unwanted, add an opt-in allowlist to `.bravros.yml`:

```yaml
skills:
  enabled: [plan, finish, auto-pr, batch-merge-prs, pr, pr-review]
```

When non-empty, only the listed skills AND any skill with `core: true` in its SKILL.md frontmatter are deployed. SDLC essentials (`/plan`, `/orchestrate`, `/finish`, `/auto-pr`, `/batch-merge-prs`, `/commit`, `/ship`, `/push`, `/start`, `/promote`, `/hotfix`, `/pr`, `/pr-review`, `/address-pr`) all carry `core: true` — they deploy regardless of the allowlist so SDLC workflows always work.

Use `--filter` to override per-invocation without editing config:

```bash
bravros deploy --filter plan,finish,orchestrate   # one-off filter; ignores skills.enabled
bravros deploy --dry-run                          # preview which skills would deploy
```

**Rule for skill authors:** If your skill is a generic SDLC primitive (not homelab/domain-specific), add `core: true` to its SKILL.md frontmatter. Personal homelab skills (`private-homelab`, etc.) must NOT be core.

### Example — Laravel project hiding homelab skills

```yaml
# .bravros.yml (in the Laravel project repo)
skills:
  enabled:
    - plan
    - finish
    - auto-pr
    # core skills (plan, finish, auto-pr, etc.) always deploy — no need to list them
    # homelab skills (private-homelab, etc.) are excluded
```

---

## Deprecation Policy

Skills marked with `deprecated_aliases_to: "<new-skill>"` in their frontmatter are **kept for 3 release cycles** after the unified replacement ships.

### Removed in v4.4 — see git history

The following deprecated alias stubs were removed in v4.4 (P-0128):

| Removed skill | Was forwarding to |
|---|---|
| `/plan-wt` | `/plan --worktree` |
| `/auto-pr-wt` | `/auto-pr --worktree` |
| `/complete` | `/finish` |

No active deprecations pending as of v4.4. Next cycle: v4.5 or later.

### Authoring a deprecation stub

Use this template for any future skill that is being replaced:

```yaml
---
name: <old-name>
description: "⚠️ Deprecated alias for `/<new-name> [--flag]`. Forwards to the new skill. Will be removed in v4.4. Triggers: <list old triggers>"
deprecated_aliases_to: "<new-name>"
mode: utility
---

# Deprecated: /<old> → /<new> [--flag]

When invoked, emit one-line warning to user:
> ⚠️ `/<old>` is deprecated and will be removed in v4.4. Forwarding to `/<new> --<flag>`.

Then call:
Skill({skill: "<new>", args: "<flag> $ARGUMENTS"})
```

Each stub is ~20 lines including frontmatter.

## Never

- Ship a skill without a `description` that lists user triggers
- Put long reference content inline when it could live in `references/` and be Read on demand
- Edit a consumer's `references/*.md` symlink target directly — always edit `skills/shared/<file>.md`
- Use `[H]/[S]/[O]` markers outside plan phase headings — they trigger audit Rule 19

## CLAUDE.md Managed Context Block

**`bravros detect-stack` / `bravros context refresh` were retired in P-0187** — there is no CLI
verb for this anymore; the model reads the manifest/lockfiles directly. The one surviving writer
of the managed block is the **`/context` skill** (Step 1: detect the stack from manifest + lock
files, then refresh the block in the root CLAUDE.md), bounded by:

```
<!-- BRAVROS:CONTEXT:STACK START -->
...
<!-- BRAVROS:CONTEXT:STACK END -->
```

The block contains:
- `## Tech Stack` — language, framework, test runner, package manager (detected from manifest/lockfiles, not a CLI verb)
- `## Context7 Library IDs (cached)` — library → Context7 ID mapping (injected only when IDs are available; workers query Context7 with these IDs)
- `## How to use these IDs` — one-paragraph guidance for workers (present only when IDs section is present)
- `Last refreshed: <ISO>` — timestamp

The block is **idempotent**: byte-equal content (ignoring the timestamp) is never re-written.
Worktrees each have their own CLAUDE.md — `/context` writes to `cwd/CLAUDE.md`. `/start` and
`/plan` no longer read or write this block (see their own SKILL.md files for current behavior).

## Audible Completion Announces

Skills that complete long-running operations fire a PT-BR audio announcement to Echo Studio
via `bash ~/.config/bravros/scripts/announce.sh "<message>" studio >/dev/null 2>&1 || true`.
`HASS_TOKEN` is exported from the macOS keychain in `~/.zshenv`, so the helper skips 1Password
entirely; it silently no-ops when the Mac is locked or HA is unreachable.
**Always redirect stdout** — the helper prints `Sent to studio: …`, which is noise in the transcript.

### ⚠️ Scope of the PT-BR rule — read this before writing any announce

The 100% Brazilian-Portuguese constraint governs **only the message string passed to
`announce.sh`**. It exists because Alexa's PT-BR voice mangles English, and it stops there.

It does **not** govern — these follow the operator-language rule in `~/.config/bravros/CLAUDE.md`:

| Always the operator's language | Always PT-BR |
|---|---|
| Your reply to the operator | The `announce.sh` message string |
| Code, identifiers, comments | App user-facing strings (UI copy, emails, PDFs) |
| Commit messages, PR titles and bodies | |
| Subagent and worker dispatch prompts | |

Reading a skill body full of Portuguese announce templates is **not** an instruction to
answer the operator in Portuguese. If the operator's language is unset, default to English.

The `Chefe, ` opener is prepended by `announce.sh` itself — **do not** hand-write it into
template strings or call sites; the script is idempotent and would otherwise be bypassed.
Override with `BRAVROS_ANNOUNCE_PREFIX` (empty string disables).

### Announce template format

Each skill that fires an announce carries an inline `<!-- announce-template: "..." -->` HTML
comment (documentation only — the template string is what to embed in the `announce.sh` call).
Allowed substitution variables:

| Variable | Source | Meaning |
|---|---|---|
| `{NUM}` | `basename "$PLAN_FILE" \| sed -E 's/-.*//' \| sed -E 's/^0+//'` | Plan number, leading zeros stripped |
| `{PR}` | `bravros pr number` (or `$PR_NUMBER` already in scope) | Pull-request number for PR-centric skills |
| `{TAG}` | `git describe --tags --abbrev=0` | Latest semver tag on current branch |
| `{N}` | rounds-executed counter maintained during round loop | Number of rounds executed |
| `{PROJECT}` | `basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")"` | Project name from meta |

**Rules:**
- 100% Brazilian Portuguese — no English words (see translation table in `~/.config/bravros/CLAUDE.md`)
- One sentence, ~20 words max
- Always placed AFTER text output (never replace on-screen recap with audio-only)
- Always non-blocking: append `2>/dev/null || true` if not using the `announce.sh` wrapper

### Skills with announce triggers

| Skill | Step | Template |
|---|---|---|
| `/finish` | Step 8/8 | `Plano {NUM} finalizado, mesclagem concluída.` |
| `/promote` | Step 5/5 | `Versão {TAG} publicada em produção.` |
| `/plan` | Step 5/5 | `Plano {NUM} criado e revisado, pronto para orquestração.` |
| `/orchestrate` | Done | `Plano {NUM} orquestrado, todas as fases concluídas.` |
| `/address-pr` | Step 8/8 | `Correções da revisão {PR} publicadas, próxima etapa pendente.` |
| `/auto-pr` | final step | `Fluxo automático finalizado. Revisão pronta no repositório.` |
| `/hotfix` | final step | `Correção urgente publicada em produção.` |

## Bash hygiene: portable array iteration

Claude Code's Bash tool executes commands via `/bin/zsh` on macOS (upstream Claude Code behavior). **In zsh, `for X in $VAR` does NOT word-split — the entire string becomes one iteration.** In bash, the same pattern word-splits by IFS. This footgun silently breaks any skill that builds a space-separated list from grep/awk output expecting iteration, because:

- Bash: `BACKLOG_IDS="B-0001 B-0002 B-0003"` + `for bid in $BACKLOG_IDS; do ... ; done` → **3 iterations** ✅
- Zsh: `BACKLOG_IDS="B-0001 B-0002 B-0003"` + `for bid in $BACKLOG_IDS; do ... ; done` → **1 iteration** (entire string becomes `$bid`) ❌

**Confirmed incidents:**
- PR #289 / PR #290 shipped security gates that silently did nothing because iteration hit the zsh word-split footgun.
- PR #300 caught a follow-up incident: `mapfile -t` (a bash-only builtin) was recommended as the portable fix and itself failed under the Bash tool's zsh on macOS. `mapfile` is not available in zsh unless the `zsh/mapfile` module is explicitly loaded — assume it is not.

The fix was not caught at write-time in either case.

### Portable recipe: use array assignment, not `mapfile`

**✅ Correct pattern (works in both bash and zsh):**

```bash
# Literal array init
ITEMS=(a b c)
for item in "${ITEMS[@]}"; do
  echo "Processing $item"
done
```

### Converting from grep/awk pipelines

When building a list from command output (e.g., `BACKLOG_IDS=$(... | grep ...)`), always convert to an array. Pick whichever form below fits — but **do not use `mapfile`**: it is a bash-only builtin and the Bash tool runs under zsh on macOS.

**Option A: `ARRAY=($(command))` (recommended — portable to bash and zsh)**

```bash
# ❌ Wrong (zsh word-split trap)
BACKLOG_IDS=$(grep -oE 'B-[0-9]+' "$FILE" | sort -u)
for bid in $BACKLOG_IDS; do
  process_item "$bid"
done

# ✅ Right (array form, portable to bash and zsh)
BACKLOG_IDS=($(grep -oE 'B-[0-9]+' "$FILE" | sort -u))
for bid in "${BACKLOG_IDS[@]}"; do
  process_item "$bid"
done
```

Caveat: unquoted `$(...)` inside `(...)` is subject to IFS splitting AND pathname expansion. Safe for tokens like `B-NNNN` (no glob metacharacters), but for free-form output that may contain `*`, `?`, `[`, or spaces inside fields, prefer Option B.

**Option B: while + IFS (portable, glob-safe)**

```bash
while IFS= read -r bid; do
  process_item "$bid"
done < <(grep -oE 'B-[0-9]+' "$FILE" | sort -u)
```

**Option C: awk in one pass (avoids subshell)**

```bash
awk '/^backlog:/{flag=1;next} /^[a-z_]+:/{flag=0} flag' "$FILE" \
  | grep -oE 'B-[0-9]+' \
  | sort -u | while read -r bid; do
  process_item "$bid"
done
```

**Anti-pattern: `mapfile -t` (bash-only — do not use here)**

```bash
# ❌ Fails silently under the Bash tool's zsh on macOS
mapfile -t BACKLOG_IDS < <(grep -oE 'B-[0-9]+' "$FILE" | sort -u)
```

Reserve `mapfile` for scripts you know will only ever run under `bash` (e.g. shebang-pinned `#!/usr/bin/env bash` with no Bash-tool execution).

### Counter-example: safe patterns (no change needed)

The following patterns are **safe** and do NOT need fixing:

- `for X in $(seq 1 5)` — command substitution with deterministic expansion
- `for X in "${ARRAY[@]}"` — explicit array expansion with quotes
- `for X in a b c` — literal words (not a variable)

**Rule:** Always use `"${ARRAY[@]}"` (with quotes and `[@]` subscript) when iterating bash arrays. Never use bare `$VAR` expansion in a for loop.

## See also

- `../docs/SKILLS.md` — human-facing catalog of all skills with one-line descriptions, grouped by category
- `skills/shared/dispatch.md` — two real dispatch primitives, worker prompt template, commit hygiene, worker codenames
- `skills/shared/pipeline.md` — canonical 7-stage pipeline
- `docs/AUDIT-RULES.md` — tombstone: where the old skill-invocation gates live now
- Root `CLAUDE.md` → "Shared Skill References" section and "Docs-sync Discipline"
