# Team Execution Guide

## Core Principle

**Leader NEVER implements code.** Leader orchestrates: read plan → delegate → verify → update plan → commit. This keeps the main context lean — with 1M context, even large plans (10+ phases, 40+ tasks) complete in a single session without restarts.

## Model Selection

Assign models based on task complexity. Optimize for speed.

Treat the tiers as vendor-neutral. Map them to whatever models you have available (Claude, GPT, Codex, etc.).

| Marker | Tier | When to Use |
|--------|------|-------------|
| `[H]` | Fast/small | CRUD, styling, config, views, renaming, migrations, simple tests |
| `[S]` | Balanced | Business logic, services, API integration, complex tests, components |
| `[O]` | Strong reasoning | Architecture, multi-system coordination, deep reasoning (rare) |

The marker IS the model — `[H]` → **Haiku**, `[S]` → **Sonnet**, `[O]` → **Opus**. No exceptions. Team-lead model = highest marker in phase. Tests are `[S]` → Sonnet. **Coordinator = Opus** (main chat orchestrates). **Haiku** is the default for all `[H]` mechanical tasks, not a fallback.

## Delegation Strategy

| Phase Shape | Strategy | How |
|-------------|----------|-----|
| 2+ parallel phases with `[S]`/`[O]` | **Parallel Teams** | Spawn N team-lead subagents in ONE message — each runs its own team internally |
| 2+ parallel phases, all `[H]` | **Parallel Subagents** | Spawn all subagents in ONE message — fire and forget |
| 1 sequential phase with `[S]`/`[O]` | **Single Team** | Coordinator directly creates one team, assigns workers, manages mid-task |
| Sequential `[S]`/`[O]` + concurrent `[H]` | **Mixed dispatch** | 1 team (coordinator manages) + subagents (fire-and-forget), all dispatched in ONE message |
| Tasks within a team need handoff | **Coordinated Team** | Worker A writes handoff block → coordinator re-prompts Worker B |
| 1 phase all `[H]`, sequential | **Single Subagent** | One `Agent` call, handles tasks in order |
| ≤3 `[H]` tasks total | **Leader Direct** | No spawn — coordinator handles directly |

### Team-First Bias

**Prefer Teams over Subagents.** Teams can report back, ask questions, and receive mid-task corrections. Subagents are black boxes — fire and forget.

If a phase could be dispatched as either a Subagent or a Team, **always pick Team**. The overhead is minimal (one extra agent layer), and the benefits are significant: mid-task course correction, structured handoff, and visibility into worker progress.

The only exception: all-`[H]` phases with simple, mechanical tasks (config edits, file renames) where team overhead adds zero value.

**The constraint:** Each team-lead can manage ONE team at a time.
- Parallel Teams = N team-lead subagents running independently (NOT one coordinator managing N teams)
- Single Team = coordinator IS the team-lead, gives full attention to one team

### Why Always Delegate?

Data from plan 0116 (TikTok Shop):
- Phase 1 Direct (leader coded): 8 tasks, **13 min**, heavy context
- Phases 3+4+5a Coordinated team (3 workers): 6 tasks, **4 min**, light context
- Phase 6 Coordinated team (2 workers): 4 tasks, **6 min**, light context

Delegating everything keeps the leader at ~20% context usage vs ~80% when implementing directly.

## Delegating via Parallel Teams (independent [S]/[O] phases)

Spawn N "team-lead subagents" in ONE message — each runs independently as its own team coordinator:

1. Spawn subagent A with a full prompt: "You are the team lead for Phase 1. Create a team named `team-bifrost`, assign workers (worker-1, worker-2, etc.), implement Phase 1..."
2. Spawn subagent B with a full prompt: "You are the team lead for Phase 2. Create a team named `team-asgard`, assign workers (worker-1, worker-2, etc.), implement Phase 2..."
3. Both subagents run in parallel — each creates its own team and manages its own workers
4. Plan coordinator waits for both to report back, then commits and continues

**Key:** The plan coordinator does NOT create the teams directly. It spawns team-lead subagents that each manage their own team. One team-lead = one team.

**When to use over parallel subagents:** Any phase with `[S]`/`[O]` work benefits from a full team internally — smarter, workers can coordinate within the phase. Pure `[H]` phases stay as regular subagents.

## Delegating via Parallel Subagents (independent all-[H] phases)

Spawn all workers in ONE message — they run simultaneously with zero team overhead:

The key is: launch multiple workers concurrently and keep prompts self-contained.

For a single phase, use one worker (no overhead either).

Model selection: the marker IS the model — `[H]` → Haiku, `[S]` → Sonnet, `[O]` → Opus. Use the hardest task marker in the worker's assignment.

Include in every worker prompt:
- Specific files and logic to implement
- Relevant context from the plan
- "Run the project's linter/formatter (e.g. pint, prettier, ruff, gofmt — use whatever the stack requires) before finishing"
- "Run targeted tests for files you touched (use the project's test runner from `sdlc meta --field stack.test_runner`)"
- "NEVER add AI signatures to commits"
- "NEVER stage `.env`, `*-api-key`, or credential files"

## Coordinated Team (handoff required)

If worker-to-worker messaging exists in your tool, use it.

If it does NOT exist (common), emulate coordination in a tool-agnostic way:

1. Worker A: produce the intermediate artifact (contract/type/schema decision) and write it into the plan file under a "Handoff" heading
2. Leader: sanity-check the handoff and re-prompt Worker B with the handoff pasted in
3. Worker B: implement against the handoff and report back
4. Leader: integrate and run targeted tests

## Worker Prompt Template

Include in every worker prompt:
- Specific files and logic to implement
- Relevant context from the plan
- "Run the project's linter/formatter (e.g. pint, prettier, ruff, gofmt — use whatever the stack requires) before reporting done"
- "Run targeted tests for files you touched (use the project's test runner from `sdlc meta --field stack.test_runner`)"
- "NEVER add AI signatures to commits"
- "NEVER stage `.env`, `*-api-key`, or credential files"
- "When done, send message to leader with summary of changes + test results"

## Worker Naming Convention

Use short, memorable codenames — sequential IDs within each team:

- Single team: `worker-1`, `worker-2`, `worker-3`, ...
- Parallel teams: prefix with team codename — `bifrost-1`, `bifrost-2`, `asgard-1`, `asgard-2`, etc.

**Team codenames** (Marvel series — pick sequentially):

| Slot | Codename | Slot | Codename |
|------|----------|------|----------|
| 1st team | `bifrost` | 5th team | `ragnarok` |
| 2nd team | `asgard` | 6th team | `nexus` |
| 3rd team | `mjolnir` | 7th team | `quantum` |
| 4th team | `valhalla` | 8th team | `titan` |

So for two parallel teams: `team-bifrost` (workers: `bifrost-1`, `bifrost-2`) + `team-asgard` (workers: `asgard-1`, `asgard-2`).
Reads cleanly in logs: `bifrost-1 reported ✅`, `asgard-2 blocked on handoff`.

## Execution Performance Log

Track execution times to build evidence for team vs subagent decisions.

| Plan | Phase | Strategy | Tasks | Time | Context |
|------|-------|----------|-------|------|---------|
| 0116 TikTok | Phase 1 | Direct (leader) | 8 | 13 min | Heavy (~80%) |
| 0116 TikTok | Phases 3+4+5a | Coordinated team (3 workers) | 6 | 4 min | Light (~20%) |
| 0116 TikTok | Phase 6 | Coordinated team (2 workers) | 4 | 6 min | Light (~20%) |

Add rows after completing plans to build a real dataset over time.
