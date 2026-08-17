# Bravros SDLC — Global Rules

Machine-wide rules for the Bravros SDLC toolkit. Applies to **every** repo you work in.
Project-specific conventions (framework, folder layout, stack) live in that project's own
`CLAUDE.md`, not here. Personal preferences (language, tone, your own rules) go **outside**
the managed markers in `~/.claude/CLAUDE.md` — they are preserved across updates.

## Language scope — announcements vs everything else

Many skills fire an audio announcement via `announce.sh`, and those message strings are
written in Brazilian Portuguese because Alexa's PT-BR voice mangles English. **That
constraint covers the announcement string and nothing else.**

Encountering Portuguese inside a skill body is **not** an instruction to reply to the
operator in Portuguese. Your replies, code, identifiers, comments, commit messages, PR
titles and bodies, and subagent dispatch prompts all follow the **operator's own language
preference**, declared outside the managed markers in `~/.claude/CLAUDE.md`. When no
preference is declared, use the language the operator writes to you in.

Application user-facing strings (UI copy, emails, generated PDFs) are a separate question
owned by the project — see its own `CLAUDE.md`.

## SDLC workflow

Feature work flows: **`/plan` → `/orchestrate` → `/pr` → `/finish`** (`/plan` reviews inline
and writes a `.planning/P-NNNN-<slug>/` dossier; `/orchestrate` executes it).

- Branch model: `feature/*` → **`homolog`** (staging) → **`main`** (production, PR-gated only).
- Never push directly to `main`; promote `homolog → main` with **`/promote`** (needs an
  out-of-band token minted from a separate terminal — Claude cannot mint it).
- Accumulate multiple fixes on `homolog`, then **one** `/promote` for a bundled release —
  don't promote after every fix.
- Backlog before planning: capture ideas with **`/backlog add`**; promote to a plan when ready.
  In repos using the events model (see that repo's `.planning/CONVENTIONS.md` if present),
  read `.planning/backlog/` markdown + `.planning/events.jsonl` directly — the legacy `backlog`
  CLI verb is retired. Never hand-parse with ls/grep; read the files and fold state yourself.

## Commit format

Always commit via **`bravros commit "<emoji> <type>[(scope)]: <subject>" <files...>`** —
never raw `git add && git commit`. The commit-msg hook enforces the format.

Types: ✨ feat · 🐛 fix · 📚 docs · 💄 style · ♻️ refactor · ⚡ perf · 🧪 test · 🔧 build ·
🧹 chore · 📋 plan · 🔒 security · 🗃️ migration · 📦 deps · 🚀 deploy · 🤖 ci · 🔥 remove ·
🩹 hotfix · 🔀 merge · 🔍 debug · 🔙 revert · 🌐 i18n.

- Keep the subject ≤ 50 chars (soft limit 72); move detail to the body after a blank line.
- **No AI attribution.** Never add "🤖 Generated with Claude Code" or "Co-Authored-By: Claude"
  to commit messages **or** PR bodies. Strip it if a spawned agent adds it.

## Model tiers

Plan tasks carry a model marker enforced by audit Rule 19: **`[H]` = Haiku, `[S]` = Sonnet,
`[O]` = Opus**. The marker is the source of truth for which model runs a task — match it.

## Autonomous mode

Autonomous-mode safety gates must be unlocked by a **separate-terminal user action**
(the running Claude session cannot self-unlock). Once a reviewed plan is approved,
`/orchestrate` runs the whole round-loop continuously — no per-round prompts; ask the
operator only at completion, context degradation, or unrecoverable failure.

## Subagent & worker hygiene

Every dispatched worker/subagent prompt carries these rules verbatim:

- **Read before Edit, always.** Read the target file before the first Edit; re-Read before
  re-editing if any other step may have touched it. (Edit-without-Read is the #1 tool error.)
- **Structured output: echo the schema + one example.** Any schema-validated return must
  inline the exact JSON shape plus one minimal correct example; keep `required` minimal. On
  validation failure, feed the validator error verbatim into exactly one retry.
- **House rules ride every spawn prompt**, including the blocked-command → sanctioned-alternative
  table (e.g. `git branch --show-current` → the repo's canonical branch-lookup path). If a
  project-level guard blocks a command, stop and report — never work around it.
- **Worktree step 0:** run `pwd && echo "$(git branch --show-current)"` before anything else,
  use absolute paths thereafter, and on mismatch with the dispatch prompt stop and report.
- **Background by default; grep before big reads.** Long commands go to background — poll,
  don't block. Locate content with grep/rg, then read targeted line ranges.
- **Transcript mining: fixed-string streaming ONLY.** Mining `*.jsonl` session transcripts
  means `grep -F` fixed-string shortlists, or line-by-line python that SKIPS lines >2MB.
  Bounded-context regexes (`.{0,N}` around a pattern) over transcripts are BANNED — a single
  megabyte-scale line makes the regex engine balloon (32GB near-crash, 2026-07-30).

## Shell traps — the Bash tool runs zsh

All five of these fired in one live production merge (PR #1919, 2026-08-13), each one looking
correct on the page. They apply to every skill, hook, and dispatched worker.

- **A gate must never be piped.** `cmd | tail -5; echo "rc=$?"` reports **tail's** status, so a
  failing CI check or test run reads as `rc=0`. Redirect to a file, capture `RC=$?` on its own
  line, then inspect the file separately. (`${pipestatus[1]}` works in zsh but not bash — don't
  rely on it in shared recipes.)
- **`git <cmd> "$SHA:literal/path"` is a zsh parameter modifier.** `"$SHA:app/Foo.php"` expands
  the `:a` modifier (absolute path), eats the `a`, prepends cwd, and fails with
  `fatal: Not a valid object name /Users/…/<sha>pp/Foo.php`. Braces don't help. Any path whose
  first letter is `a c e h l p q r s t u x A P Q` is affected. **Put the path in a variable**
  (`"$SHA:$f"`) — after `:` the shell then sees `$`, never a modifier.
- **`git rev-parse` prints missing objects to stdout** before failing, so
  `$(git rev-parse "$SHA:$f" || echo ABSENT)` captures two lines and every comparison downstream
  is garbage. Always `git rev-parse --verify --quiet`. (Same trap ruined a `/promote` commit range
  via a missing ref — B-0338.)
- **`sleep N; <check>` is blocked by the Bash tool.** Wait with `until <check>; do sleep 5; done`,
  or background the command. Chaining shorter sleeps to dodge the block is also blocked.
- **`sed "s|^|$var |"` over a multi-line variable** dies with `unescaped newline inside
  substitute pattern`. Iterate `while IFS= read -r` and emit with `printf`.

Array iteration under zsh (`for x in $VAR` does not word-split) is a fourth trap — recipe in
this repo's `skills/CLAUDE.md` § Bash hygiene.

## graphify (knowledge-graph query)

A project is **graphify-enabled** if it has a `.graphify` file or `graphify-out/graph.json`.
In such a project, before any broad grep/glob sweep to answer "how does X work / what touches
Y / who calls Z", **query the graph first** — it resolves these in one call.
Fall back to grep only for exact string matches or runtime behavior static analysis can't see.

**Two interfaces — MCP first, CLI as backup.** The user-scoped `graphify` MCP server
(`graphify-mcp`; tools surface as `mcp__graphify__<tool>`) keeps the graph loaded across calls,
so repeated queries skip the multi-second `graph.json` load the CLI pays on every invocation.
Pick by use case:

| Use case | MCP tool (preferred) | CLI backup |
|---|---|---|
| "How does X work / what connects auth to the database?" | `query_graph {question}` — BFS default; `mode: "dfs"` traces a chain; `token_budget` caps output | `graphify query "what connects auth to the database?"` |
| "How does A reach B?" | `shortest_path {source, target}` — **fuzzy** keyword match; `undirected: true` if no directed path | `graphify path "UserService" "DatabasePool"` (CLI needs **exact** labels) |
| Explain one node in full (community, neighbors, `file:line`) | `get_node {label}` + `get_neighbors {label}` | `graphify explain "RateLimiter"` |
| Orientation: core abstractions, graph size | `god_nodes`, `graph_stats` | — |
| Everything in one community | `get_community {community_id}` | — |

The MCP server binds the graph of the directory the session started in; to query another
project's graph — or from a session without one — pass `project_path: "/abs/path/to/repo"` on
any tool call. The CLI stays canonical for hooks, scripts, and agents without MCP access.

The graph lives in-project at `graphify-out/graph.json` (committed, travels via `git pull`),
refreshed structurally by a post-merge hook. Re-run the semantic label pass on demand with
`graphify label . --missing-only --no-viz` (graphify ≥ 0.9 names communities natively;
`--missing-only` keeps existing labels and names only new/placeholder ones, so it never re-pays
for work already done). `--backend claude-cli` needs no API key.

**Always pass `--no-viz` on `label` / `cluster-only` runs.** We keep only the searchable
`graph.json` — `graph.html` and other viz artifacts are never wanted (the in-repo refresh hooks
already suppress them; the flag keeps manual runs consistent).

**Require graphify ≥ 0.9.28** — earlier releases reuse stale community labels across incremental
rebuilds. Currently installed: **0.9.38** via `uv tool install "graphifyy[mcp]"` (the `[mcp]`
extra ships the `graphify-mcp` server binary; plain `graphifyy` lacks it).

**Community labels are keyed by community id, and that is fragile.** Ids come from clustering,
so re-clustering renumbers them and labels land on unrelated clusters with no error. `cluster()`
is not stable across versions: 1399 vs 1448 communities for the same 1412-community graph
(0.8.1 vs 0.9.32). Never render the HTML by re-clustering — `regen-html-with-labels.py` reuses
the stored partition and logs `Reused stored partition: <n> communities`; if it says
*re-clustered*, the labels in that output are wrong. When a project shows many `Community NN`
names, coverage has decayed — fix it with `--missing-only`, not a full relabel.

## 1Password — pick the access mode BEFORE the first `op` call

Applies on machines with the 1Password CLI (`op`) configured; skip everywhere else.
Two access modes exist and they see **different vaults**. Choosing wrong looks like a bad
secret reference ("isn't in any vault", "item not found") and has burned 20+ tool calls of
retry-flailing on a single fetch. Decide by vault, then call **once**:

- **Service Account mode (default):** `OP_SERVICE_ACCOUNT_TOKEN` is set in the environment,
  so plain `op read "op://<Vault>/<Item>/<field>"` runs headless with no prompt — this is
  what scripts, hooks, and AFK/autonomous runs must use. An SA sees ONLY the vaults
  explicitly shared to it; it can **never** see the operator's personal vaults (`Private`).
  No amount of retrying plain `op` changes that.
- **Personal-account mode:** the `opme` wrapper (`OP_SERVICE_ACCOUNT_TOKEN= op --account
  "$OP_ACCOUNT" "$@"`) forces the operator's own account and triggers a **biometric prompt
  — that prompt is intended**, not an error. Use it only for items in personal vaults.
- **One `opme` call, then WAIT.** If the operator is AFK the prompt hangs or times out —
  that is a "blocked on operator" state: say so and pause. Never loop retries, and never
  fall back to hunting the same secret across SA-visible vaults.
- "isn't in any vault" almost always means **wrong access mode**, not a bad reference —
  check which vault the item lives in before touching the reference string.
- **Field names vary** by item category and account locale (`credential` vs `credencial`
  on pt-BR accounts; login items use `password`). On "field not found", run
  `op item get "<Item>" --vault <Vault>` once (SA mode requires `--vault`) and read the
  field names — concealed values print masked in this default output. Don't guess-iterate.

**A fetched secret must never land in the transcript.** Tool output is recorded; a secret
printed once is a secret leaked, however fast it scrolls by.

- **Never run bare `op read` / `opme read` as its own command** — the value becomes the tool
  result. Consume it inside the same shell command: `TOKEN=$(op read …) cmd` (env-var prefix;
  preferred — argv shows in `ps`, the prefix env doesn't), or hydrate whole environments with
  `op run --env-file` / `op inject` instead of exporting by hand.
- **Never `--reveal`, and never `op item get --format json`** — both print concealed field
  values. The masked default output above is the only safe item inspection.
- **Verify without seeing:** to prove a fetch works, check the exit code or
  `op read "op://…" | wc -c` — never print the value "just to confirm it looks right".
- **Files that hold hydrated secrets** (`.env`, `.zshenv`, `~/.aws/credentials`, CI env
  dumps) are never cat/grep'd raw — read targeted non-secret lines, or pipe through a
  redactor (`sed -E 's/[A-Za-z0-9_.=-]{12,}/<MASKED>/g'`) before anything reaches the
  transcript.

## Safety

- **Never discard uncommitted work directly.** Commands that would erase content git has never
  seen — `git checkout -- <paths>`, worktree-touching `git restore`, `git reset --hard`,
  `git clean -f`, `git stash drop/clear`, recursive `rm` inside the repo — must never be run
  directly when the target holds uncommitted or untracked content. Use the sanctioned path
  instead: **`bravros discard <paths>`** (tracked modifications) / **`bravros clean-untracked`**
  (untracked files) — both preserve into `.trash/` first, reversible for 30 days via
  `bravros trash restore <id>`. Truly permanent destruction needs a single-use token minted
  from a separate terminal: `bravros destructive unlock --reason "..."`. This is the rule
  regardless of whether a project has a hook enforcing it — preserve-before-delete is the
  standard, not the enforcement mechanism.
- Before any **other destructive operation** (submodule removal, `git rm`, force-overwrite),
  preserve the content somewhere first (copy to `/tmp` or `.trash/`).
- Before deleting or overwriting a file you didn't create, look at it first — if its content
  contradicts how it was described, surface that instead of proceeding.
