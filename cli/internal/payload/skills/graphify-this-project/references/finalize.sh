#!/usr/bin/env bash
# graphify-out/finalize.sh
# Run AFTER the semantic swarm has produced graphify-out/.graphify_chunk_*.json files.
# Validates chunks, merges with AST, builds the graph, and emits final outputs.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -f graphify-out/.graphify_python ]; then
  echo "✗ graphify-out/.graphify_python missing — run /graphify-this-project first to bootstrap." >&2
  exit 1
fi
PYTHON="$(cat graphify-out/.graphify_python)"

if [ ! -f graphify-out/.graphify_ast.json ]; then
  echo "✗ graphify-out/.graphify_ast.json missing — AST layer was not extracted." >&2
  exit 1
fi

CHUNKS=(graphify-out/.graphify_chunk_*.json)
if [ ${#CHUNKS[@]} -eq 0 ] || [ ! -e "${CHUNKS[0]}" ]; then
  echo "✗ No chunk JSONs found at graphify-out/.graphify_chunk_*.json" >&2
  echo "  Did the swarm finish writing? Expected one chunk per group from graphify-out/groups/" >&2
  exit 1
fi

echo "▸ Validating ${#CHUNKS[@]} chunk JSON file(s)..."
"$PYTHON" - <<'PY'
import glob, json, sys
from pathlib import Path

bad = []
totals = {"nodes": 0, "edges": 0, "hyperedges": 0}
for c in sorted(glob.glob('graphify-out/.graphify_chunk_*.json')):
    try:
        d = json.loads(Path(c).read_text())
    except Exception as e:
        bad.append((c, f"JSON decode failed: {e}"))
        continue
    if not isinstance(d, dict) or 'nodes' not in d or 'edges' not in d:
        bad.append((c, "missing 'nodes' or 'edges' keys"))
        continue
    totals['nodes'] += len(d.get('nodes') or [])
    totals['edges'] += len(d.get('edges') or [])
    totals['hyperedges'] += len(d.get('hyperedges') or [])

if bad:
    print(f"✗ {len(bad)} chunk(s) failed validation:", file=sys.stderr)
    for c, why in bad:
        print(f"  {c}: {why}", file=sys.stderr)
    sys.exit(1)

print(f"  ✓ All chunks valid. Totals: {totals['nodes']} nodes, {totals['edges']} edges, {totals['hyperedges']} hyperedges")
PY

echo "▸ Canonicalizing chunk IDs against source_file (rewrites worker-mangled stems)..."
"$PYTHON" - <<'PY'
import glob, json, re
from pathlib import Path
from collections import defaultdict

def canon_stem(source_file: str) -> str:
    if not source_file:
        return ''
    stem = Path(source_file).stem.lower()
    stem = re.sub(r'[^a-z0-9]+', '_', stem).strip('_')
    return stem

def canon_token(s: str) -> str:
    return re.sub(r'[^a-z0-9]+', '_', s.lower()).strip('_')

# Per-chunk canonicalization: for each node group sharing a source_file,
# infer the worker's stem prefix from the longest common prefix ending at '_',
# then rebuild id = canonical_stem + remainder.
total_remapped = 0
total_dangling = 0
total_orphans = 0
files_touched = 0
log = []

for c_path in sorted(glob.glob('graphify-out/.graphify_chunk_*.json')):
    chunk = json.loads(Path(c_path).read_text())
    nodes = chunk.get('nodes', []) or []
    edges = chunk.get('edges', []) or []
    hyperedges = chunk.get('hyperedges', []) or []

    # Group nodes by source_file
    by_file = defaultdict(list)
    for i, n in enumerate(nodes):
        by_file[n.get('source_file') or ''].append(i)

    id_map = {}        # old_id -> new_id
    chunk_remapped = 0

    for source_file, idxs in by_file.items():
        if not source_file:
            continue
        target_stem = canon_stem(source_file)
        if not target_stem:
            continue
        ids_for_file = [nodes[i].get('id', '') for i in idxs]

        # Already correct? Keep as-is
        all_good = all(nid == target_stem or nid.startswith(target_stem + '_') for nid in ids_for_file)
        if all_good:
            continue

        # Find the worker's stem: longest prefix ending at '_' shared by ALL ids,
        # OR the entire common prefix if it equals every id (1-node case).
        if len(ids_for_file) == 1:
            nid = ids_for_file[0]
            # Single node: assume worker_stem ends at first '_'
            worker_stem = nid.split('_', 1)[0] if '_' in nid else nid
        else:
            # Common prefix across all ids
            cp = ids_for_file[0]
            for other in ids_for_file[1:]:
                while not other.startswith(cp):
                    cp = cp[:-1]
                    if not cp:
                        break
                if not cp:
                    break
            # Trim back to last '_' (so we don't slice through entity)
            if '_' in cp:
                worker_stem = cp.rstrip('_').rsplit('_', 0)[0] if cp.endswith('_') else cp.rsplit('_', 1)[0]
                # If the trimmed stem is empty, fall back to first segment
                if not worker_stem:
                    worker_stem = ids_for_file[0].split('_', 1)[0]
            else:
                worker_stem = cp

        # Rewrite each id in the file
        for i in idxs:
            old_id = nodes[i].get('id', '')
            if old_id == target_stem or old_id.startswith(target_stem + '_'):
                continue
            # Strip the worker_stem prefix (and trailing '_'), add canonical stem
            if worker_stem and old_id.startswith(worker_stem):
                rest = old_id[len(worker_stem):]
                if rest.startswith('_'):
                    rest = rest[1:]
            else:
                # Fallback: strip up to first '_'
                rest = old_id.split('_', 1)[1] if '_' in old_id else ''
            new_id = f'{target_stem}_{rest}' if rest else target_stem
            new_id = canon_token(new_id)  # safety: re-normalize
            if new_id and new_id != old_id:
                id_map[old_id] = new_id
                nodes[i]['id'] = new_id
                chunk_remapped += 1

    # Apply id_map to edges and hyperedges
    if id_map:
        for e in edges:
            e['source'] = id_map.get(e.get('source'), e.get('source'))
            e['target'] = id_map.get(e.get('target'), e.get('target'))
        for h in hyperedges:
            h['nodes'] = [id_map.get(x, x) for x in (h.get('nodes') or [])]

    # Drop dangling edges (refs to nodes not in this chunk after canonicalization).
    node_ids = {n.get('id') for n in nodes}
    kept_edges = []
    chunk_dangling = 0
    for e in edges:
        s, t = e.get('source'), e.get('target')
        if s in node_ids and t in node_ids:
            kept_edges.append(e)
        else:
            chunk_dangling += 1

    # Filter dangling refs out of hyperedges (drop missing nodes; remove hyperedge if <2 left)
    kept_hyperedges = []
    chunk_orphans = 0
    for h in hyperedges:
        present = [x for x in (h.get('nodes') or []) if x in node_ids]
        if len(present) >= 2:
            chunk_orphans += len(h.get('nodes') or []) - len(present)
            h['nodes'] = present
            kept_hyperedges.append(h)

    if chunk_remapped or chunk_dangling or chunk_orphans:
        files_touched += 1
        log.append(f"  {Path(c_path).name}: remapped {chunk_remapped} ids, dropped {chunk_dangling} dangling edge(s), pruned {chunk_orphans} hyperedge orphan(s)")

    chunk['nodes'] = nodes
    chunk['edges'] = kept_edges
    chunk['hyperedges'] = kept_hyperedges
    Path(c_path).write_text(json.dumps(chunk, indent=2))
    total_remapped += chunk_remapped
    total_dangling += chunk_dangling
    total_orphans += chunk_orphans

if files_touched == 0:
    print("  ✓ All chunks already canonical — no rewrites needed.")
else:
    for line in log:
        print(line)
    print(f"  ✓ Canonicalized {files_touched} chunk(s): {total_remapped} ids remapped, {total_dangling} dangling edges dropped, {total_orphans} hyperedge orphans pruned")
PY

echo "▸ Merging swarm chunks into .graphify_semantic_new.json..."
"$PYTHON" - <<'PY'
import glob, json
from pathlib import Path

chunks = sorted(glob.glob('graphify-out/.graphify_chunk_*.json'))
all_nodes, all_edges, all_hyperedges = [], [], []
total_in, total_out = 0, 0
for c in chunks:
    d = json.loads(Path(c).read_text())
    all_nodes += d.get('nodes', []) or []
    all_edges += d.get('edges', []) or []
    all_hyperedges += d.get('hyperedges', []) or []
    total_in += d.get('input_tokens', 0) or 0
    total_out += d.get('output_tokens', 0) or 0

Path('graphify-out/.graphify_semantic_new.json').write_text(json.dumps({
    'nodes': all_nodes, 'edges': all_edges, 'hyperedges': all_hyperedges,
    'input_tokens': total_in, 'output_tokens': total_out,
}, indent=2))
print(f"  ✓ Merged {len(chunks)} chunks: {total_in:,} in / {total_out:,} out tokens")
PY

echo "▸ Saving to graphify semantic cache..."
"$PYTHON" - <<'PY'
import json
from graphify.cache import save_semantic_cache
from pathlib import Path

new = json.loads(Path('graphify-out/.graphify_semantic_new.json').read_text())
saved = save_semantic_cache(new.get('nodes', []), new.get('edges', []), new.get('hyperedges', []))
print(f"  ✓ Cached {saved} files for future --update runs")
PY

echo "▸ Building unified semantic JSON..."
cp graphify-out/.graphify_semantic_new.json graphify-out/.graphify_semantic.json

echo "▸ Merging AST + semantic into final extraction..."
"$PYTHON" - <<'PY'
import json
from pathlib import Path

ast = json.loads(Path('graphify-out/.graphify_ast.json').read_text())
sem = json.loads(Path('graphify-out/.graphify_semantic.json').read_text())

seen = {n['id'] for n in ast['nodes']}
merged_nodes = list(ast['nodes'])
for n in sem['nodes']:
    if n['id'] not in seen:
        merged_nodes.append(n)
        seen.add(n['id'])

merged = {
    'nodes': merged_nodes,
    'edges': ast['edges'] + sem['edges'],
    'hyperedges': sem.get('hyperedges', []),
    'input_tokens': sem.get('input_tokens', 0),
    'output_tokens': sem.get('output_tokens', 0),
}
Path('graphify-out/.graphify_extract.json').write_text(json.dumps(merged, indent=2))
print(f"  ✓ Merged: {len(merged_nodes)} nodes, {len(merged['edges'])} edges ({len(ast['nodes'])} AST + {len(sem['nodes'])} semantic)")
PY

echo "▸ Building graph, clustering, generating report..."
"$PYTHON" - <<'PY'
import json
from pathlib import Path
from graphify.build import build_from_json
from graphify.cluster import cluster, score_all
from graphify.analyze import god_nodes, surprising_connections, suggest_questions
from graphify.report import generate
from graphify.export import to_json

extraction = json.loads(Path('graphify-out/.graphify_extract.json').read_text())
detection = json.loads(Path('graphify-out/.graphify_detect.json').read_text())

G = build_from_json(extraction)
if G.number_of_nodes() == 0:
    raise SystemExit("ERROR: graph is empty.")

communities = cluster(G)
cohesion = score_all(G, communities)
tokens = {'input': extraction.get('input_tokens', 0), 'output': extraction.get('output_tokens', 0)}
gods = god_nodes(G)
surprises = surprising_connections(G, communities)
labels = {cid: f'Community {cid}' for cid in communities}
questions = suggest_questions(G, communities, labels)

report = generate(G, communities, cohesion, labels, gods, surprises, detection, tokens, '.', suggested_questions=questions)
Path('graphify-out/GRAPH_REPORT.md').write_text(report)
to_json(G, communities, 'graphify-out/graph.json')

analysis = {
    'communities': {str(k): v for k, v in communities.items()},
    'cohesion': {str(k): v for k, v in cohesion.items()},
    'gods': gods,
    'surprises': surprises,
    'questions': questions,
}
Path('graphify-out/.graphify_analysis.json').write_text(json.dumps(analysis, indent=2))
print(f"  ✓ {G.number_of_nodes()} nodes, {G.number_of_edges()} edges, {len(communities)} communities")
print()
print("▸ Next: label communities (/graphify-this-project Step 6) and generate HTML viz with `graphify export html`.")
PY

echo
echo "✓ finalize.sh complete."
echo
echo "Outputs:"
echo "  graphify-out/graph.json            — raw graph data"
echo "  graphify-out/GRAPH_REPORT.md       — audit report (community labels still placeholder)"
echo "  graphify-out/.graphify_analysis.json — for the labeling step"
echo
echo "To finish:"
echo "  1. Re-invoke /graphify-this-project (or ask Claude) to label communities + regenerate report"
echo "  2. \`graphify export html\` to produce the interactive viz"
