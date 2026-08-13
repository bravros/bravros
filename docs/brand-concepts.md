# Brand concepts — visual metaphors for Bravros

If you want to design alternative logo variations, here are the core concepts and visual metaphors that define Bravros:

### 1. The Interconnected Graph (Workflow Traversal)
Bravros is driven by **knowledge graphs** and structural connectivity. Using tools like `graphify`, the toolkit maps AST code components, COMMUNITY structures, and directories into a logical graph database.
*   *Visual elements:* Connected nodes, networks, structural grids, branching lines, constellation formations.

### 2. The Shield / Gates (Security & Constraints)
Security is a non-negotiable pillar. The CLI acts as a validator: it enforces commit format through a git hook, preserves content into `.trash/` before anything deletes it, and requires an out-of-band token — minted in a separate terminal, refused inside an agent session — before a merge or destructive gate opens.
*   *Visual elements:* Shields, locks, keys, rings, gates, protective bounding boxes, concentric loops.

### 3. The Swarm (Multi-Agent Cooperation)
Bravros fans work out to independent subagents and merges their results back into one verdict.
`/scout` runs three lenses at once — `code-tracer`, `blast-radius-mapper`, `repro-verifier` — then a
separate pass tries to *refute* the hypothesis they agree on; `/orchestrate` dispatches plan phases
by complexity marker; `/triage-sweep` and `/batch-merge-prs` classify and verify their queues in
parallel. The shape is many streams converging on a single trunk, with an adversarial check at the
join.
*   *Visual elements:* Concentrated waves, arrows converging, hexagons, swarms, overlapping layers, gears.

### 4. The Loop / Pipeline (Automated SDLC)
The lifecycle runs in a continuous circle: `/backlog` ➔ `/recon` ➔ `/orchestrate` ➔ `/pr` ➔ `/pr-review` ➔ `/finish`.
*   *Visual elements:* Infinity symbols, dynamic circular arrows, progress trackers, linear segments wrapping into a circle.
