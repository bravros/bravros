#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""make-code-groups.py — partition code+doc files into per-layer groups for the
semantic swarm pattern (parallel sub-agents).

WHY THIS EXISTS:
  When using parallel Claude Code sub-agents (Haiku/Sonnet) for semantic
  extraction instead of `graphify extract --backend gemini`, you need to
  pre-split files into worker-sized batches. Each Haiku worker has a ~200K
  context budget — that means roughly 40-50 PHP files per worker (avg 3-5K
  tokens per source file).

  This script reads `.graphify_detect.json`, buckets files by architectural
  layer (Services/Jobs/Listeners/Models/Livewire/etc), splits oversized
  buckets in half, and writes one .txt per group at `graphify-out/groups/`.

USAGE:
  python3 make-code-groups.py [project_root]

  Default project_root: current working directory.
  Requires: graphify-out/.graphify_detect.json (run ast-only-extract.py first).

OUTPUTS:
  graphify-out/groups/docs-1..6.txt           — doc files (md), 20-25 each
  graphify-out/groups/app-services-1.txt      — Services + Actions + Repositories
  graphify-out/groups/app-jobs-1.txt          — Jobs (split by alphabet)
  graphify-out/groups/app-listeners.txt       — Listeners
  graphify-out/groups/app-models-{1,2}.txt    — Eloquent models
  graphify-out/groups/app-livewire-{1a,1b...}.txt — Livewire components
  graphify-out/groups/app-console-{1,2,3}.txt — Console commands
  graphify-out/groups/app-http-{1,2}.txt      — Http controllers/middleware/requests
  graphify-out/groups/app-notifications-{1,2}.txt
  graphify-out/groups/app-foundation{a,b}.txt — Enums/Traits/Concerns/Interfaces
  graphify-out/groups/{config,routes,database,app-settings}.txt
  graphify-out/groups/app-misc.txt            — Policies/Rules/Observers/Providers

SIZE BUDGET:
  Hard cap: 50 files per group (safe for Haiku 4.5 context).
  Aggregate input ~150-250K tokens including the worker prompt.

  After partitioning, dispatch one Agent call per .txt file using the
  worker prompt at references/worker-prompt.md. Each agent writes
  graphify-out/.graphify_chunk_<group-name>.json. Then run finalize.sh.
"""
from __future__ import annotations
import json
import sys
from collections import defaultdict
from pathlib import Path

MAX_PER_GROUP = 50  # Haiku 4.5 context safety margin


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
    out = root / "graphify-out"
    groups_dir = out / "groups"
    groups_dir.mkdir(exist_ok=True, parents=True)

    detect_path = out / ".graphify_detect.json"
    if not detect_path.exists():
        print(f"❌ {detect_path} not found — run ast-only-extract.py first", file=sys.stderr)
        return 1

    det = json.loads(detect_path.read_text())
    docs_abs = sorted(det.get("files", {}).get("document", []))
    code_abs = sorted(det.get("files", {}).get("code", []))

    def rel(p_abs: str) -> str:
        try:
            return str(Path(p_abs).relative_to(root))
        except ValueError:
            return str(p_abs)

    # Doc groups (~20 each)
    docs_rel = [rel(d) for d in docs_abs]
    n_doc_groups = max(3, (len(docs_rel) + 24) // 25)
    if docs_rel:
        chunk = (len(docs_rel) + n_doc_groups - 1) // n_doc_groups
        for i in range(n_doc_groups):
            grp = docs_rel[i * chunk : (i + 1) * chunk]
            if grp:
                (groups_dir / f"docs-{i+1}.txt").write_text("\n".join(grp) + "\n")

    # Code groups bucketed by Laravel layer
    buckets: dict[str, list[str]] = defaultdict(list)
    for c_abs in code_abs:
        r = rel(c_abs)
        parts = r.split("/")
        if len(parts) < 2:
            buckets["top-level"].append(r)
            continue
        top = parts[0]
        if top == "app":
            layer = parts[1]
            if layer in {"Services", "Actions", "Repositories", "Util", "Helpers", "Support"}:
                buckets["app-services-actions"].append(r)
            elif layer in {"Jobs"}:
                buckets["app-jobs"].append(r)
            elif layer in {"Listeners"}:
                buckets["app-listeners"].append(r)
            elif layer in {"Events"}:
                buckets["app-events"].append(r)
            elif layer in {"Notifications", "Mail"}:
                buckets["app-notifications"].append(r)
            elif layer == "Models":
                buckets["app-models"].append(r)
            elif layer == "Livewire":
                buckets["app-livewire"].append(r)
            elif layer == "Console":
                buckets["app-console"].append(r)
            elif layer == "Http":
                buckets["app-http"].append(r)
            elif layer == "MercadoLivre":
                buckets["app-mercadolivre"].append(r)
            elif layer == "Settings":
                buckets["app-settings"].append(r)
            elif layer in {"Telegram"}:
                buckets["app-telegram"].append(r)
            elif layer in {"Enums", "Traits", "Concerns", "Contracts", "Interfaces", "Abstracts"}:
                buckets["app-foundation"].append(r)
            elif layer in {"Policies", "Rules", "Observers", "Health", "Providers", "Exports", "Exceptions"}:
                buckets["app-misc"].append(r)
            else:
                buckets["app-misc"].append(r)
        elif top == "tests":
            buckets["tests"].append(r)  # likely empty if .graphifyignore excludes tests/
        elif top == "resources":
            if len(parts) >= 2 and parts[1] == "views":
                buckets["resources-views"].append(r)
            elif len(parts) >= 2 and parts[1] == "lang":
                buckets["resources-lang"].append(r)
            else:
                buckets["resources-misc"].append(r)
        elif top in {"config", "routes", "database"}:
            buckets[top].append(r)
        else:
            buckets["other"].append(r)

    # Sub-split buckets > MAX_PER_GROUP, then write
    final: dict[str, list[str]] = {}
    for name, items in buckets.items():
        items_sorted = sorted(items)
        if not items_sorted:
            continue
        if len(items_sorted) <= MAX_PER_GROUP:
            final[name] = items_sorted
        else:
            n = (len(items_sorted) + MAX_PER_GROUP - 1) // MAX_PER_GROUP
            sz = (len(items_sorted) + n - 1) // n
            for i in range(n):
                chunk = items_sorted[i * sz : (i + 1) * sz]
                if chunk:
                    final[f"{name}-{i+1}"] = chunk

    for name, items in sorted(final.items(), key=lambda kv: -len(kv[1])):
        (groups_dir / f"{name}.txt").write_text("\n".join(items) + "\n")
        print(f"  {name}: {len(items)} files")

    total_groups = len(list(groups_dir.glob("*.txt")))
    total_files = sum(len((groups_dir / g.name).read_text().splitlines()) for g in groups_dir.glob("*.txt"))
    print()
    print(f"  ✓ {total_groups} groups, {total_files} files queued for semantic swarm")
    print(f"  Next: dispatch one Agent (model=haiku, subagent_type=general-purpose)")
    print(f"  per group, using references/worker-prompt.md as the system prompt.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
