#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
graphify-status.py — coverage report for every graphify-enabled project on this machine.

Answers one question: does each project's knowledge graph actually have usable community
names, or is `graphify query` still handing back "Community 417"? Labels are keyed by
community id and decay silently as clustering renumbers on rebuild, so coverage regresses
without anything failing — this is the check that surfaces it.

Discovery: walks each root for `.graphify` or `graphify-out/graph.json` (default root
~/Sites, depth 3). Projects with the marker but no graph are reported as "not built".

On detecting degraded coverage it also writes a paste-ready labelling prompt per project
(see emit-relabel-prompt.py) — detection without a next step is how a graph stays degraded
between checks. Prompts go to /tmp; the repo is only touched once you run
merge-missing-labels.py with the agent's reply.

Usage:
    graphify-status.py [root ...] [--json] [--depth N] [--no-prompt]
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

DEFAULT_ROOTS = [Path.home() / "Sites"]
DEFAULT_DEPTH = 3


def discover(roots: list[Path], depth: int) -> list[Path]:
    found: set[Path] = set()
    for root in roots:
        if not root.is_dir():
            continue
        base = len(root.parts)
        for dirpath, dirnames, filenames in os.walk(root):
            p = Path(dirpath)
            if len(p.parts) - base >= depth:
                dirnames[:] = []
                continue
            # never descend into heavy or irrelevant trees
            dirnames[:] = [
                d for d in dirnames
                if d not in {"node_modules", "vendor", ".git", "graphify-out", "storage", ".venv"}
            ]
            if ".graphify" in filenames or (p / "graphify-out" / "graph.json").exists():
                found.add(p)
                dirnames[:] = []  # a project is a leaf; don't nest
    return sorted(found)


def node_label(node: dict) -> str | None:
    """Real community name on a node, or None if it is unnamed.

    graphify renamed this node field `community_label` -> `community_name`. A graph
    carries whichever key was current when it was built, so read both — reading only
    the old key scores a correctly-labelled new-format graph as 100% uncovered.
    The "Community NN" placeholder counts as unnamed.
    """
    for key in ("community_label", "community_name"):
        value = node.get(key)
        if value and not str(value).startswith("Community "):
            return str(value)
    return None


def measure(project: Path) -> dict:
    row = {
        "project": project.name,
        "path": str(project),
        "communities": 0,
        "labels": 0,
        "numeric": 0,
        "nodes": 0,
        "pct": 0.0,
        "status": "ok",
    }
    graph = project / "graphify-out" / "graph.json"
    if not graph.exists():
        row["status"] = "not built"
        return row
    try:
        g = json.loads(graph.read_text(encoding="utf-8"))
    except Exception as exc:  # corrupt or partially written graph
        row["status"] = f"unreadable ({type(exc).__name__})"
        return row

    nodes = g.get("nodes", [])
    row["nodes"] = len(nodes)
    row["communities"] = len({n["community"] for n in nodes if n.get("community") is not None})
    # A node is uncovered if it has no label at all OR the placeholder "Community NN".
    # Counting only the placeholder would score a never-labelled graph as 0% — healthy-
    # looking, and wrong: those nodes render as bare numbers too.
    # graphify renamed the node field `community_label` -> `community_name`; graphs
    # carry one or the other depending on when they were built, so read both.
    row["numeric"] = sum(1 for n in nodes if not node_label(n))
    row["pct"] = (100.0 * row["numeric"] / len(nodes)) if nodes else 0.0

    labels = project / "graphify-out" / "community-labels.json"
    if labels.exists():
        try:
            row["labels"] = len(json.loads(labels.read_text(encoding="utf-8")))
        except Exception:
            row["status"] = "labels unreadable"
    return row


def render(rows: list[dict]) -> str:
    headers = ("project", "communities", "labels", "nodes showing Community NN")
    body = []
    for r in rows:
        if r["status"] != "ok":
            body.append((r["project"], "—", "—", r["status"]))
            continue
        if r["numeric"] == 0:
            cov = "0%"
        else:
            cov = f"{r['numeric']:,} / {r['nodes']:,} — {r['pct']:.0f}%"
        body.append((r["project"], f"{r['communities']:,}", f"{r['labels']:,}", cov))

    widths = [max(len(h), *(len(c[i]) for c in body)) if body else len(h)
              for i, h in enumerate(headers)]
    top = "┌" + "┬".join("─" * (w + 2) for w in widths) + "┐"
    sep = "├" + "┼".join("─" * (w + 2) for w in widths) + "┤"
    bot = "└" + "┴".join("─" * (w + 2) for w in widths) + "┘"

    def line(cells):
        return "│" + "│".join(f" {c.ljust(w)} " for c, w in zip(cells, widths)) + "│"

    out = [top, line(headers)]
    for cells in body:
        out += [sep, line(cells)]
    out.append(bot)
    return "\n".join(out)


def emit_prompts(degraded: list[dict], suppressed: bool) -> None:
    """Generate a paste-ready labelling prompt for each degraded project.

    Detection without a next step is what let paylog sit degraded between checks, so this
    fires automatically. It only ever writes to /tmp — the repo is untouched until the
    operator runs merge-missing-labels.py with the results.
    """
    if suppressed:
        print()
        print("Fix: re-run without --no-prompt to generate a labelling prompt, or run")
        print("emit-relabel-prompt.py <project> directly. Do NOT use collate-labels.py for a")
        print("gap-fill — it rebuilds the labels file from one run and placeholders the rest.")
        return

    emitter = Path(__file__).parent / "emit-relabel-prompt.py"
    if not emitter.exists():  # deployed copy predates the emitter
        return

    print()
    print("Labelling prompts (paste into Antigravity, Gemini, or any agent):")
    for r in degraded:
        try:
            proc = subprocess.run(
                [sys.executable, str(emitter), r["path"]],
                capture_output=True, text=True, timeout=120,
            )
        except (OSError, subprocess.SubprocessError) as exc:
            print(f"  {r['project']}: prompt generation failed ({type(exc).__name__}) — "
                  f"run {emitter} {r['path']} by hand")
            continue
        if proc.returncode != 0:
            detail = (proc.stderr or "").strip().splitlines()
            print(f"  {r['project']}: prompt generation failed"
                  f"{' — ' + detail[-1] if detail else ''}")
            continue
        for line in proc.stdout.rstrip().splitlines():
            print(f"  {line}" if line else "")


def main() -> int:
    args = sys.argv[1:]
    as_json = "--json" in args
    no_prompt = "--no-prompt" in args
    args = [a for a in args if a not in ("--json", "--no-prompt")]
    depth = DEFAULT_DEPTH
    if "--depth" in args:
        i = args.index("--depth")
        depth = int(args[i + 1])
        del args[i:i + 2]
    roots = [Path(a).expanduser() for a in args] or DEFAULT_ROOTS

    projects = discover(roots, depth)
    if not projects:
        print(f"No graphify-enabled projects found under: {', '.join(str(r) for r in roots)}")
        return 0

    rows = [measure(p) for p in projects]
    # healthiest first, then by size; unbuilt/broken sink to the bottom
    rows.sort(key=lambda r: (r["status"] != "ok", r["pct"], -r["nodes"]))

    if as_json:
        print(json.dumps(rows, indent=2))
        return 0

    print(render(rows))

    degraded = [r for r in rows if r["status"] == "ok" and r["numeric"]]
    unbuilt = [r for r in rows if r["status"] != "ok"]
    if degraded:
        print()
        print("Degraded coverage — these answer some queries with numbers instead of names:")
        for r in degraded:
            missing = r["communities"] - r["labels"]
            print(f"  {r['project']}: {missing:,} of {r['communities']:,} communities unnamed"
                  f"  ({r['path']})")
        emit_prompts(degraded, no_prompt)
    if unbuilt:
        print()
        for r in unbuilt:
            print(f"  {r['project']}: {r['status']} ({r['path']})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
