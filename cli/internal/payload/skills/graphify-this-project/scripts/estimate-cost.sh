#!/usr/bin/env bash
# estimate-cost.sh — estimate DeepSeek LLM extraction cost based on AST node count
#
# Usage: estimate-cost.sh <ast-node-count>
# Output: estimated cost in USD (float)
#
# Heuristic (tune from .graphify history after a few real runs):
#   input tokens  ≈ ast_nodes × 200
#   output tokens ≈ ast_nodes × 30
#   DeepSeek V4 Flash pricing (per locked decision #3, 2026-05-13):
#     $0.14/M input (cache miss), $0.0028/M input (cache hit), $0.28/M output
#   Conservative estimate uses cache-miss pricing for input.

set -euo pipefail

NODES="${1:-0}"

if [ "$NODES" -eq 0 ]; then
    echo "0.00"
    exit 0
fi

uv run python -c "
nodes = int($NODES)
input_tokens = nodes * 200
output_tokens = nodes * 30
cost = (input_tokens / 1_000_000) * 0.14 + (output_tokens / 1_000_000) * 0.28
print(f'{cost:.2f}')
"
