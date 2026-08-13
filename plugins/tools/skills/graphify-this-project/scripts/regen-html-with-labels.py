#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "graphifyy>=0.7.10",
# ]
# ///
"""
regen-html-with-labels.py — render graphify HTML viz with community labels embedded.

Reads:  graphify-out/graph.json
        graphify-out/community-labels.json
Writes: graphify-out/graph.html (overwrites; D3 force-directed, label-aware)

Uses graphify's own Python API (graphify.export.to_html) rather than the
graphify CLI's `cluster-only` command, because the CLI doesn't accept a
community_labels argument and would render with bare numeric "Community N" names.

IMPORTANT: this script must run with graphify's Python interpreter, NOT system
python — it imports `networkx` and `graphify` which only exist in the
graphifyy uv venv.

Usage:
    /Users/<user>/.local/share/uv/tools/graphifyy/bin/python regen-html-with-labels.py [graph_path]
    # or via shebang substitution if .graphify_python file exists:
    "$(cat graphify-out/.graphify_python)" regen-html-with-labels.py
"""
from __future__ import annotations
import json
import sys
from pathlib import Path

try:
    from graphify.build import build_from_json
    from graphify.cluster import cluster
    from graphify.export import to_html
except ImportError:
    print("❌ this script must run inside graphifyy's uv venv", file=sys.stderr)
    print("   try: $HOME/.local/share/uv/tools/graphifyy/bin/python regen-html-with-labels.py", file=sys.stderr)
    sys.exit(1)


def main() -> int:
    graph_path = Path(sys.argv[1] if len(sys.argv) > 1 else "graphify-out/graph.json")
    out_dir = graph_path.parent
    labels_path = out_dir / "community-labels.json"

    if not graph_path.exists():
        print(f"❌ {graph_path} not found", file=sys.stderr)
        return 1
    if not labels_path.exists():
        print(f"❌ {labels_path} not found — run collate-labels.py first", file=sys.stderr)
        return 1

    with open(graph_path) as f:
        graph = json.load(f)
    with open(labels_path) as f:
        int_labels = {int(k): v for k, v in json.load(f).items()}

    print(f"Loaded: {len(graph.get('nodes', []))} nodes, {len(int_labels)} labels", file=sys.stderr)

    G = build_from_json(graph)

    # Reuse the partition already stored in graph.json rather than re-deriving one.
    # cluster() is not stable across runs or graphify versions: for the same 1412-community
    # graph it returned 1399 communities under 0.8.1 and 1448 under 0.9.32. community-labels
    # .json is keyed by the community ids from the ORIGINAL clustering, so labelling a
    # freshly-computed partition scatters every label onto an unrelated cluster — measured on
    # cartpy, only 0.1% of nodes kept the correct label. Cluster from scratch only when the
    # graph carries no community assignment at all.
    communities: dict[int, list[str]] = {}
    for n in graph.get("nodes", []):
        cid = n.get("community")
        if cid is None:
            continue
        try:
            communities.setdefault(int(cid), []).append(n["id"])
        except (ValueError, TypeError):
            continue

    if communities:
        communities = {k: sorted(v) for k, v in sorted(communities.items())}
        print(f"Reused stored partition: {len(communities)} communities", file=sys.stderr)
    else:
        communities = cluster(G)
        print(f"No stored partition — clustered fresh: {len(communities)} communities", file=sys.stderr)

    out_html = out_dir / "graph.html"
    to_html(G, communities, str(out_html), community_labels=int_labels)
    print(f"✓ wrote {out_html}", file=sys.stderr)

    # Sanity-check that labels actually embedded
    html_text = out_html.read_text()
    sample_labels = list(int_labels.values())[:5]
    found = sum(1 for lbl in sample_labels if lbl in html_text)
    print(f"  verified {found}/{len(sample_labels)} sampled labels present in HTML", file=sys.stderr)

    return 0


if __name__ == "__main__":
    sys.exit(main())
