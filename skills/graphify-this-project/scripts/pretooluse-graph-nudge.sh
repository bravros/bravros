#!/usr/bin/env bash
#
# pretooluse-graph-nudge.sh — PreToolUse hook for Claude Code that nudges
# agents to query the graphify CLI BEFORE running broad text searches.
#
# Reads the tool input from stdin (Claude Code's PreToolUse contract), checks
# whether the command is a search command (grep/rg/find/fd/ack/ag), and if so
# emits an additionalContext hint pointing at the project's CLI-accessible graph.
#
# MCP is retired. In the in-project model the graph lives at
# graphify-out/graph.json (committed to the repo). This script prefers that
# in-project path, falls back to .graphify graph_path/context_path and then
# .mcp.json for legacy/unmigrated projects, and suggests
# `graphify query --graph <path>`.
#
# Idempotent: safe to fire on every PreToolUse event. Only produces output
# when the tool is a recognized search command AND the graph is accessible.

set -euo pipefail

INPUT=$(cat 2>/dev/null || true)
if [ -z "$INPUT" ]; then
    exit 0
fi

CMD=$(uv run python -c "import json,sys; d=json.load(sys.stdin); print(d.get('tool_input',d).get('command',''))" 2>/dev/null <<<"$INPUT" || echo "")

case "$CMD" in
    *grep*|*rg\ *|*ripgrep*|*find\ *|*fd\ *|*ack\ *|*ag\ *)
        # Prefer the in-project graph; fall back to .graphify graph_path/context_path,
        # then .mcp.json for legacy/unmigrated projects.
        GRAPH=$(uv run python -c "
import os, json, yaml
# 1. In-project graph (the new default)
if os.path.exists('graphify-out/graph.json'):
    print('graphify-out/graph.json')
    exit(0)
# 2. .graphify graph_path (in-project) or context_path (legacy central)
if os.path.exists('.graphify'):
    try:
        with open('.graphify') as f:
            doc = yaml.safe_load(f) or {}
        for key in ('graph_path', 'context_path'):
            cp = doc.get(key, '')
            if cp and os.path.exists(cp):
                print(cp)
                exit(0)
    except: pass
# 3. Fallback: .mcp.json (legacy projects)
if os.path.exists('.mcp.json'):
    try:
        with open('.mcp.json') as f:
            d = json.load(f)
        s = next(iter(d.get('mcpServers', {}).values()), {})
        cp = s.get('args', [''])[0]
        if cp and os.path.exists(cp):
            print(cp)
            exit(0)
    except: pass
" 2>/dev/null || echo "")

        if [ -n "$GRAPH" ] && [ -f "$GRAPH" ]; then
            cat <<JSON
{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"graphify: Knowledge graph available via `graphify query --graph $GRAPH`. Query the graphify CLI BEFORE running broad text searches."}}
JSON
        fi
        ;;
esac

exit 0
