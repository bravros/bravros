---
name: commit
category: sdlc
description: Commit staged changes with emoji+type conventions and formatting.
core: true
---

# commit

INTENT: commit the current changes. Commit only — never push.

HARD CONSTRAINTS:
- Always `bravros commit "<emoji> <type>[(scope)]: <subject>" <files...>` — never raw
  `git add && git commit`. The verb runs the project formatter (pint / prettier / ruff /
  gofmt / cargo fmt) before committing, and the commit-msg hook enforces the format.
- Name files explicitly — never blanket-stage. Never stage `.env`, `.env.*`, credentials, or API keys.
- NEVER add AI signatures — **no `Co-Authored-By:` trailer of any kind** (not `Claude`, not `Claude Sonnet …`), no "Generated with…" line. The harness may append one by default: omit it from the message; the hook rejects it ("AI signature detected!").
- **Stale-hook error:** if the commit-msg hook rejects a message that matches the format above (e.g. `✨ feat(orders): …` → `Invalid commit message format … Required format: <emoji> <type>: <description>`), the repo's hook is stale (older than the `(scope)` template). Do NOT silently retry without the scope or hand-edit the hook — report it and run `/update-hooks`, then recommit with the original message.
- Subject ≤ 50 chars (hard 72), present tense, lowercase, why over what; detail goes in the body.
- No branch gate here — that lives in `/push` and `/ship`. Committing on `main` is normal in a direct-main repo — `bravros config get police.direct_main` prints `true` (`/git-this` personal repos); anything else (empty, an error, an older CLI reporting an unknown key) means PR-gated: branch first, then commit. `staging_branch` is never the discriminator — it never prints empty.

REPO FACT — the only accepted `<emoji> <type>` pairs:
✨ feat · 🐛 fix · 📚 docs · 💄 style · ♻️ refactor · ⚡ perf · 🧪 test · 🔧 build · 🧹 chore ·
📋 plan · 🔒 security · 🗃️ migration · 📦 deps · 🚀 deploy · 🤖 ci · 🔥 remove · 🩹 hotfix ·
🔀 merge · 🔍 debug · 🔙 revert · 🌐 i18n

Use $ARGUMENTS as context for the commit message if provided.
