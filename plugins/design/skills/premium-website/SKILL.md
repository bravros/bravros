---
name: premium-website
description: "Use on /premium-website or when a UI looks generic and needs anti-slop polish — premium typography, color calibration, motion. Adapts to the house stack (Inertia + React + shadcn + Vite+); also covers Next.js."
---

# premium-website — anti-slop design system router

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

Overrides default LLM design biases (Inter font, purple gradients, centered 3-card layouts, neon glows, fake "John Doe" data) with a curated system.

## House-stack adapter (apply before any pack)

Packs were written for Next.js/RSC. New apps use the private Laravel starter kit — see `~/.agent_config/CLAUDE.md` § New project stack: Laravel + Inertia + React + TypeScript + Tailwind 4 + shadcn/ui (vendored) + Vite+ (`vp`). Livewire products stay Livewire (Blade/Tailwind; no React). Translate pack advice:
- Ignore RSC, `"use client"`, `next/*` imports and Next-only APIs; use Inertia pages and plain React components.
- Motion: CSS (transitions, keyframes, `@starting-style`, scroll-driven animations) first; at most ONE already-approved animation lib, and only if CSS cannot do it. Never add Framer Motion or similar by default.
- No extra UI kits on top of shadcn; edit the vendored shadcn components instead of installing new libraries.
- Never install packages unasked; propose first.

| Pack | File | When to read |
|---|---|---|
| **taste** (default) | `references/taste.md` | New React/Next.js builds — dials, creative arsenal, bento paradigm |
| **redesign** | `references/redesign-skill.md` | Existing project needing a design audit/upgrade (100+ checks) |
| **soft** | `references/soft-skill.md` | Expensive agency-tier look — Apple/Linear aesthetic |
| **output** | `references/output-skill.md` | AI being lazy — placeholders, half-finished code |
| **minimalist** | `references/minimalist-skill.md` | Clean editorial — monochrome, crisp borders, Notion/Linear |
| **brutalist** | `references/brutalist-skill.md` | Raw mechanical — Swiss print + CRT terminal (beta) |
| **stitch** | `references/stitch-skill.md` | Google Stitch semantic rules + DESIGN.md export |
