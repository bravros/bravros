#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
reapply-labels-by-node.py — carry community_label forward onto a freshly AST-rebuilt
graph.json by matching nodes on their STABLE identity (id/label), NOT by community id.

Why: an AST rebuild (`graphify update`) re-clusters and renumbers communities, so the
cid->label sidecar (apply-labels.py) mis-maps after a rebuild. Matching per-node by its
stable key preserves the DeepSeek labels on every node whose identity is unchanged
(the common case for an incremental refresh). New/renamed nodes stay unlabeled until the
next DeepSeek pass. This is free (pure JSON, no LLM).

Usage: reapply-labels-by-node.py <old_labeled_graph.json> <new_rebuilt_graph.json>
  Snapshots node_key -> community_label from the OLD graph and writes it onto matching
  nodes in the NEW graph, in place. Idempotent; safe no-op if either file is missing.
"""
from __future__ import annotations
import json
import sys
from pathlib import Path


def node_key(n: dict):
    # Namespace id-space vs label-space so an old label-keyed node can't be
    # inherited by an unrelated new node whose id == that label. Nodes with no
    # stable identity (neither id nor label) return None and are skipped.
    nid = n.get("id")
    if nid is not None:
        return ("id", nid)
    lbl = n.get("label")
    return ("label", lbl) if lbl is not None else None


def main() -> int:
    if len(sys.argv) < 3:
        print("usage: reapply-labels-by-node.py <old_graph.json> <new_graph.json>", file=sys.stderr)
        return 2
    old_p, new_p = Path(sys.argv[1]), Path(sys.argv[2])
    if not old_p.exists() or not new_p.exists():
        print("  reapply-labels: old or new graph missing — skipping", file=sys.stderr)
        return 0

    old = json.load(open(old_p))
    new = json.load(open(new_p))

    # graphify renamed the node field `community_label` -> `community_name`; a graph
    # carries whichever key was current when it was built. Reading one key only would
    # snapshot nothing from a new-format graph and silently strip every label here.
    snap = {}
    for n in old.get("nodes", []):
        lbl = n.get("community_label") or n.get("community_name")
        k = node_key(n)
        if lbl and k is not None:
            snap[k] = lbl

    # The new graph is a fresh AST rebuild and carries neither key, so stamp both —
    # whichever one this graphify build reads back, it finds the label.
    applied = 0
    for n in new.get("nodes", []):
        k = node_key(n)
        if k is None:
            continue
        lbl = snap.get(k)
        if lbl:
            n["community_label"] = lbl
            n["community_name"] = lbl
            applied += 1

    new_p.write_text(json.dumps(new, ensure_ascii=False, separators=(",", ":")))
    print(f"  reapply-labels: carried {applied} labels forward (of {len(snap)} snapshot, {len(new.get('nodes', []))} nodes)", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
