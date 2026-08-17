#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
prep-label-batches.py — partition graph communities into N batches for the labeling swarm.

Reads:  graphify-out/graph.json
Writes: /tmp/graphify-label/batch-<i>.json  (i = 0 .. N-1)
Returns N on stdout (skill captures it for the dispatch fanout).

Auto-scales N: 1 agent per ~75 communities, capped at 20, floor 3.

Usage:
    python3 prep-label-batches.py [graph_path]
    # default graph_path: ./graphify-out/graph.json
"""
from __future__ import annotations
import json
import random
import sys
from collections import defaultdict
from pathlib import Path


COMMUNITIES_PER_BATCH = 75
N_MIN = 3
N_MAX = 20
SAMPLE_PER_COMMUNITY = 12
SEED = 42


def main() -> int:
    graph_path = Path(sys.argv[1] if len(sys.argv) > 1 else "graphify-out/graph.json")
    if not graph_path.exists():
        print(f"❌ {graph_path} not found", file=sys.stderr)
        return 1

    random.seed(SEED)

    with open(graph_path) as f:
        g = json.load(f)
    nodes = g.get("nodes", [])

    # Group nodes by community
    by_community: dict[int, list[dict]] = defaultdict(list)
    for n in nodes:
        cid = n.get("community")
        if cid is None:
            continue
        label = n.get("label", "") or n.get("norm_label", "")
        src = (n.get("source_file") or "").split("/")[-1] if n.get("source_file") else ""
        if label:
            by_community[cid].append({"label": label[:60], "src": src})

    if not by_community:
        print("❌ no communities found in graph", file=sys.stderr)
        return 1

    # Sample max-N nodes per community
    samples: dict[int, list[dict]] = {}
    for cid, items in by_community.items():
        if len(items) <= SAMPLE_PER_COMMUNITY:
            samples[cid] = items
        else:
            samples[cid] = random.sample(items, SAMPLE_PER_COMMUNITY)

    # Auto-scale batch count
    n_communities = len(samples)
    n_batches = max(N_MIN, min(N_MAX, n_communities // COMMUNITIES_PER_BATCH or N_MIN))

    # Sort communities by size descending so big ones are evenly distributed via round-robin
    sorted_cids = sorted(samples.keys(), key=lambda c: -len(by_community[c]))

    batches: list[list[int]] = [[] for _ in range(n_batches)]
    for i, cid in enumerate(sorted_cids):
        batches[i % n_batches].append(cid)

    # Write batch files
    out_dir = Path("/tmp/graphify-label")
    out_dir.mkdir(exist_ok=True)
    # Wipe stale results from prior runs
    for stale in out_dir.glob("results-*.json"):
        stale.unlink()
    for stale in out_dir.glob("batch-*.json"):
        stale.unlink()

    for i, cids in enumerate(batches):
        batch_data = {
            "batch_id": i,
            "communities": [
                {
                    "cid": int(cid),
                    "size": len(by_community[cid]),
                    "samples": [s["label"] for s in samples[cid]],
                    "sample_files": sorted({s["src"] for s in samples[cid] if s["src"]})[:5],
                }
                for cid in cids
            ],
        }
        out_path = out_dir / f"batch-{i}.json"
        out_path.write_text(json.dumps(batch_data, indent=2))

    # Stats to stderr (skill reads only stdout for N)
    print(f"prep: {n_communities} communities → {n_batches} batches", file=sys.stderr)
    print(f"      largest 5 sizes: {sorted([len(by_community[c]) for c in by_community], reverse=True)[:5]}", file=sys.stderr)
    print(f"      median size: {sorted([len(v) for v in by_community.values()])[len(by_community) // 2]}", file=sys.stderr)

    # N on stdout for the skill to capture
    print(n_batches)
    return 0


if __name__ == "__main__":
    sys.exit(main())
