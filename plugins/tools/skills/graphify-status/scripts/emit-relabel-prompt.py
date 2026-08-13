#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
emit-relabel-prompt.py — turn a degraded graph into a paste-ready labelling prompt.

`/graphify-status` reports that a project has unnamed communities. This writes the prompt
that fixes it: self-contained, with sample node labels inlined, so it can be pasted into an
external agent (Antigravity, Gemini, a web chat) without that agent needing to open
graph.json. Results come back through merge-missing-labels.py.

WHY NOT collate-labels.py: that script rebuilds community-labels.json from ONLY the current
run's results and stamps "Community NN" on every community it did not see. Point it at a
2-community gap in a 4,274-community graph and it destroys 4,272 good labels. This pipeline
is additive and never rewrites an existing entry.

Usage:
    emit-relabel-prompt.py <project-path> [--out DIR] [--batch-size N] [--stdout]
"""
from __future__ import annotations

import json
import random
import sys
from pathlib import Path

SAMPLE_PER_COMMUNITY = 12   # node labels shown per community — enough to infer a name
SAMPLE_FILES = 5
BATCH_SIZE = 60             # communities per prompt file; keeps a single prompt paste-able
STYLE_SAMPLE = 12           # existing labels inlined so new names match house style
SEED = 42

# Below this many unnamed communities, the calling Claude session should just name them
# itself — the samples are already in its context and the round-trip through an external
# agent costs more attention than the work. Above it, the token spend is real and belongs
# somewhere with spare quota.
INLINE_MAX = 25


def uncovered(graph: dict, labels: dict) -> dict[int, list[dict]]:
    """Communities whose nodes render as a bare number.

    Uncovered = no community_label, the "Community NN" placeholder, or absent from
    community-labels.json. Same three-way test graphify-status.py uses, so the two
    always agree on the count.
    """
    by_cid: dict[int, list[dict]] = {}
    for n in graph.get("nodes", []):
        cid = n.get("community")
        if cid is None:
            continue
        cl = n.get("community_label")
        if cl and not str(cl).startswith("Community ") and str(cid) in labels:
            continue
        label = n.get("label") or n.get("norm_label") or ""
        src = n.get("source_file") or ""
        by_cid.setdefault(cid, []).append(
            {"label": str(label)[:80], "src": src.split("/")[-1] if src else ""}
        )
    return by_cid


def build_prompt(project: Path, chunk: list[dict], style: list[str],
                 results_path: Path, part: int, total: int) -> str:
    part_note = f" (part {part} of {total})" if total > 1 else ""
    payload = json.dumps({"communities": chunk}, indent=2, ensure_ascii=False)
    style_list = "\n".join(f"  {s}" for s in style)
    example = ", ".join(f'"{c["cid"]}": "…"' for c in chunk[:2])

    return f"""\
# Name {len(chunk)} unlabelled graph communities{part_note}

Repo: `{project}`

A knowledge graph clusters this codebase into communities. The ones below have no name, so
queries against them return `Community 4171` instead of something meaningful. Give each one a
short name describing **what that cluster of code does**.

## Rules

- **lowercase-kebab-case**, 2–4 words. No numbers, no `Community` prefix.
- Name the **responsibility**, not the file type: `municipality-reference-data`, not `json-files`.
- The code is largely Brazilian Portuguese; **the names must be English** — that is what lets
  the graph be queried in English. Keep domain nouns that are literally the database schema
  (`pedido`, `pacote`, `romaneio`) only where no clean English equivalent exists.
- Every name must be **distinct from the others in this batch**. Reusing one name across
  communities is the main failure mode here — a past fan-out produced 68 communities all
  called `order-service`, which is technically labelled and practically useless.
- If a community looks incoherent, name it after its dominant member rather than inventing a
  vague umbrella like `misc-utils`.

## House style — existing names in this graph

{style_list}

## Communities to name

```json
{payload}
```

`samples` are node labels drawn from the community, `sample_files` the files they came from,
`size` the total node count.

## Output

Return **only** a JSON object mapping community id (as a string) to its name:

```json
{{{example}}}
```

Write it to:

```
{results_path}
```

If you cannot write files, output the JSON block and it will be saved by hand. No commentary,
and no community id that is not listed above.
"""


def main() -> int:
    args = sys.argv[1:]
    if not args or args[0].startswith("--"):
        print(__doc__.strip(), file=sys.stderr)
        return 2

    project = Path(args[0]).expanduser().resolve()
    to_stdout = "--stdout" in args
    out_dir = Path("/tmp/graphify-label")
    batch_size = BATCH_SIZE
    inline_max = INLINE_MAX
    if "--out" in args:
        out_dir = Path(args[args.index("--out") + 1]).expanduser()
    if "--batch-size" in args:
        batch_size = int(args[args.index("--batch-size") + 1])
    if "--inline-threshold" in args:
        inline_max = int(args[args.index("--inline-threshold") + 1])

    graph_path = project / "graphify-out" / "graph.json"
    labels_path = project / "graphify-out" / "community-labels.json"
    if not graph_path.exists():
        print(f"no graph at {graph_path}", file=sys.stderr)
        return 1

    graph = json.loads(graph_path.read_text(encoding="utf-8"))
    labels: dict = {}
    if labels_path.exists():
        try:
            labels = json.loads(labels_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            print(f"warning: {labels_path} unreadable — treating every community as unnamed",
                  file=sys.stderr)

    gaps = uncovered(graph, labels)
    if not gaps:
        print(f"{project.name}: fully labelled — nothing to do")
        return 0

    random.seed(SEED)
    by_size = {cid: len(items) for cid, items in gaps.items()}

    entries = []
    for cid in sorted(gaps, key=lambda c: -by_size[c]):
        items = gaps[cid]
        picked = (items if len(items) <= SAMPLE_PER_COMMUNITY
                  else random.sample(items, SAMPLE_PER_COMMUNITY))
        entries.append({
            "cid": int(cid),
            "size": by_size[cid],
            "samples": [p["label"] for p in picked if p["label"]],
            "sample_files": sorted({p["src"] for p in picked if p["src"]})[:SAMPLE_FILES],
        })

    # Show real existing names so the external agent matches house style instead of
    # inventing its own convention.
    existing = sorted(set(labels.values()))
    style = (random.sample(existing, min(STYLE_SAMPLE, len(existing))) if existing
             else ["(no existing labels — this graph has never been labelled)"])

    out_dir.mkdir(parents=True, exist_ok=True)
    chunks = [entries[i:i + batch_size] for i in range(0, len(entries), batch_size)]
    written: list[Path] = []
    for i, chunk in enumerate(chunks, start=1):
        results_path = out_dir / f"{project.name}-labels-{i}.json"
        prompt = build_prompt(project, chunk, style, results_path, i, len(chunks))
        if to_stdout:
            print(prompt)
            continue
        p = out_dir / f"{project.name}-relabel-prompt-{i}.md"
        p.write_text(prompt, encoding="utf-8")
        written.append(p)

    if to_stdout:
        return 0

    total_nodes = sum(by_size.values())
    inline = len(entries) <= inline_max
    print(f"{project.name}: {len(entries)} unnamed communities ({total_nodes} nodes)"
          f" → {len(written)} prompt file(s)")
    for p in written:
        print(f"  {p}")
    print()
    merge_cmd = (f"uv run {Path(__file__).parent / 'merge-missing-labels.py'} {project} "
                 f"{out_dir}/{project.name}-labels-*.json")
    if inline:
        print(f"MODE: inline ({len(entries)} ≤ {inline_max}) — small enough to name directly.")
        print("Read the prompt, write the JSON reply to the path it names, then:")
    else:
        print(f"MODE: external ({len(entries)} > {inline_max}) — hand this off.")
        print("Paste into Antigravity (or any agent with spare quota), save its JSON reply, then:")
    print(f"  {merge_cmd}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
