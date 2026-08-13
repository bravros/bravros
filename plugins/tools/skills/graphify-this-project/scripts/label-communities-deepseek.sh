#!/usr/bin/env bash
#
# label-communities-deepseek.sh — headless community labeling via DeepSeek
#
# Fixes the "community0, community1, ..." problem in headless extraction.
# Reads the graph, groups communities into batches, sends them to DeepSeek
# for semantic naming, and writes community-labels.json.
#
# Usage: label-communities-deepseek.sh <context-out-dir>
#
# Behavior:
#   1. Reads <context-out>/graph.json
#   2. Extracts communities with their top nodes
#   3. Chunked into batches of LABEL_BATCH_SIZE (default 30 communities)
#   4. Serial dispatch to DeepSeek via openai-compatible endpoint
#   5. On bad JSON: log, skip batch, continue (soft-fail)
#   6. Writes <context-out>/community-labels.json
#   7. Applies labels onto graph.json nodes via apply-labels.py (no re-cluster)
#
# Prerequisites: DEEPSEEK_API_KEY set via env or 1Password

set -euo pipefail

CONTEXT_OUT="${1:-}"
if [ -z "$CONTEXT_OUT" ] || [ ! -d "$CONTEXT_OUT" ]; then
    echo "Usage: label-communities-deepseek.sh <context-out-dir>" >&2
    exit 1
fi

GRAPH_JSON="$CONTEXT_OUT/graph.json"
LABELS_JSON="$CONTEXT_OUT/community-labels.json"
LABEL_BATCH_SIZE="${LABEL_BATCH_SIZE:-30}"

DEEPSEEK_BASE_URL="${DEEPSEEK_BASE_URL:-https://api.deepseek.com}"
DEEPSEEK_MODEL="${DEEPSEEK_MODEL:-deepseek-v4-flash}"

SKILL_DIR="$HOME/.claude/skills/graphify-this-project"
LOG_DIR="/tmp/graphify-llm"
mkdir -p "$LOG_DIR"
LOG="$LOG_DIR/label-communities-$(date +%Y%m%d-%H%M%S).log"

log()  { echo "$@" | tee -a "$LOG"; }
vlog() { echo "$@" >> "$LOG"; }

# --- Secret loading ---
load_secrets() {
    if command -v op >/dev/null 2>&1; then
        if op account get 2>/dev/null | grep -q .; then
            export DEEPSEEK_API_KEY="${DEEPSEEK_API_KEY:-$(op read 'op://ClaudeCode/DeepSeek Api/password' 2>/dev/null || true)}"
        fi
    fi

    if [ -z "${DEEPSEEK_API_KEY:-}" ]; then
        log "label-communities-deepseek: DEEPSEEK_API_KEY not set and not in 1Password — cannot run."
        return 1
    fi
}

# --- Extract communities with their top nodes ---
extract_communities() {
    uv run python -c "
import json, sys
from collections import Counter, defaultdict

with open('$GRAPH_JSON') as f:
    g = json.load(f)

nodes = g.get('nodes', [])
edges = g.get('edges', g.get('links', []))

# Build node lookup
node_by_id = {n['id']: n for n in nodes}

# Build adjacency: node_id → set of neighbor node_ids
adj = defaultdict(set)
for e in edges:
    s, t = e.get('source', ''), e.get('target', '')
    if s and t:
        adj[s].add(t)
        adj[t].add(s)

# Group nodes by community
by_community = defaultdict(list)
for n in nodes:
    cid = n.get('community')
    if cid is not None:
        by_community[cid].append(n)

# For each community, select representative nodes:
# - god nodes (highest degree within community)
# - file nodes (source_file entries, important for naming)
# - a few random leaves for context
communities = []
for cid in sorted(by_community.keys(), key=lambda c: int(c) if str(c).isdigit() else 0):
    members = by_community[cid]
    if not members:
        continue

    # Degree within the community graph
    degrees = {n['id']: len(adj.get(n['id'], set()) & {m['id'] for m in members}) for n in members}
    sorted_members = sorted(members, key=lambda n: degrees.get(n['id'], 0), reverse=True)

    # Top nodes by degree (capped at 10)
    top_nodes = sorted_members[:10]
    top_info = []
    for n in top_nodes:
        info = {
            'label': n.get('label', '?'),
            'type': n.get('type', '?'),
            'file': n.get('source_file', n.get('file', '')),
            'degree': degrees.get(n['id'], 0)
        }
        top_info.append(info)

    # File-centric nodes for naming context
    file_nodes = [n for n in members if n.get('type') in ('file', 'File') or (n.get('source_file') and not n.get('source_file', '').endswith('.test'))]
    file_samples = file_nodes[:5]

    communities.append({
        'community_id': cid,
        'size': len(members),
        'top_nodes': top_info,
        'file_samples': [{'label': n.get('label', '?'), 'file': n.get('source_file', n.get('file', ''))} for n in file_samples]
    })

json.dump(communities, sys.stdout, indent=2)
"
}

# --- Build a labeling prompt for a batch of communities ---
build_prompt() {
    local batch_json="$1"
    uv run python -c "
import json, sys

communities = json.loads(sys.stdin.read())

prompt = '''You are analyzing a codebase knowledge graph. Below are communities (clusters of related code entities) discovered by the Leiden algorithm. Each community has top nodes (most connected entities) and file samples.

For each community, infer a 2-4 word lowercase-hyphenated semantic label that describes what that cluster does. Use English labels. For singletons (size 1), use the dominant entity name. For ambiguous groups, use \"mixed-utility\". For Portuguese codebases, use Portuguese labels.

Return ONLY a JSON object mapping community_id → label. Example:
{\"0\": \"order-lifecycle\", \"1\": \"webhook-ingestion\", \"2\": \"auth-middleware\"}

Communities to label:
'''

for c in communities:
    prompt += f\"\"\"
Community {c['community_id']} (size={c['size']}):
  Top nodes: {', '.join(n['label'] for n in c['top_nodes'][:8])}
  Files: {', '.join(n.get('file','?').split('/')[-1] for n in c.get('file_samples',[]) if n.get('file'))}
\"\"\"

prompt += '\nJSON response (community_id → label only):\n'
print(json.dumps({'model': 'deepseek-v4-flash', 'messages': [{'role': 'user', 'content': prompt}], 'temperature': 0.3, 'max_tokens': 4096}))
" <<<"$batch_json"
}

# --- Main ---
log "=== label-communities-deepseek $(date -Iseconds) ==="
log "  context-out: $CONTEXT_OUT"
log "  model:       $DEEPSEEK_MODEL"
log "  batch size:  $LABEL_BATCH_SIZE"

if ! load_secrets; then
    log "  ✗ secret loading failed — aborting"
    exit 1
fi

if [ ! -f "$GRAPH_JSON" ]; then
    log "  ✗ $GRAPH_JSON not found"
    exit 1
fi

log "  extracting communities from graph..."

COMMUNITIES_JSON=$(extract_communities) || {
    log "  ✗ failed to extract communities"
    exit 1
}

COMMUNITY_COUNT=$(uv run python -c "import json,sys; print(len(json.loads(sys.stdin.read())))" <<<"$COMMUNITIES_JSON")
log "  communities found: $COMMUNITY_COUNT"

if [ "$COMMUNITY_COUNT" -eq 0 ]; then
    log "  no communities to label — skipping"
    exit 0
fi

# Split into batches
BATCHES_DIR="/tmp/graphify-label-batches-$$"
mkdir -p "$BATCHES_DIR"

uv run python -c "
import json, sys, math, os

communities = json.loads(sys.stdin.read())
batch_size = $LABEL_BATCH_SIZE
num_batches = math.ceil(len(communities) / batch_size)

for i in range(num_batches):
    batch = communities[i * batch_size:(i + 1) * batch_size]
    with open(os.path.join('$BATCHES_DIR', f'batch-{i:03d}.json'), 'w') as f:
        json.dump(batch, f)
" <<<"$COMMUNITIES_JSON"

BATCH_COUNT=$(ls "$BATCHES_DIR"/batch-*.json 2>/dev/null | wc -l | tr -d ' ')
log "  batches: $BATCH_COUNT (${LABEL_BATCH_SIZE} communities each)"

# Process batches serially
ALL_LABELS="{}"
SUCCEEDED=0
FAILED_BATCHES=0

for batch_file in "$BATCHES_DIR"/batch-*.json; do
    [ -f "$batch_file" ] || continue
    BATCH_NAME=$(basename "$batch_file" .json)
    vlog "  ▶ $BATCH_NAME"

    PROMPT_JSON=$(build_prompt "$(cat "$batch_file")")

    RESPONSE=$(curl -s -X POST "$DEEPSEEK_BASE_URL/chat/completions" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $DEEPSEEK_API_KEY" \
        -d "$PROMPT_JSON" 2>&1) || true

    if [ -z "$RESPONSE" ]; then
        log "  ✗ $BATCH_NAME: empty response"
        FAILED_BATCHES=$((FAILED_BATCHES + 1))
        vlog "    (raw response empty)"
        continue
    fi

    # Extract the content from the response
    CONTENT=$(uv run python -c "
import json, sys
try:
    d = json.loads(sys.stdin.read())
    print(d['choices'][0]['message']['content'])
except Exception as e:
    print('PARSE_ERROR:' + str(e), file=sys.stderr)
" <<<"$RESPONSE" 2>/dev/null) || {
        log "  ✗ $BATCH_NAME: failed to parse API response"
        FAILED_BATCHES=$((FAILED_BATCHES + 1))
        vlog "    $(echo "$RESPONSE" | head -c 500)"
        continue
    }

    if echo "$CONTENT" | grep -q "PARSE_ERROR"; then
        log "  ✗ $BATCH_NAME: API response parse error"
        FAILED_BATCHES=$((FAILED_BATCHES + 1))
        vlog "    $CONTENT"
        continue
    fi

    # Extract the JSON object from the content (may be wrapped in backticks or text)
    BATCH_LABELS=$(uv run python -c "
import json, re, sys

text = sys.stdin.read()
# Try to find JSON object in the response
match = re.search(r'\{[^{}]*\"\d+\"[^}]*\}', text, re.DOTALL)
if match:
    try:
        parsed = json.loads(match.group())
        # Ensure keys are strings (community IDs)
        result = {str(k): str(v) for k, v in parsed.items()}
        print(json.dumps(result))
    except:
        pass
" <<<"$CONTENT" 2>/dev/null) || true

    if [ -z "$BATCH_LABELS" ] || [ "$BATCH_LABELS" = "{}" ]; then
        log "  ✗ $BATCH_NAME: could not extract valid JSON labels"
        FAILED_BATCHES=$((FAILED_BATCHES + 1))
        vlog "    content: $(echo "$CONTENT" | head -c 300)"
        continue
    fi

    # Merge into all labels
    ALL_LABELS=$(uv run python -c "
import json
all_labels = json.loads('''$ALL_LABELS''')
batch = json.loads('''$BATCH_LABELS''')
all_labels.update(batch)
print(json.dumps(all_labels, separators=(',', ':')))
")

    SUCCEEDED=$((SUCCEEDED + 1))
    BATCH_LABEL_COUNT=$(uv run python -c "import json; print(len(json.loads('''$BATCH_LABELS''')))")
    vlog "    ✓ $BATCH_NAME: $BATCH_LABEL_COUNT labels"
done

# Write community-labels.json
echo "$ALL_LABELS" > "$LABELS_JSON"
TOTAL_LABELS=$(uv run python -c "import json; print(len(json.load(open('$LABELS_JSON'))))")
log "  labels written: $TOTAL_LABELS (succeeded: $SUCCEEDED batches, failed: $FAILED_BATCHES batches)"

# Apply labels onto graph.json nodes (stamp community_label — no re-cluster)
log "  applying labels via apply-labels.py..."
APPLY_SCRIPT="$(cd "$(dirname "$0")" >/dev/null && pwd)/apply-labels.py"
if uv run python "$APPLY_SCRIPT" "$GRAPH_JSON" "$LABELS_JSON" 2>&1 | tee -a "$LOG"; then
    log "  ✓ labels applied to graph.json"
else
    log "  ⚠ apply-labels.py had non-zero exit"
fi

# Cleanup
rm -rf "$BATCHES_DIR"

log "✓ label-communities-deepseek done ($SUCCEEDED/$((SUCCEEDED + FAILED_BATCHES)) batches)"

exit 0
