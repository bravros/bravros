#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "graphifyy>=0.7.10",
# ]
# ///
"""suppress-hyperedges-viz.py — regenerate graph.html WITHOUT the hyperedge polygon overlay.

WHY THIS EXISTS:
  graphify's `to_html` renders each hyperedge as a semi-transparent polygon
  connecting all member nodes. With 100+ hyperedges across an 11K-node graph,
  the polygons stack into a giant blue/purple blob that obscures the actual
  nodes and edges (see CleanShot screenshot from paylog 2026-05-09 session).

  This script:
    1. Loads the canonical graph.json (or rebuilds from .graphify_extract.json)
    2. Clears `G.graph["hyperedges"]` BEFORE export — the polygons disappear,
       nodes/edges remain
    3. Calls `to_html` with community_labels for proper coloring
    4. Hyperedge metadata stays in graph.json (MCP `get_community` etc. still work)

USAGE:
  /Users/$USER/.local/share/uv/tools/graphifyy/bin/python3 \
    /Users/$USER/.claude/skills/graphify-this-project/scripts/suppress-hyperedges-viz.py [project_root]

  Default project_root: current working directory.

OUTPUTS:
  graphify-out/graph.html  — D3 force-directed viz, hyperedge polygons OFF
"""
import json
import os
import sys
from pathlib import Path


def main() -> int:
    from graphify.build import build_from_json
    from graphify.cluster import cluster
    from graphify.export import to_html

    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
    out = root / "graphify-out"

    # Prefer .graphify_extract.json (the full pre-cluster extraction), fall back
    # to graph.json if extract is missing.
    extract_path = out / ".graphify_extract.json"
    if extract_path.exists():
        extraction = json.loads(extract_path.read_text())
        print(f"  ✓ loaded extraction from {extract_path.name}")
    else:
        graph_data = json.loads((out / "graph.json").read_text())
        extraction = {
            "nodes": graph_data.get("nodes", []),
            "edges": graph_data.get("links", []),
            "hyperedges": graph_data.get("hyperedges", []),
        }
        print(f"  ✓ rebuilt extraction from graph.json (extract.json missing)")

    G = build_from_json(extraction)
    communities = cluster(G)
    print(f"  ✓ Graph: {G.number_of_nodes()} nodes / {G.number_of_edges()} edges / {len(communities)} communities")

    # Load community labels
    cl_path = out / "community-labels.json"
    int_labels = None
    if cl_path.exists():
        cl = json.loads(cl_path.read_text())
        int_labels = {int(k): v for k, v in cl.items()}
        print(f"  ✓ loaded {len(int_labels)} community labels")

    # CRITICAL FIX: clear hyperedges from G.graph metadata BEFORE export
    n_hyperedges = len(G.graph.get("hyperedges", []))
    if n_hyperedges:
        G.graph["hyperedges"] = []
        print(f"  ✓ suppressed {n_hyperedges} hyperedge polygons in viz")
        print(f"    (metadata preserved in graph.json — MCP queries still see them)")

    # Allow large graphs (default limit is 5000)
    os.environ["GRAPHIFY_VIZ_NODE_LIMIT"] = "20000"

    print("▸ Generating graph.html (hyperedge layer suppressed)...")
    to_html(G, communities, str(out / "graph.html"), community_labels=int_labels)
    size = (out / "graph.html").stat().st_size
    print(f"  ✓ graph.html ({size // 1024} KB)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
