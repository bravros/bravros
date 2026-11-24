#!/usr/bin/env bash
#
# apply-dedup-fix.sh — install the pinned graphifyy (with the [mcp] extra).
#
# Name kept for compatibility with SKILL.md/runbook references. The old dedup.py patch
# is gone: upstream merged the fix in 0.9 (#1504, dedup keys on source_file) together
# with a new node-ID scheme, so the vendored v0.7.10 file would DOWNGRADE extraction.
# Pins below 0.9.0 are therefore refused.
#
# One pin source: references/.graphify-version (override per call with GRAPHIFY_PIN).
# Keep ALL machines on the same pin — 0.8.1 and 0.9.x produce structurally different
# graphs (paylog: 26k vs 59k nodes for the same code), so mixed pins ping-pong the
# committed graph.json and every label resync aborts. The inline `graphifyy==` pins in
# scripts/{ast-only-extract,regen-html-with-labels,suppress-hyperedges-viz}.py must match.
#
# Usage: bash apply-dedup-fix.sh
#
set -euo pipefail

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." >/dev/null && pwd)"

# 1. Ensure uv is available
if ! command -v uv >/dev/null 2>&1; then
    echo "❌ uv not installed. Run: curl -LsSf https://astral.sh/uv/install.sh | sh"
    exit 1
fi

# 2. Resolve the pin (no hard-coded fallback — the version file is the single source)
GRAPHIFY_VERSION_FILE="$SKILL_DIR/references/.graphify-version"
if [ -n "${GRAPHIFY_PIN:-}" ]; then
    : # caller-supplied pin wins
elif [ -f "$GRAPHIFY_VERSION_FILE" ]; then
    GRAPHIFY_PIN=$(tr -d '[:space:]' < "$GRAPHIFY_VERSION_FILE")
else
    echo "❌ pin file missing: $GRAPHIFY_VERSION_FILE (and GRAPHIFY_PIN unset)"
    exit 1
fi
if [ -z "$GRAPHIFY_PIN" ]; then
    echo "❌ empty graphifyy pin in $GRAPHIFY_VERSION_FILE"
    exit 1
fi
_pin_major=${GRAPHIFY_PIN%%.*}; _pin_rest=${GRAPHIFY_PIN#*.}; _pin_minor=${_pin_rest%%.*}
if [ "$_pin_major" -eq 0 ] && [ "$_pin_minor" -lt 9 ]; then
    echo "❌ graphifyy ${GRAPHIFY_PIN} predates the upstream dedup fix (#1504) — use >= 0.9.0"
    exit 1
fi

# 3. Install (idempotent)
echo "📦 ensuring graphifyy[mcp]==${GRAPHIFY_PIN} is installed (with openai dep)..."
# --force so a pin CHANGE actually takes: `uv tool install` no-ops when the tool is
# already present at another version (that is how a machine got stuck on 0.8.1).
# [mcp] extra is REQUIRED: it provides the mcp dependency for the user-scoped
# graphify-mcp server (registered in Claude via `claude mcp add --scope user`);
# installing without it leaves a graphify-mcp binary that dies on import.
uv tool install "graphifyy[mcp]==${GRAPHIFY_PIN}" --python 3.12 --with openai --force 2>&1 | tail -3

# 4. Verify
INSTALLED_VERSION=$(graphify --version 2>/dev/null | grep -Eo '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)
if [ "$INSTALLED_VERSION" != "$GRAPHIFY_PIN" ]; then
    echo "❌ installed graphify '${INSTALLED_VERSION:-none}' != pin ${GRAPHIFY_PIN}"
    exit 1
fi
echo "✅ graphify ${INSTALLED_VERSION} installed (carries the upstream dedup fix #1504)"
echo ""
echo "📋 next: set a backend key (e.g. GEMINI_API_KEY) if you plan a paid extract,"
echo "        or run the default AST-only path (see references/setup-runbook.md)"
