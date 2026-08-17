#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
collate-labels.py — merge labeling-swarm results, patch graph.json with community_label.

Reads:  /tmp/graphify-label/results-*.json (one per swarm worker)
        graphify-out/graph.json
Writes: graphify-out/community-labels.json
        graphify-out/graph.json (in place; adds community_label to each node)

Handles missing/malformed results gracefully — uses "Community NN" fallback
for communities the swarm didn't label.

Usage: python3 collate-labels.py [graph_path]
"""
from __future__ import annotations
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path


def main() -> int:
    graph_path = Path(sys.argv[1] if len(sys.argv) > 1 else "graphify-out/graph.json")
    results_dir = Path("/tmp/graphify-label")

    if not graph_path.exists():
        print(f"❌ {graph_path} not found", file=sys.stderr)
        return 1

    # Collate all results
    all_labels: dict[int, str] = {}
    result_files = sorted(results_dir.glob("results-*.json"))
    if not result_files:
        print(f"❌ no results-*.json in {results_dir} — swarm didn't run?", file=sys.stderr)
        return 1

    for p in result_files:
        try:
            with open(p) as f:
                batch_labels = json.load(f)
            for cid_str, label in batch_labels.items():
                try:
                    all_labels[int(cid_str)] = str(label)
                except (ValueError, TypeError):
                    print(f"  ⚠️  skipping malformed entry in {p.name}: {cid_str}={label!r}", file=sys.stderr)
            print(f"  ✓ {p.name}: {len(batch_labels)} labels", file=sys.stderr)
        except json.JSONDecodeError as e:
            print(f"  ⚠️  {p.name} malformed JSON: {e}", file=sys.stderr)

    # Quality stats
    counter = Counter(all_labels.values())
    duplicates = {label: count for label, count in counter.items() if count > 1}
    print(f"\nCollated: {len(all_labels)} labels, {len(counter)} distinct ({len(duplicates)} duplicates)", file=sys.stderr)
    if duplicates:
        top = sorted(duplicates.items(), key=lambda kv: -kv[1])[:5]
        print(f"  most-repeated: {dict(top)}", file=sys.stderr)

    # Save canonical labels file
    out_dir = graph_path.parent
    labels_path = out_dir / "community-labels.json"
    labels_path.write_text(json.dumps({str(k): v for k, v in sorted(all_labels.items())}, indent=2))
    print(f"\n✓ wrote {labels_path}", file=sys.stderr)

    # Patch graph.json — stamp the community name on every node. graphify renamed this
    # field `community_label` -> `community_name`, so write both: the graph then reads
    # correctly whichever key this build looks for.
    with open(graph_path) as f:
        g = json.load(f)

    patched = 0
    unmatched = 0
    for n in g.get("nodes", []):
        cid = n.get("community")
        if cid is None:
            continue
        if cid in all_labels:
            n["community_label"] = n["community_name"] = all_labels[cid]
            patched += 1
        else:
            # Fallback for any community the swarm didn't label
            n["community_label"] = n["community_name"] = f"Community {cid}"
            unmatched += 1

    graph_path.write_text(json.dumps(g, ensure_ascii=False, separators=(",", ":")))
    print(f"✓ patched graph.json: {patched} nodes labeled, {unmatched} fallback", file=sys.stderr)

    # Show samples of biggest labeled communities
    by_community = defaultdict(int)
    for n in g.get("nodes", []):
        if n.get("community") is not None:
            by_community[n["community"]] += 1
    biggest = sorted(by_community.items(), key=lambda kv: -kv[1])[:10]
    print("\nTop 10 communities → labels:", file=sys.stderr)
    for cid, size in biggest:
        label = all_labels.get(cid, f"Community {cid}")
        print(f"  C{cid:4d}  {label:35s}  ({size} nodes)", file=sys.stderr)

    return 0


if __name__ == "__main__":
    sys.exit(main())
