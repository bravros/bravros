#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
strip-framework-verbs.py — remove Livewire/Alpine framework-verb nodes from graph.json.

Background: graphify's AST extractor creates a single conceptual node for JS/template
framework verbs invoked from many files (Livewire `$set`, `$dispatch`, Alpine `x-data`,
blade `wire:click`, etc.). The `source_file` of that single node is whichever file
graphify happened to see it in first. Edges from many files point at one fake hub.

These nodes pollute god_nodes (the 9th god in afterpay was `$set(` with degree 14)
and distort community structure. They don't represent project architecture — they're
the Livewire/Alpine framework's vocabulary, used everywhere.

This script removes such nodes (and rewrites their edges) from graph.json, then writes
back in place. Idempotent: re-running on a cleaned graph is a no-op.

Reads:  graphify-out/graph.json
Writes: graphify-out/graph.json (in place; backup at graph.json.before-strip.bak)

Usage: python3 strip-framework-verbs.py [graph_path]
"""
from __future__ import annotations
import json
import re
import shutil
import sys
from pathlib import Path


# Patterns for nodes whose labels are framework-verbs, not architecture.
# Conservative — only strip patterns that are unambiguously Livewire/Alpine/blade verbs.
FRAMEWORK_VERB_PATTERNS = [
    # Livewire JS API — always invoked, never a defined function in user code
    re.compile(r"^\$set\b"),                    # $set(
    re.compile(r"^\$dispatch\b"),               # $dispatch(
    re.compile(r"^\$dispatchSelf\b"),
    re.compile(r"^\$dispatchTo\b"),
    re.compile(r"^\$refresh\b"),                # $refresh()
    re.compile(r"^\$emit\b"),                   # $emit( (Livewire v2 legacy)
    re.compile(r"^\$emitTo\b"),
    re.compile(r"^\$emitSelf\b"),
    re.compile(r"^\$on\b"),                     # $on(
    re.compile(r"^\$wire\b"),                   # $wire.* / $wire.set / $wire.call
    re.compile(r"^\$parent\b"),                 # $parent.* (nested components)
    re.compile(r"^\$errors\b"),                 # $errors / $errors.*
    re.compile(r"^\$get\b"),                    # $get( (server-side wire data accessor)
    re.compile(r"^\$entangle\b"),               # $entangle(
    re.compile(r"^\$call\b"),                   # $call(
    re.compile(r"^\$watch\b"),                  # $watch(
    # Alpine.js directives (x-*) — DOM-binding directives, not user functions
    re.compile(r"^x-(data|init|show|on|bind|cloak|effect|for|if|ignore|model|modelable|ref|teleport|text|html|id|transition|collapse|mask|anchor|trap|persist|intersect|sort|tooltip|menu|disclosure|dialog|listbox|combobox|popover|navigation|focus|tabs|switch|radio|checkbox|number-input|password-input|file-input|date-picker|time-picker|color-picker|range-slider|select|toggle|accordion|alert|avatar|badge|breadcrumbs|button|card|carousel|chip|dropdown|kbd|loader|notification|pagination|progress|rating|skeleton|spinner|stepper|table|tag|timeline|toast|tooltip|tree|wizard)\b", re.IGNORECASE),
    # Blade wire:* directives — only show up if extractor scans HTML attributes
    re.compile(r"^wire:(click|model|loading|target|confirm|dirty|init|initialized|key|keydown|keyup|navigate|navigate\.hover|offline|online|poll|replace|scroll|stream|submit|transition|ignore)\b"),
    # Alpine bindings sometimes show up too
    re.compile(r"^@(click|input|change|submit|keydown|keyup|focus|blur|mouseenter|mouseleave|scroll|resize)\b"),
]


def is_framework_verb(label: str) -> bool:
    """Return True if a node label matches any framework-verb pattern."""
    if not label:
        return False
    return any(p.match(label) for p in FRAMEWORK_VERB_PATTERNS)


def main() -> int:
    graph_path = Path(sys.argv[1] if len(sys.argv) > 1 else "graphify-out/graph.json")
    if not graph_path.exists():
        print(f"❌ {graph_path} not found", file=sys.stderr)
        return 1

    with open(graph_path, encoding="utf-8") as f:
        g = json.load(f)

    nodes = g.get("nodes", [])
    edges_key = "links" if "links" in g else "edges"
    edges = g.get(edges_key, [])

    # Identify framework-verb nodes by label
    verb_node_ids: set[str] = set()
    verb_labels: dict[str, str] = {}  # node_id -> label (for reporting)
    for n in nodes:
        if is_framework_verb(n.get("label", "")):
            nid = n.get("id")
            if nid:
                verb_node_ids.add(nid)
                verb_labels[nid] = n["label"]

    if not verb_node_ids:
        print("✓ no framework-verb nodes detected — graph is clean", file=sys.stderr)
        return 0

    # Backup before mutation
    backup = graph_path.with_suffix(".json.before-strip.bak")
    if not backup.exists():
        shutil.copy(graph_path, backup)
        print(f"💾 backup: {backup}", file=sys.stderr)

    # Filter
    new_nodes = [n for n in nodes if n.get("id") not in verb_node_ids]
    new_edges = [
        e
        for e in edges
        if e.get("source") not in verb_node_ids and e.get("target") not in verb_node_ids
    ]

    g["nodes"] = new_nodes
    g[edges_key] = new_edges
    graph_path.write_text(json.dumps(g, ensure_ascii=False, separators=(",", ":")), encoding="utf-8")

    # Report
    removed_node_count = len(nodes) - len(new_nodes)
    removed_edge_count = len(edges) - len(new_edges)
    print(f"✓ stripped {removed_node_count} framework-verb nodes, {removed_edge_count} dangling edges", file=sys.stderr)
    print(f"  {len(nodes)} → {len(new_nodes)} nodes, {len(edges)} → {len(new_edges)} edges", file=sys.stderr)
    print(f"\n  removed labels (sample):", file=sys.stderr)
    for nid, lbl in list(verb_labels.items())[:15]:
        print(f"    {lbl}", file=sys.stderr)
    if len(verb_labels) > 15:
        print(f"    ... and {len(verb_labels) - 15} more", file=sys.stderr)

    return 0


if __name__ == "__main__":
    sys.exit(main())
