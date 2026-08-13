#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
apply-labels.py — re-apply community labels from community-labels.json onto graph.json nodes.

Used by the post-merge hook after `graphify update`/`extract` regenerates graph.json
(which strips custom fields). This script reads the cid→label mapping and writes
`community_label` onto every node based on its `community` field.

Caveat: assumes community IDs are stable across re-clustering. If Leiden re-runs
shifted cids meaningfully, labels will be slightly misaligned. The user should
re-run the full /graphify-this-project pipeline (which re-labels via LLM swarm)
periodically to refresh.

Idempotent: safe to re-run.

Reads:  graphify-out/graph.json
        graphify-out/community-labels.json (cid -> label)
Writes: graphify-out/graph.json (in place; adds community_label to each node)

Usage: python3 apply-labels.py [graph_path] [labels_path]
"""
from __future__ import annotations
import json
import sys
from pathlib import Path


def main() -> int:
    graph_path = Path(sys.argv[1] if len(sys.argv) > 1 else "graphify-out/graph.json")
    labels_path = Path(sys.argv[2] if len(sys.argv) > 2 else "graphify-out/community-labels.json")

    if not graph_path.exists():
        print(f"❌ {graph_path} not found", file=sys.stderr)
        return 1
    if not labels_path.exists():
        # Not an error — labels may not have been generated yet
        print(f"  no community-labels.json — skipping label application", file=sys.stderr)
        return 0

    with open(labels_path, encoding="utf-8") as f:
        int_labels = {int(k): v for k, v in json.load(f).items()}

    with open(graph_path, encoding="utf-8") as f:
        g = json.load(f)

    patched = 0
    fallback = 0
    for n in g.get("nodes", []):
        cid = n.get("community")
        if cid is None:
            continue
        # community may be serialized as an int OR a string ("5") depending on the
        # graphify version. Coerce to int so the lookup never silently misses and
        # falls back to a numeric label.
        try:
            cid_int = int(cid)
        except (ValueError, TypeError):
            cid_int = None
        label = int_labels.get(cid_int) if cid_int is not None else None
        if label is not None:
            n["community_label"] = label
            patched += 1
        else:
            n["community_label"] = f"Community {cid}"
            fallback += 1

    graph_path.write_text(json.dumps(g, ensure_ascii=False, separators=(",", ":")), encoding="utf-8")
    print(f"  applied {patched} community labels ({fallback} fallback to numeric)", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
