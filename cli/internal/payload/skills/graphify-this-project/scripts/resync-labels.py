#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
resync-labels.py — carry community labels across an AST rebuild WITHOUT trusting community ids.

Why this exists
---------------
`apply-labels.py` maps community-id -> label. Community ids come from clustering, and every
rebuild re-clusters: ids get renumbered, so the sidecar silently stamps labels onto unrelated
clusters. Measured on cartpy: an 8-node change moved 2.5% of communities, and rendering a
freshly-clustered partition with a cid-keyed sidecar agreed with the truth on 0.1% of nodes
while looking entirely plausible.

This script instead carries each label on the NODE that owned it (stable identity), then
re-derives the sidecar from the new partition by majority vote — so community-labels.json ends
up keyed to the ids that actually exist in the new graph.

Sequence, run right after the rebuild:
    1. node_key(old node) -> its community_label      (identity, survives renumbering)
    2. stamp those labels onto matching nodes in the new graph
    3. for each NEW community, majority-vote its members' carried labels -> that community's name
    4. rewrite community-labels.json from step 3, and stamp every node from it so a community
       is internally consistent (all members share one label)

Nodes with no match (genuinely new code) stay unlabelled; their community only gets a name if
enough of its other members carried one. Nothing is invented.

Idempotent. Safe no-op if either graph is missing.

Usage:
    resync-labels.py <old_graph.json> <new_graph.json> [labels.json]
      labels.json defaults to <new_graph dir>/community-labels.json
"""
from __future__ import annotations

import collections
import json
import sys
from pathlib import Path


def node_keys(n: dict) -> list:
    """Identity candidates for one node, most-specific first.

    Node ids are NOT stable across graphify versions: the 0.8.1 -> 0.9.32 dedup rewrite
    re-minted them, and id-only matching then recovered 2.3% of cartpy's nodes where
    source_file matching recovered 88.9%. So cascade — try the precise key, fall back to
    coarser ones — rather than betting everything on ids.

    Each key is namespaced so an id can never be inherited by an unrelated node whose label
    happens to equal that id.
    """
    keys = []
    nid, lbl, src = n.get("id"), n.get("label"), n.get("source_file")
    if nid is not None:
        keys.append(("id", nid))
    if src and lbl:
        keys.append(("sf+label", src, lbl))
    if lbl is not None:
        keys.append(("label", lbl))
    if src:
        keys.append(("sf", src))
    return keys


def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__, file=sys.stderr)
        return 2
    old_p, new_p = Path(sys.argv[1]), Path(sys.argv[2])
    labels_p = Path(sys.argv[3]) if len(sys.argv) > 3 else new_p.parent / "community-labels.json"

    if not new_p.exists():
        print("  resync: no new graph — skipping", file=sys.stderr)
        return 0
    if not old_p.exists():
        print("  resync: no pre-rebuild snapshot — skipping (labels left as-is)", file=sys.stderr)
        return 0

    try:
        old = json.loads(old_p.read_text(encoding="utf-8"))
        new = json.loads(new_p.read_text(encoding="utf-8"))
    except Exception as exc:
        print(f"  resync: unreadable graph ({exc}) — skipping", file=sys.stderr)
        return 0

    def real(lbl) -> bool:
        return bool(lbl) and not str(lbl).startswith("Community ")

    # 1. snapshot label under every identity candidate, most-specific first. Coarser keys
    #    (label, source_file) are only recorded when unambiguous — if two old nodes in
    #    different communities share a source_file, that key is poisoned and dropped, so a
    #    coarse match can never silently pick the wrong community.
    #    graphify renamed this field `community_label` -> `community_name`; a graph carries
    #    whichever key was current when it was built, so read both or snapshot nothing.
    carried, poisoned = {}, set()
    for n in old.get("nodes", []):
        lbl = n.get("community_label") or n.get("community_name")
        if not real(lbl):
            continue
        for k in node_keys(n):
            if k in poisoned:
                continue
            if k in carried and carried[k] != lbl:
                del carried[k]
                poisoned.add(k)
            else:
                carried[k] = lbl

    # 2+3. majority-vote each NEW community from the labels its members carried
    votes = collections.defaultdict(collections.Counter)
    matched = 0
    for n in new.get("nodes", []):
        cid = n.get("community")
        if cid is None:
            continue
        lbl = next((carried[k] for k in node_keys(n) if k in carried), None)
        if lbl:
            matched += 1
            votes[int(cid)][lbl] += 1

    resolved = {cid: c.most_common(1)[0][0] for cid, c in votes.items() if c}

    # SAFETY FLOOR — never destroy a good sidecar on a failed match.
    #
    # This script WRITES community-labels.json (apply-labels.py only ever read it), so a poor
    # identity match does not merely fail to help: it replaces hand-made labels with whatever
    # few survived. That is exactly what happened on cartpy 2026-08-04 — a graphify 0.8.1 →
    # 0.9.32 upgrade changed node ids (rewritten dedup), 221 of 9,500 nodes matched, and the
    # 1,412-label sidecar was overwritten with 56 and auto-pushed.
    #
    # When the match rate collapses the honest move is to change NOTHING and say so loudly:
    # the labels are still correct for the OLD graph, and a human needs to relabel against the
    # new one. Silently shrinking the sidecar destroys the only copy.
    have = len(json.loads(labels_p.read_text(encoding="utf-8"))) if labels_p.exists() else 0
    total_c = len({n["community"] for n in new.get("nodes", []) if n.get("community") is not None})
    with_c = sum(1 for n in new.get("nodes", []) if n.get("community") is not None)
    match_rate = (matched / with_c) if with_c else 0.0

    # Thresholds are deliberately strict. A dry run against paylog (26k-node committed graph vs
    # a 0.9.32 rebuild) matched 52% of nodes and resolved 1,710 of 2,441 communities — it would
    # have sailed past a 50% floor and silently dropped 731 hand-made labels. Losing 30% of the
    # sidecar without a word is not an acceptable default: below these bars the mapping is
    # genuinely stale and a human should relabel, not have the loss auto-committed.
    if have and (match_rate < 0.8 or len(resolved) < have * 0.9):
        print(
            f"  resync: ABORTED — only {matched:,}/{with_c:,} nodes ({match_rate:.0%}) matched the "
            f"pre-rebuild graph, which would cut community-labels.json from {have:,} to "
            f"{len(resolved):,} entries.\n"
            f"  resync: leaving graph.json and community-labels.json UNTOUCHED. The existing "
            f"labels describe the previous graph and are still the only copy.\n"
            f"  resync: this usually means node identity changed (a graphify upgrade altering "
            f"extraction/dedup). Relabel against the new graph, then commit.",
            file=sys.stderr,
        )
        # Exit 3, NOT 0 — this is the load-bearing signal to the caller.
        #
        # Returning 0 here made the abort self-defeating: it protects community-labels.json,
        # but by this point the caller's AST rebuild has ALREADY stripped community_label from
        # graph.json on disk. A silent success let the refresh hook carry straight on and
        # auto-commit that stripped graph — so the sidecar survived while the graph it
        # describes was published label-less, which is exactly the "we lose all our labels on
        # every merge" symptom (paylog, 2026-08-04: 37,732/41,133 nodes unlabelled across six
        # consecutive auto-refresh commits).
        #
        # Callers MUST treat 3 as "nothing was written, and the graph on disk is stripped —
        # restore it from HEAD and do not commit."
        return 3

    # 4. stamp every node from the resolved map so each community is internally consistent.
    #    Both key spellings are written (and both cleared), so the graph reads correctly
    #    whichever one this graphify build looks for — and a stale name under the other
    #    key can never outlive the clear.
    stamped = 0
    for n in new.get("nodes", []):
        cid = n.get("community")
        if cid is None:
            continue
        lbl = resolved.get(int(cid))
        if lbl:
            n["community_label"] = lbl
            n["community_name"] = lbl
            stamped += 1
        else:
            n.pop("community_label", None)
            n.pop("community_name", None)

    new_p.write_text(json.dumps(new, ensure_ascii=False, separators=(",", ":")), encoding="utf-8")
    labels_p.write_text(
        json.dumps({str(k): v for k, v in sorted(resolved.items())}, ensure_ascii=False, indent=0),
        encoding="utf-8",
    )

    total_c = len({n["community"] for n in new.get("nodes", []) if n.get("community") is not None})
    unnamed = total_c - len(resolved)
    print(
        f"  resync: carried {matched:,} node labels by identity → "
        f"{len(resolved):,}/{total_c:,} communities named, {stamped:,} nodes stamped"
        + (f", {unnamed:,} communities need labelling" if unnamed else ""),
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
