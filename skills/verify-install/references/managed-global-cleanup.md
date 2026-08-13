# First-migration cleanup of the managed global CLAUDE.md (LLM-guided)

Load this when `verify.sh` reports the managed block was just **migrated**, or the
operator asks to "clean up my global CLAUDE.md".

The deterministic reconcile in `scripts/reconcile-global-claude.py` handles only the
marker-delimited block. It cannot tell *stale duplicated toolkit prose* from *genuine
personal config* in the region outside the markers — that judgment needs a model.

1. Read `~/.claude/CLAUDE.md` and `~/.claude/templates/global-CLAUDE.md`.
2. In the pre-marker (personal) region: **keep** genuinely personal content —
   language/tone preferences, personal rules, machine-specific TTS / 1Password /
   home-automation setup. **Propose removing** content that merely duplicates the
   managed block: SDLC workflow, commit format, model tiers, subagent hygiene,
   graphify, safety.
3. **Never touch anything inside the markers.** **Never delete personal content
   without showing a diff and getting confirmation.** Back up to
   `CLAUDE.md.bak.<TS>` before writing.
4. If the operator has no 1Password / home-automation setup, do **not** add
   owner-specific rules — the managed global is intentionally generic, and this is
   the exact mistake that produced the duplication being cleaned up.
