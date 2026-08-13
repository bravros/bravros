#!/usr/bin/env bash
#
# apply-dedup-fix.sh — install graphifyy, and patch dedup.py only on pre-0.9 builds.
#
# Idempotent: safe to re-run. Verifies the patch is in place via content hash.
# Vendored from: https://github.com/safishamsi/graphify/blob/v0.7.10/graphify/dedup.py
# Patched: dedup.py:131-135 keys on (label, source_file) instead of label-only.
#
# 2026-08-04 — upstream merged the fix (#1504, "pre-#1504 scheme keyed off the bare
# filename stem"); 0.9.x dedup.py already keys on source_file throughout. The patch is
# therefore SKIPPED on >= 0.9.0: applying a v0.7.10 file onto 0.9.x would revert the new
# ID scheme and silently downgrade extraction. The pin moved to 0.9.33 for the same
# reason — 0.8.1 and 0.9.x produce structurally different graphs (paylog: 26k vs 59k
# nodes for the same code), so two machines on different pins ping-pong the committed
# graph.json and every label resync aborts. Keep ALL machines on the same pin.
#
# Usage: bash apply-dedup-fix.sh
#
set -euo pipefail

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." >/dev/null && pwd)"
PATCHED_DEDUP="$SKILL_DIR/references/dedup-patched.py"

if [ ! -f "$PATCHED_DEDUP" ]; then
    echo "❌ vendored dedup-patched.py missing at $PATCHED_DEDUP"
    exit 1
fi

# 1. Ensure uv is available
if ! command -v uv >/dev/null 2>&1; then
    echo "❌ uv not installed. Run: curl -LsSf https://astral.sh/uv/install.sh | sh"
    exit 1
fi

# 2. Install graphifyy with openai dep (idempotent)
# Pinned version: read from the skill-local references/.graphify-version pin if
# present (single source of truth, in-project — no ~/Sites/context dependency).
# Override at call time with GRAPHIFY_PIN env. Fallback to the upstream PyPI
# release graphifyy==0.9.38.
GRAPHIFY_VERSION_FILE="$SKILL_DIR/references/.graphify-version"
if [ -n "${GRAPHIFY_PIN:-}" ]; then
    : # caller-supplied pin wins
elif [ -f "$GRAPHIFY_VERSION_FILE" ]; then
    GRAPHIFY_PIN=$(tr -d '[:space:]' < "$GRAPHIFY_VERSION_FILE")
else
    GRAPHIFY_PIN="0.9.38"
fi
echo "📦 ensuring graphifyy[mcp]==${GRAPHIFY_PIN} is installed (with openai dep)..."
# --force so a pin CHANGE actually takes: `uv tool install` no-ops when the tool is
# already present at another version (that is how a machine got stuck on 0.8.1).
# [mcp] extra is REQUIRED: it provides the mcp dependency for the user-scoped
# graphify-mcp server (registered in Claude via `claude mcp add --scope user`);
# installing without it leaves a graphify-mcp binary that dies on import.
uv tool install "graphifyy[mcp]==${GRAPHIFY_PIN}" --python 3.12 --with openai --force 2>&1 | tail -3

# 2b. VERSION GATE — the vendored patch is a v0.7.10 file. Upstream merged the dedup fix
# in 0.9 (#1504) along with a new node-ID scheme, so overwriting 0.9.x dedup.py with it
# would downgrade extraction. Skip the patch entirely on >= 0.9.0.
INSTALLED_VERSION=$(graphify --version 2>/dev/null | grep -Eo '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
if [ -n "$INSTALLED_VERSION" ]; then
    _major=${INSTALLED_VERSION%%.*}; _rest=${INSTALLED_VERSION#*.}; _minor=${_rest%%.*}
    if [ "$_major" -gt 0 ] || [ "$_minor" -ge 9 ]; then
        echo "✅ graphify ${INSTALLED_VERSION} already carries the upstream dedup fix (#1504) — patch skipped"
        exit 0
    fi
fi

# 3. Locate the installed dedup.py
TOOL_DIR="$HOME/.local/share/uv/tools/graphifyy"
INSTALLED_DEDUP=$(find "$TOOL_DIR" -path '*/site-packages/graphify/dedup.py' 2>/dev/null | head -1)

if [ -z "$INSTALLED_DEDUP" ]; then
    echo "❌ couldn't find installed dedup.py under $TOOL_DIR"
    exit 1
fi

echo "📍 found graphifyy install: $(dirname "$INSTALLED_DEDUP")"

# 4. Idempotency check: hash the installed file vs the vendored patched version
INSTALLED_HASH=$(shasum -a 256 "$INSTALLED_DEDUP" | cut -d' ' -f1)
PATCHED_HASH=$(shasum -a 256 "$PATCHED_DEDUP" | cut -d' ' -f1)

if [ "$INSTALLED_HASH" = "$PATCHED_HASH" ]; then
    echo "✅ patch already applied (hashes match) — nothing to do"
    exit 0
fi

# 5. Backup the upstream version
BACKUP="$INSTALLED_DEDUP.upstream.bak"
if [ ! -f "$BACKUP" ]; then
    cp "$INSTALLED_DEDUP" "$BACKUP"
    echo "💾 backed up upstream dedup.py → $BACKUP"
else
    echo "💾 backup already exists at $BACKUP (not overwriting)"
fi

# 6. Apply patch (overwrite installed with vendored)
cp "$PATCHED_DEDUP" "$INSTALLED_DEDUP"
echo "🔧 applied patch: dedup.py keys on (label, source_file)"

# 7. Verify graphify CLI still works
if graphify --help >/dev/null 2>&1; then
    echo "✅ graphify CLI verified working"
else
    echo "⚠️  graphify --help failed after patch — restoring backup"
    cp "$BACKUP" "$INSTALLED_DEDUP"
    exit 1
fi

echo ""
echo "📋 next: ensure GEMINI_API_KEY (or backend of choice) is set,"
echo "        then run: graphify extract . --backend gemini"
