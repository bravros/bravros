---
name: verify-install
description: Health-check the Bravros SDLC install — skill drift, config, hooks, toolchain — and optionally repair it. Use on `/verify-install`, or `--auto` from a SessionStart hook.
core: true
---

# verify-install

Health-check the Bravros SDLC install — skill drift, config, hooks, toolchain — and optionally repair it.

```bash
S=~/.agent_config/skills/verify-install/scripts/verify.sh
bash $S            # report          bash $S --auto   # SessionStart: silent when healthy
bash $S --fix      # report + repair bash $S --json   # machine-readable
```

## Rule

1. Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.
