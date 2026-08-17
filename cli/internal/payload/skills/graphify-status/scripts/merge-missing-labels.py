#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
merge-missing-labels.py — fold externally-produced labels into an existing graph, additively.

Takes the JSON that emit-relabel-prompt.py's prompt asked for ({"4171": "some-name", ...}),
merges it into community-labels.json, and patches only the matching nodes in graph.json.

THE INVARIANT, and the reason this script exists rather than reusing collate-labels.py:
an existing label is NEVER overwritten and a community absent from the results is NEVER
touched. collate-labels.py rebuilds the labels file from just the current run and stamps
"Community NN" over everything else — correct for a full relabel, catastrophic for a
missing-only pass. If you actually want to overwrite drifted names, pass --force and it
will tell you exactly which ones it replaced.

Usage:
    merge-missing-labels.py <project-path> <results.json> [more.json ...] [--force] [--dry-run]
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

NAME_RE = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")


def load_results(paths: list[Path]) -> tuple[dict[int, str], list[str]]:
    """Parse result files into {cid: name}, collecting complaints rather than dying."""
    out: dict[int, str] = {}
    problems: list[str] = []
    for p in paths:
        if not p.exists():
            problems.append(f"{p}: not found")
            continue
        raw = p.read_text(encoding="utf-8").strip()
        # Agents habitually wrap JSON in a markdown fence despite being told not to.
        if raw.startswith("```"):
            raw = re.sub(r"^```[a-z]*\n?", "", raw)
            raw = re.sub(r"\n?```$", "", raw).strip()
        try:
            data = json.loads(raw)
        except json.JSONDecodeError as exc:
            problems.append(f"{p.name}: malformed JSON ({exc})")
            continue
        if not isinstance(data, dict):
            problems.append(f"{p.name}: expected an object, got {type(data).__name__}")
            continue
        for cid_str, name in data.items():
            try:
                cid = int(cid_str)
            except (TypeError, ValueError):
                problems.append(f"{p.name}: non-numeric community id {cid_str!r}")
                continue
            name = str(name).strip()
            if not NAME_RE.match(name):
                problems.append(f"{p.name}: {cid} → {name!r} is not lowercase-kebab-case")
                continue
            if name.startswith("community-"):
                problems.append(f"{p.name}: {cid} → {name!r} is a placeholder, not a name")
                continue
            out[cid] = name
    return out, problems


def main() -> int:
    args = [a for a in sys.argv[1:]]
    force = "--force" in args
    dry = "--dry-run" in args
    args = [a for a in args if not a.startswith("--")]
    if len(args) < 2:
        print(__doc__.strip(), file=sys.stderr)
        return 2

    project = Path(args[0]).expanduser().resolve()
    result_paths = [Path(a).expanduser() for a in args[1:]]

    graph_path = project / "graphify-out" / "graph.json"
    labels_path = project / "graphify-out" / "community-labels.json"
    if not graph_path.exists():
        print(f"no graph at {graph_path}", file=sys.stderr)
        return 1

    incoming, problems = load_results(result_paths)
    for p in problems:
        print(f"  ⚠️  {p}", file=sys.stderr)
    if not incoming:
        print("no usable labels in the results — nothing merged", file=sys.stderr)
        return 1

    labels: dict[str, str] = {}
    if labels_path.exists():
        labels = json.loads(labels_path.read_text(encoding="utf-8"))

    graph = json.loads(graph_path.read_text(encoding="utf-8"))
    valid_cids = {n["community"] for n in graph.get("nodes", []) if n.get("community") is not None}

    added: dict[int, str] = {}
    replaced: dict[int, tuple[str, str]] = {}
    skipped_existing: list[int] = []
    unknown: list[int] = []

    for cid, name in incoming.items():
        if cid not in valid_cids:
            unknown.append(cid)
            continue
        key = str(cid)
        if key in labels:
            if force and labels[key] != name:
                replaced[cid] = (labels[key], name)
            else:
                skipped_existing.append(cid)
            continue
        added[cid] = name

    # Collisions matter: a name reused across communities is as useless as a number.
    final = dict(labels)
    for cid, name in added.items():
        final[str(cid)] = name
    for cid, (_, name) in replaced.items():
        final[str(cid)] = name

    seen: dict[str, list[str]] = {}
    for k, v in final.items():
        seen.setdefault(v, []).append(k)
    new_collisions = {
        v: ks for v, ks in seen.items()
        if len(ks) > 1 and any(int(k) in added or int(k) in replaced for k in ks)
    }

    print(f"{project.name}: {len(added)} added, {len(replaced)} replaced, "
          f"{len(skipped_existing)} left alone, {len(unknown)} unknown id(s)")
    for cid, name in sorted(added.items()):
        print(f"  + {cid} → {name}")
    for cid, (old, new) in sorted(replaced.items()):
        print(f"  ~ {cid} → {new}   (was {old})")
    if unknown:
        print(f"  ⚠️  not in this graph, ignored: {sorted(unknown)}")
    if skipped_existing and not force:
        print(f"  ℹ️  {len(skipped_existing)} already had names; --force to overwrite")
    if new_collisions:
        print("  ⚠️  name collisions introduced:")
        for name, ks in sorted(new_collisions.items()):
            print(f"       {name} ← communities {', '.join(sorted(ks))}")

    if not added and not replaced:
        print("nothing to write")
        return 0
    if dry:
        print("\n--dry-run: no files written")
        return 0

    labels_path.parent.mkdir(parents=True, exist_ok=True)
    labels_path.write_text(
        json.dumps({k: final[k] for k in sorted(final, key=int)}, indent=2, ensure_ascii=False),
        encoding="utf-8",
    )

    # graphify renamed the node field `community_label` -> `community_name`. Stamp the
    # key this graph already uses; a never-labelled graph carries neither, so write both
    # rather than guess which one this graphify build will read back.
    node_keys = [
        k for k in ("community_label", "community_name")
        if any(k in n for n in graph.get("nodes", []))
    ] or ["community_label", "community_name"]

    touched = set(added) | set(replaced)
    patched = 0
    for n in graph.get("nodes", []):
        cid = n.get("community")
        if cid in touched:
            for k in node_keys:
                n[k] = final[str(cid)]
            patched += 1

    # Match graphify's own on-disk format: compact separators, unicode preserved.
    graph_path.write_text(
        json.dumps(graph, ensure_ascii=False, separators=(",", ":")), encoding="utf-8"
    )
    print(f"\n✓ {labels_path.name}: {len(final)} labels")
    print(f"✓ {graph_path.name}: {patched} nodes relabelled")
    print("\nCommit both files — the graph is tracked so the labels travel via git pull.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
