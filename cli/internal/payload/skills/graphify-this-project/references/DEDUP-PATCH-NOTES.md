# Graphify dedup.py — afterpay patch notes (RESOLVED upstream)

**Status:** upstream merged the fix in graphifyy 0.9 (#1504) — `dedup.py` now keys on `source_file` throughout. `apply-dedup-fix.sh` gates the vendored patch off on 0.9.x (applying the v0.7.10 copy would revert the new node-ID scheme). This file is kept as the diagnostic record; the framework-verb limitation at the bottom is still live and is what `strip-framework-verbs.py` exists for.

**Patched against:** graphify v0.7.10 (May 7 2026 release)
**Originally diagnosed:** afterpay project, 2026-05-09 session
**Upstream:** https://github.com/safishamsi/graphify

## The bug

`dedup.py` Pass 1 (`exact normalization`) keys on `_norm(label)` only, ignoring source file. Generic method labels collapse across all classes:

```python
# upstream (broken):
norm_to_nodes: dict[str, list[dict]] = defaultdict(list)
for node in unique_nodes:
    key = _norm(node.get("label", node.get("id", "")))
    if key:
        norm_to_nodes[key].append(node)
```

Effect on a Laravel codebase:
- Every class's `__construct()` collapses into ONE node with degree 60+
- Every Job/Listener/Command's `handle()` → ONE node, degree 80+
- Every Eloquent model's `casts()` → ONE node, degree 30+
- Every ServiceProvider's `boot()` → ONE node, degree 20+

Result: top god nodes are mostly false hubs, not real architecture. Community structure is distorted because spurious cross-cluster edges flow through these collapsed pseudo-nodes.

## The fix (2 lines)

Key Pass 1 by `(label, source_file)` instead of label alone:

```python
# patched:
norm_to_nodes: dict[tuple[str, str], list[dict]] = defaultdict(list)
for node in unique_nodes:
    label_key = _norm(node.get("label", node.get("id", "")))
    src_key = node.get("source_file", "") or ""
    if label_key:
        norm_to_nodes[(label_key, src_key)].append(node)
```

Why `source_file` is the right disambiguator:
- PSR-4 (PHP) and most modern stack conventions: one class per file
- Method-level node labels are scoped to their owning class, which lives in one file
- Exact matches on `(label, source_file)` correspond to "the same method symbol within one file" — the real dedup intent

Pass 2 (MinHash/LSH/Jaro-Winkler) is unaffected — it operates on high-entropy labels only, and generic names like `__construct` fail the entropy threshold so they never reach Pass 2 anyway.

## Verified outcomes (afterpay project)

| Metric | Before patch | After patch |
|---|---:|---:|
| Exact merges | 612 (over-aggressive) | 3 (correct) |
| Total nodes | 1640 | **2272** (+632 distinct) |
| Communities | 521 (warped) | **730** (tightened) |
| Top god node | `.handle()` (88, fake) | `User` (35, real) |
| Distinct `__construct` | 1 | 64 |
| Distinct `.handle()` | 1 | 42 |
| Distinct `.casts()` | 1 | 38 |

## Known limitation (different bug, not addressed by this patch)

The graphify AST extractor creates a single conceptual node for **JS/template framework verbs** (Livewire `$set`, `$dispatch`, Alpine `x-data`, etc.) regardless of source. The `source_file` of that node is whichever file graphify happened to see it in first. Edges from many files point at this single node.

Filed for separate investigation. Possible fixes:
1. Stop-list at extract time (skip emitting nodes for `$set`, `$dispatch`, `wire:*`, `x-*`)
2. Post-process filter to strip these nodes before HTML render

This patch does NOT address that bug. Method-collapse and framework-verb-collapse are independent issues.
