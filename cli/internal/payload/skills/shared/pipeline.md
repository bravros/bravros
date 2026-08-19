# Shared Pipeline Architecture

Core SDLC stage sequence shared by `/auto-pr` (including `--worktree`).

## Pipeline Stages

| # | Stage | Skill Delegated | What it Does |
|---|-------|-----------------|--------------|
| 1 | **Plan** | `/plan` | Create a `.planning/P-NNNN-<slug>/` dossier folder AND review it inline — phases, `[H]/[S]/[O]` tier markers, `## Acceptance` — ready for `/orchestrate` with zero translation |
| 2 | **Execute** | `/orchestrate` | Dispatch subagents by model tier per phase, verify diffs, commit per phase |
| 3 | **PR** | `/pr` | Create and push PR to base branch |
| 4 | **Review Loop** | `/address-pr` (async) | Poll for review feedback, dispatch fixes, loop max 3x |
| 5 | **Report** | (coordinator) | Post final report comment, output summary |

`/plan-review`, `/plan-approved`, and `/plan-check` were retired — `/plan` now reviews its own
output in the same run (no second skill, no second session), and `/orchestrate` folds
dispatch, diff verification, and acceptance-checking into one continuous pass.

## Entry Point Detection

Resume by reading the dossier folder under `.planning/` (its events log / folder-suffix
state), not by re-planning:

| Condition | Start Stage |
|-----------|-------------|
| No dossier folder exists | **Stage 1** — /plan |
| Dossier folder exists, not yet executed | **Stage 2** — /orchestrate (resume from last incomplete phase via the task list) |
| Dossier folder suffixed `-complete` / carries a `SHIPPED.md` | **Stage 3** — /pr (create only if no open PR on branch) |
| `--from <stage>` flag provided | Jump directly to that stage |

## Git Integration

- Feature branches: `feat/<description-slug>` (created by `/plan`; `--worktree` wraps it
  in `git worktree add` — see `worktree-setup.md`)
- Base branch: `homolog` if it exists, else `main`
- Each merge is a separate PR — never direct commits to `main`
- `.planning/` tracks plan state

## Failure Posture

- Autonomous runs continue to the next stage on a recoverable stage failure and note the
  issue; interactive runs ask the user.
- Catastrophic failure (merge conflict, test infra down): commit what exists **with an
  explicit file list — never `git add .`**, create the PR with a failure note, and
  proceed to the final report. A partial PR is better than no PR.

## References

- **mode-autonomous.md** — Quality Sweep, Green Gate, review loop (autonomous rules)
- **worktree-setup.md** — worktree creation and cleanup (`--worktree`)
- **dispatch.md** — worker prompt template, model selection, commit hygiene
