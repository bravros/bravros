# Repo inspection — scope and budget

Read-only throughout. Prefer `rg --files` for discovery, excluding dependency folders, build output, VCS metadata, and likely secret files.

## Size tiers

| Repo | Approach |
|---|---|
| **Small** (< ~100 source/config files) | Inspect README/docs, manifests, entry points, core domain modules, tests, deploy config directly. |
| **Medium** (~100–500) | Map directories and manifests first, then sample app boundaries, route/API surfaces, data models, tests, and whatever the question touches. |
| **Large** (~500–2,000) | Docs, manifests, architecture notes, then identify subsystems and review targeted slices only. Do not summarize every subsystem. |
| **Huge / monorepo** | Ask which app, package, or service. If the user cannot narrow it, produce a shallow map and recommend the most useful target for deeper review. |

A broad request gets **one bounded first pass**: map the tree, read main docs and manifests, check CI/test config, sample the two or three subsystems most relevant to the question. Then either deliver a scoped assessment or ask where to go deeper — never let a broad review silently become an exhaustive audit.

## High-signal evidence

README/docs/ADRs · manifests and lock files · entry points (`main.*`, `index.*`, `app.*`, `server.*`, `cli.*`) · route/controller/API definitions · domain and service modules · data models, schemas, migrations · auth, permissions, secrets handling, validation · tests, fixtures, CI workflows, lint/typecheck config · Docker/compose/infra/platform config.

Sampling is purposeful; every finding cites the file or command behind it.

## Inspection scope note (medium repos and up)

State: what was mapped · what was inspected deeply · what was sampled · what was intentionally skipped · which findings are high-confidence versus provisional.

## Secrets

If `.env*`, `*.pem`, `*.key`, `id_rsa`, `credentials.json`, `secrets.*`, token files, production dumps, or local auth/session stores turn up: report only that they exist and recommend secure handling. Do not read, print, or summarize their contents.

## Degraded inputs

- **No accessible files** — ask for a path, archive, GitHub URL, or a short project description.
- **Idea only** — pre-build mode on stated assumptions; name the questions that would change the recommendation.
- **GitHub URL only** — public README, file tree, manifests, key files via browsing or a read-only clone. Never assume private access.
- **Tiny or empty project** — framing, stack choice, setup, structure, first useful vertical slice.
- **Non-code project** — review organization, conventions, automation, data quality, docs, maintainability instead of code architecture.
- **External research blocked** — say so, then proceed on local evidence and engineering judgment alone.
