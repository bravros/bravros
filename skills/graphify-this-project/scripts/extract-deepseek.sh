#!/usr/bin/env bash
# extract-deepseek.sh — ON-DEMAND in-project semantic LLM refresh via DeepSeek
#
# Usage: extract-deepseek.sh [project-dir]   (default: $PWD)
#
# IN-PROJECT MODEL: writes the graph to <project-dir>/graphify-out/ (graphify's
# default --out target). The graph is committed to the project's OWN git, so it
# travels via `git pull` across machines. There is NO ~/Sites/context redirect
# and NO _global cross-project graph.
#
# This is the EXPENSIVE semantic pass — it re-runs AST extraction AND calls the
# DeepSeek LLM to (re)derive semantic edges + community labels. It is SEPARATE
# from the cheap background AST refresh installed by `graphify hook install`
# (that one is AST-only, ~30s, no LLM). Run this by hand when you want fresh
# semantic communities, not on every commit.
#
# Loads DEEPSEEK_API_KEY via op (1Password) JIT or honors a pre-exported key.
# Never writes the key to disk. On success: best-effort updates .graphify
# (last_llm_run) if write-graphify-state.sh is present.
#
# Default model: deepseek-v4-flash. Override with DEEPSEEK_MODEL
# (e.g. deepseek-v4-pro for higher-quality semantic edges).
#
# Prerequisite: uv tool install graphifyy --with openai (openai-compatible backend)

set -euo pipefail

PROJECT_DIR="${1:-$PWD}"
PROJECT_DIR="$(cd "$PROJECT_DIR" && pwd)"

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." >/dev/null && pwd)"
STATE_SCRIPT="$SKILL_DIR/scripts/write-graphify-state.sh"
LABEL_SCRIPT="$SKILL_DIR/scripts/label-communities-deepseek.sh"

DEEPSEEK_BASE_URL="${DEEPSEEK_BASE_URL:-https://api.deepseek.com}"
DEEPSEEK_MODEL="${DEEPSEEK_MODEL:-deepseek-v4-flash}"

LOG_DIR="/tmp/graphify-llm"
mkdir -p "$LOG_DIR"
LOG="$LOG_DIR/extract-deepseek-$(date +%Y%m%d-%H%M%S).log"

# --- Secret loading ---
# 1Password reference: op://ClaudeCode/DeepSeek Api/password
load_secrets() {
    if [ -z "${DEEPSEEK_API_KEY:-}" ] && command -v op >/dev/null 2>&1; then
        if op account get >/dev/null 2>&1; then
            export DEEPSEEK_API_KEY="$(op read 'op://ClaudeCode/DeepSeek Api/password' 2>/dev/null || true)"
        fi
    fi

    if [ -z "${DEEPSEEK_API_KEY:-}" ]; then
        echo "extract-deepseek: DEEPSEEK_API_KEY not set and not in 1Password — cannot run."
        echo "  Interactive: ensure DEEPSEEK_API_KEY is exported (op_lazy / opload)."
        echo "  Manual: op read 'op://ClaudeCode/DeepSeek Api/password' should resolve."
        echo "  CI: export DEEPSEEK_API_KEY directly."
        return 1
    fi
}

# --- Run extraction (AST + semantic) into <project>/graphify-out/ ---
run_extraction() {
    local dir="$1"
    cd "$dir"

    echo "=== extract-deepseek $(date -Iseconds) ==="
    echo "  project:   $dir"
    echo "  model:     $DEEPSEEK_MODEL"
    echo "  backend:   openai-compatible ($DEEPSEEK_BASE_URL)"
    echo "  out:       $dir/graphify-out (in-project)"

    export OPENAI_API_KEY="$DEEPSEEK_API_KEY"
    export OPENAI_BASE_URL="$DEEPSEEK_BASE_URL"
    export GRAPHIFY_OPENAI_MODEL="$DEEPSEEK_MODEL"

    local start_ts; start_ts=$(date +%s)

    # IN-PROJECT: default --out (.) writes to <dir>/graphify-out/graph.json.
    if ! graphify extract . --backend openai --model "$DEEPSEEK_MODEL" 2>&1; then
        echo "  ✗ graphify extract failed"
        return 1
    fi

    local end_ts; end_ts=$(date +%s)
    local duration=$((end_ts - start_ts))

    local files
    files=$(find . -type f -not -path './.git/*' -not -path './vendor/*' -not -path './node_modules/*' -not -path './graphify-out/*' | wc -l | tr -d ' ')
    local nodes=0 edges=0
    local graph_path="$dir/graphify-out/graph.json"
    if [ -f "$graph_path" ]; then
        nodes=$(jq -r '.nodes | length' "$graph_path" 2>/dev/null || echo 0)
        edges=$(jq -r '.edges | length' "$graph_path" 2>/dev/null || echo 0)
    fi

    local cost
    cost=$(awk -v n="$nodes" 'BEGIN{
        inp = n * 200
        out = n * 30
        printf "%.4f", inp/1e6*0.14 + out/1e6*0.28
    }')

    echo "  ✓ done in ${duration}s"
    echo "  nodes=$nodes edges=$edges files=$files cost_est=\$$cost"

    if [ -f "$STATE_SCRIPT" ]; then
        bash "$STATE_SCRIPT" update-llm "$dir" \
            "openai-deepseek" "$DEEPSEEK_MODEL" \
            "$files" "$nodes" "$edges" \
            "$cost" "$duration" "success" 2>/dev/null || true
    fi

    return 0
}

# --- Label communities (in-project graph.json, soft-fail) ---
run_labeling() {
    local out_dir="$1"
    echo ""
    echo "--- community labeling ---"

    if [ ! -x "$LABEL_SCRIPT" ]; then
        echo "  label script not found at $LABEL_SCRIPT — skipping"
        return 0
    fi

    if "$LABEL_SCRIPT" "$out_dir" 2>&1; then
        echo "  ✓ communities labeled"
    else
        echo "  ⚠ community labeling failed — graph retains Community N names"
    fi
    return 0
}

# --- Main ---
GRAPH_OUT="$PROJECT_DIR/graphify-out"
mkdir -p "$GRAPH_OUT"

echo "extract-deepseek (in-project): $PROJECT_DIR" | tee "$LOG"

if ! load_secrets; then
    echo "extract-deepseek: secret loading failed — aborting" | tee -a "$LOG"
    exit 1
fi

START=$(date +%s)

_run_and_wrapup() {
    if run_extraction "$PROJECT_DIR" >> "$LOG" 2>&1; then
        local duration=$(($(date +%s) - START))
        echo "extract-deepseek: extraction SUCCESS — ${duration}s" | tee -a "$LOG"
        run_labeling "$GRAPH_OUT" >> "$LOG" 2>&1
        return 0
    fi
    return 1
}

if _run_and_wrapup; then
    echo "extract-deepseek: graph written to $GRAPH_OUT/graph.json" | tee -a "$LOG"
    echo "  → review, then commit it to THIS repo (it travels via git pull):" | tee -a "$LOG"
    echo "    git add graphify-out/graph.json graphify-out/community-labels.json graphify-out/GRAPH_REPORT.md" | tee -a "$LOG"
    exit 0
else
    echo "extract-deepseek: first attempt failed — retrying in 60s..." | tee -a "$LOG"
    sleep 60
    START=$(date +%s)
    if _run_and_wrapup; then
        echo "extract-deepseek: graph written to $GRAPH_OUT/graph.json" | tee -a "$LOG"
        exit 0
    fi
    echo "extract-deepseek: FAILED after retry — see $LOG" | tee -a "$LOG"
    exit 1
fi
