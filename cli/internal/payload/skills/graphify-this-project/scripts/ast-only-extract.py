#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "graphifyy>=0.7.10",
# ]
# ///
"""ast-only-extract.py — run graphify detect + AST extraction WITHOUT an LLM backend.

NOTE on the patched dedup: when run via `uv run --script`, uv creates an isolated
venv with the upstream graphifyy package (no afterpay dedup patch). If you need
the patched dedup behavior (PSR-4 per-class disambiguation), invoke this script
with the graphifyy uv-tool interpreter instead:
  $HOME/.local/share/uv/tools/graphifyy/bin/python3 ast-only-extract.py [project_root]

WHY THIS EXISTS:
  `graphify extract` requires a `--backend` (gemini|kimi|claude|openai|ollama),
  even if you only want the deterministic AST layer. There is no `--ast-only` flag.
  This script calls graphify's Python API directly to bypass the LLM step,
  producing `.graphify_ast.json` and `.graphify_detect.json` for use in either:
    1. The semantic swarm pattern (parallel sub-agents fill semantic chunks)
    2. A standalone AST-only graph (no semantic edges)

  Use this when:
    - You don't have a Gemini/Kimi/OpenAI API key handy
    - You want to use Claude Code parallel sub-agents for semantic instead of the
      built-in LLM backends (zero cost on Pro/Max plans)
    - You're prototyping the graph and don't need semantic enrichment yet

USAGE:
  /Users/$USER/.local/share/uv/tools/graphifyy/bin/python3 \
    /Users/$USER/.claude/skills/graphify-this-project/scripts/ast-only-extract.py [project_root]

  Default project_root: current working directory.

OUTPUTS:
  graphify-out/.graphify_detect.json   — file inventory (code/docs/papers/images)
  graphify-out/.graphify_ast.json      — AST nodes + edges (deterministic, no LLM)
  graphify-out/cache/ast/*             — per-file AST caches (reused by `graphify update`)

NOTE: this script needs the `if __name__ == "__main__":` guard — graphify's
extract() uses multiprocessing which fails without it on macOS.
"""
import json
import sys
from pathlib import Path


def main() -> int:
    from graphify.detect import detect
    from graphify.extract import collect_files, extract

    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
    out = root / "graphify-out"
    out.mkdir(exist_ok=True)

    print(f"▸ Detecting files in {root}...")
    det = detect(root)
    (out / ".graphify_detect.json").write_text(json.dumps(det, indent=2, default=str))
    files_summary = det.get("files", {})
    print(f"  ✓ Detected: code={len(files_summary.get('code', []))} "
          f"document={len(files_summary.get('document', []))} "
          f"paper={len(files_summary.get('paper', []))} "
          f"image={len(files_summary.get('image', []))} "
          f"total_words={det.get('total_words', 0):,}")

    print("▸ Collecting code files for AST...")
    code_files: list[Path] = []
    for f in files_summary.get("code", []):
        p = Path(f)
        if p.is_dir():
            code_files.extend(collect_files(p, root=root))
        else:
            code_files.append(p)
    code_files = sorted(set(code_files))
    print(f"  ✓ {len(code_files)} code files to AST-extract")

    if not code_files:
        (out / ".graphify_ast.json").write_text(
            json.dumps({"nodes": [], "edges": [], "input_tokens": 0, "output_tokens": 0}))
        print("  → No code files; wrote empty AST.")
        return 0

    print("▸ Running AST extraction (parallel, deterministic, no LLM)...")
    result = extract(code_files, cache_root=root)
    (out / ".graphify_ast.json").write_text(json.dumps(result, indent=2, default=str))
    print(f"  ✓ AST: {len(result.get('nodes', []))} nodes, {len(result.get('edges', []))} edges")

    # Also write the python interpreter pointer for finalize.sh
    interpreter = sys.executable
    (out / ".graphify_python").write_text(interpreter)
    print(f"  ✓ wrote .graphify_python pointer ({interpreter})")

    return 0


if __name__ == "__main__":
    sys.exit(main())
