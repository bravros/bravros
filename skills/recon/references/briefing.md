# recon — mechanics that do not fit in SKILL.md

`SKILL.md` carries the method. This file carries the mechanics: identity reservation, event
recording, and the failure modes worth naming. **Nothing here restates SKILL.md** — when the two
appear to disagree, SKILL.md wins and this file is the bug.

Folder contract: [`dossier-template.md`](dossier-template.md).

## Reserving identity

`uv run scripts/planning-events/fold.py` prints the live status table; a subject already covering
this territory → surface it and ask before writing a near-duplicate.

IDs must not collide across worktrees and concurrent branches, so reserve on `homolog` first:

1. `git fetch origin homolog`
2. switch to `homolog` locally
3. `PLAN_ID=$(bravros nextid reserve plan --slug "$SLUG")`
4. `bravros commit "📋 plan: reserve $PLAN_ID $SLUG" .planning/P-*` then `git push origin homolog`
5. switch back to the branch you started on and `git merge origin/homolog`

Started on `homolog` already → steps 2 and 5 are no-ops; reserve in place. Read the staging branch
from `bravros config get staging_branch` — it is `homolog` in the flow above only by default.

**Abort before the folder exists → `bravros nextid release "$PLAN_ID"`.** The reservation is a
placeholder file plus a commit; leaving it behind burns an id and shows up as a phantom plan in the
fold table. Release is idempotent — safe to run when unsure whether the folder was written.

## Branch discipline

Recon writes `.planning/` only, and `.planning/` bookkeeping is the one thing allowed to land on the
staging branch — that is why the reservation above commits there. Everything else about branches is
**not recon's job**:

- **Never `git checkout -b`, never `git worktree add`** unless `--worktree` was passed. The operator
  runs several sessions in one checkout; a branch switch here moves their working tree under them
  (operator, 2026-08-20: "dont start implementing or create branch because we have other sessions
  running here okay? so document all so i can create a worktree just to work on this").
- The hand-off names the branch `/orchestrate` will create — `feature/p-NNNN-<slug>` (change) or
  `fix/p-NNNN-<slug>` (defect), cut from `origin/<staging>` — so the operator can pre-create a
  worktree for it. Naming is not creating.
- `--worktree` is the only path that creates anything, and it creates the worktree + branch together
  via [`worktree-extension.md`](worktree-extension.md), never a bare branch in this checkout.

## Recording state

Two appends — the events ARE the state change — then one commit:

```bash
E() { echo '{"ts":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","id":"e_'"$(date +%s)_$RANDOM"'","kind":"'"$1"'","subject":"'"$2"'"'"$3"',"by":"agent:recon"}' >> .planning/events.jsonl; }
E created  "$PLAN_ID" ""
E reviewed "$PLAN_ID" ',"verdict":"approved","note":"<what this wave established>"'
E promoted "B-NNNN"   ""        # only for /recon B-NNNN — the B- file stays put
bravros commit "📋 plan: add P-NNNN <slug>" .planning/
```

A later wave that changes conclusions appends another `reviewed` with a `note:` saying what moved.
Never rewrite an earlier event.

## Failure modes this skill exists to prevent

- **A confident wrong premise.** The most expensive recon failure is not a missing finding, it is a
  wrong one stated as fact and then built on. Two real cases: a justification of the form "correct by
  construction" that was never executed, and a locked off-by-one that would have frozen a page
  permanently. Both survived self-review and died within minutes of an outside reader. Hence the
  mandatory `Confidence:` tag and `Falsifier:` line, and hence a `LOCKED` decision resting on `READ`
  getting fresh eyes.
- **Editing the README in place across waves.** Findings arrive over hours. A README rewritten each
  wave ends up self-contradictory — stating one artefact count in the header and another in the index,
  demanding a file that a later supersession deleted the premise for. Keep the README short and
  append waves to `log.md`.
- **Interpreting the evidence in the evidence index.** `01-evidence.md` records what an artefact
  *shows*. The moment it records what you *conclude*, the raw observation is gone and cannot be
  re-read when the conclusion turns out wrong.
- **A tool that was unavailable, silently.** If the browser extension was not connected, the log was
  not readable, the DB was not reachable — say so in the file, in a "method note". A finding that
  looks first-hand but was read off shipped source is a different grade of evidence and must say so.
- **Planning the execution.** Ordering, grouping and tier are `/orchestrate`'s, made with the whole
  picture. Recon-authored ordering is re-derived away at best; at worst the orchestrator adopts it and
  ships the slowest safe schedule. If you catch yourself writing "ordered after", stop.

## Attachments

Copy every artefact into `evidence/` — do not leave it in a cache directory that will be swept.
Number in arrival order and never renumber. Video: extract frames (`ffmpeg -i <in> -vf fps=1 …`) and
keep the frames you cite, not just the source. Record the original path in `01-evidence.md` so the
provenance survives.

## Autonomous mode

Autonomous lock (`.planning/.auto-*-lock`) or `--auto`: skip every prompt, print
`STATUS: dossier-ready. NEXT: orchestrate`, and return. Interviews and clarifying questions are
disabled — write the open question into the README's `## Open questions` instead.

## Announce

Per the operator's global rule, always the wrapper — never bare `bravros ha say`:

```bash
bash ~/.agent_config/scripts/announce.sh --force "Dossiê <NUM> documentado e revisado, pronto para orquestração. Ramo <fragmento>, projeto <repo>." studio || true
```

One sentence, Brazilian Portuguese, ~20 words, ending with its origin. The wrapper prepends the
opener and silences its own stdout.
