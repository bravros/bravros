# /graphify-status

One table, every graphify project on the machine, answering a single question: **does `graphify query` return real names, or is it still handing back `Community 417`?**

## Run it

```bash
uv run ~/.bravros/skills/graphify-status/scripts/graphify-status.py
```

Print the table verbatim. Do not re-measure by hand or re-format it — the script is the source of truth and its numbers are computed from each `graph.json`.

Options:

| | |
|---|---|
| `--json` | machine-readable rows instead of the table |
| `--depth N` | walk depth per root (default 3) |
| `--no-prompt` | skip auto-generating labelling prompts for degraded projects |
| `<root> ...` | scan specific roots instead of `~/Sites` |

## Reading the output

```
┌──────────┬─────────────┬────────┬────────────────────────────┐
│ project  │ communities │ labels │ nodes showing Community NN │
├──────────┼─────────────┼────────┼────────────────────────────┤
│ paylog   │ 2,441       │ 2,441  │ 0%                         │
├──────────┼─────────────┼────────┼────────────────────────────┤
│ payloglp │ 76          │ 29     │ 283 / 533 — 53%            │
└──────────┴─────────────┴────────┴────────────────────────────┘
```

- **communities** — clusters in `graph.json` that need a name.
- **labels** — entries in `community-labels.json`. **This should equal `communities`.** The gap is the defect.
- **nodes showing Community NN** — the number that matters. **Lower is better; 0% means every community is named.** A node counts as uncovered if it has no `community_label` *or* carries the `Community NN` placeholder — a never-labelled graph must not score 0%.

Rows are sorted healthiest first; unbuilt or unreadable graphs sink to the bottom.

## What it does NOT tell you

**Coverage is not correctness.** A community can be fully labelled and confidently wrong — the label survives while the code inside it is refactored away. That reads as 0% here and is worse than a placeholder, because it looks trustworthy. Only a fresh relabel fixes it; `--missing-only`-style passes never revisit an existing name.

Nor does it detect **duplicates**. Parallel labelling agents can't see each other, so cross-batch collisions are routine — one run produced 68 communities all named `order-service`. That is 0% here and near-useless in practice. If a graph was labelled by a fan-out, check uniqueness separately.

## When coverage is degraded

Labels are keyed by **community id**, and ids come from clustering. Any rebuild that re-clusters renumbers them, so labels silently land on the wrong clusters and new communities arrive unnamed. Coverage therefore decays on its own — re-check after big refactors, not just after a graph is first built.

**A degraded row auto-generates its own fix.** The run writes a paste-ready prompt per degraded project to `/tmp/graphify-label/<project>-relabel-prompt-N.md`, then prints a `MODE:` verdict telling you which of two paths to take. Nothing in the repo is touched at this stage.

The prompt is **self-contained**: sample node labels are inlined, so whoever names the clusters never needs to open the 10MB `graph.json`. It also carries the naming rules — kebab-case, English names over Portuguese code, and an explicit uniqueness demand, because a past fan-out produced 68 communities all named `order-service`.

### `MODE: inline` — ≤ 25 communities: just do it yourself

Do not hand the operator a chore this small. Read the prompt file, name the communities directly, write the JSON to the path the prompt names, run the merge, and report what you named. The sample labels are right there in the prompt — for a gap this size that is a few seconds of work and no meaningful token cost.

Follow the prompt's own rules; they are not decoration. The uniqueness one especially: check your names against `community-labels.json` before merging, since a duplicate is as useless as a number. If a community's samples are genuinely too thin to name honestly — one file, no context — say so and leave it rather than inventing something plausible.

Do **not** spawn a subagent for this. A cold agent re-reads everything you already have.

### `MODE: external` — > 25 communities: hand it off

Relay the prompt paths and let the operator paste them into **Antigravity** (or Gemini, or any agent with spare quota). At this size the token spend is real and there is no reason for it to land on Claude. Then merge their reply with the command below.

Tune the cutoff with `--inline-threshold N` on `emit-relabel-prompt.py` if a project's communities are unusually large or thin.

### Round trip

```bash
# 1. detect + emit (automatic on any /graphify-status run)
uv run ~/.bravros/skills/graphify-status/scripts/graphify-status.py

# 2. paste the prompt into Antigravity; save its JSON reply to the path the prompt names

# 3. merge back — additive, never destructive
uv run ~/.bravros/skills/graphify-status/scripts/merge-missing-labels.py \
    <project> /tmp/graphify-label/<project>-labels-*.json [--dry-run] [--force]
```

`--dry-run` shows exactly what would change. Commit both `community-labels.json` and `graph.json` — the graph is tracked, so labels travel via `git pull`.

### 🚨 Never use `collate-labels.py` for a partial relabel

`graphify-this-project/scripts/collate-labels.py` rebuilds `community-labels.json` from **only the current run's results** and stamps `Community NN` over every community it did not see. That is correct for a full relabel and catastrophic for a gap-fill: pointed at paylog's 2-community gap it would have destroyed 4,272 good labels and reset the graph to ~100% placeholders.

`merge-missing-labels.py` exists for this reason. It never overwrites an existing entry (`--force`, which reports every replacement, is the opt-in), ignores community ids not in the graph, rejects non-kebab-case names, strips the markdown fence agents habitually add, and warns when a new name collides with one already in use.

### Can this be fully automated?

Yes — with **graphifyy ≥ 0.9** installed (the machine-wide pin, `[mcp]` extra), `graphify label . --missing-only --no-viz --backend claude-cli` is the one-command gap-fill (**always `--no-viz`** — we keep only the searchable `graph.json`, no HTML). The prompt round-trip above remains useful when you'd rather spend someone else's tokens (Antigravity / Gemini) on a large gap.

But `--missing-only` is *only* a gap-fill. It never revisits an existing name, so it cannot repair labels that drifted onto the wrong clusters after a re-cluster. That still needs a full relabel.

### Spot-checking a label via MCP

To sanity-check what a named community actually contains, one MCP call beats opening `graph.json`: `mcp__graphify__get_community {community_id, project_path: "<abs repo path>"}` lists its nodes; `mcp__graphify__graph_stats` gives the per-project totals this table is computed from.
