---
name: premium-website
description: "Premium frontend design system based on Leonxlnx/taste-skill — eliminates generic AI slop from generated UIs. Use this skill whenever building React/Next.js frontends that need to look premium, modern, and hand-crafted rather than AI-generated. Triggers on: /premium-website, premium UI, anti-slop design, high-end frontend, agency-quality interface, awwwards-tier, bento grid, magnetic buttons, parallax cards, taste-skill, or any request where the user wants their frontend to not look like AI made it."
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

# Premium Website — Anti-Slop Frontend Design

Apply the [taste-skill](https://github.com/Leonxlnx/taste-skill) design system by Leonxlnx to eliminate generic AI patterns from frontend code.

## What It Does

- Overrides default LLM biases that produce boring, cookie-cutter UIs
- Enforces premium typography, color calibration, layout diversity, and motion choreography
- Bans common AI design tells: Inter font, purple gradients, centered 3-card layouts, neon glows, fake "John Doe" data

## Bundled Sub-Skills

All 7 skills from the repo are bundled locally in `references/`. Read the relevant one based on context:

| Skill | Reference File | When to Read |
|---|---|---|
| **taste-skill** | `references/taste-skill.md` | Default — new React/Next.js frontend builds. The main design system with dials, rules, creative arsenal, and bento paradigm. |
| **redesign-skill** | `references/redesign-skill.md` | User has an **existing** project and wants to audit/upgrade its design. 100+ checks across typography, color, layout, interactivity, content, components, iconography, code quality. |
| **soft-skill** | `references/soft-skill.md` | User wants an expensive, soft, agency-tier look. Premium fonts, whitespace, depth, smooth spring animations. Think Apple/Linear aesthetic. |
| **output-skill** | `references/output-skill.md` | AI is being lazy — generating placeholders, skipping code blocks, half-finishing outputs. Read this to enforce complete output. |
| **minimalist-skill** | `references/minimalist-skill.md` | Clean, editorial-style interfaces. Monochrome, crisp borders, inspired by Notion/Linear. |
| **brutalist-skill** | `references/brutalist-skill.md` | Raw mechanical interfaces — Swiss typographic print + CRT terminal aesthetics. (Beta) |
| **stitch-skill** | `references/stitch-skill.md` | Google Stitch-compatible semantic design rules. Includes DESIGN.md export format. |

**How to use**: When this skill triggers, read the summary below for quick rules. For full implementation, read the appropriate reference file based on what the user is building. You can combine multiple — e.g., taste-skill + output-skill to get premium design AND complete code output.

## Dev Server Preview

NEVER launch the dev server via run_in_background — it will timeout.
Use AskUserQuestion to ask the user to run it in a separate terminal:
  "Run `npx vite --host` in a separate terminal for live preview"
The user prefers running dev servers in their own terminal, not managed by Claude.

## Installation (for other tools)

```bash
npx skills add https://github.com/Leonxlnx/taste-skill
```

Works with Cursor, Antigravity, Claude Code, Codex, Windsurf, Copilot, etc.

## Core Design Dials

The taste-skill uses three tunable settings (1-10 scale):

- **DESIGN_VARIANCE** (default: 8) — Layout experimentation. Low = symmetrical/centered. High = asymmetric/masonry.
- **MOTION_INTENSITY** (default: 6) — Animation level. Low = hover states only. High = scroll-triggered choreography.
- **VISUAL_DENSITY** (default: 4) — Content density. Low = airy/luxury. High = packed dashboards.

Adapt these dynamically based on what the user is building.

## Key Rules Summary

### Typography
- Ban Inter, Roboto, Arial — use `Geist`, `Outfit`, `Cabinet Grotesk`, or `Satoshi`
- Display: `text-4xl md:text-6xl tracking-tighter leading-none`
- Body: `text-base text-gray-600 leading-relaxed max-w-[65ch]`
- Serif fonts banned for dashboard/software UIs

### Color
- Max 1 accent color, saturation < 80%
- No purple/blue "AI gradient" aesthetic
- No pure `#000000` — use off-black (Zinc-950, Charcoal)
- Tint shadows to match background hue

### Layout
- No centered heroes when variance > 4 — use split screen, asymmetric whitespace
- No 3-column equal card rows — use zig-zag, asymmetric grid, or horizontal scroll
- Use `min-h-[100dvh]` never `h-screen` (iOS Safari bug)
- Use CSS Grid over flexbox percentage math

### Motion
- Spring physics: `type: "spring", stiffness: 100, damping: 20`
- Never animate `top`, `left`, `width`, `height` — only `transform` and `opacity`
- Staggered reveals for lists/grids, not instant mounting
- Use `useMotionValue`/`useTransform` for continuous animations (not useState)

### Content (Anti-Slop)
- No "John Doe", "Acme Corp", "SmartFlow" — invent realistic names
- No round numbers (`99.99%`, `50%`) — use organic data (`47.2%`)
- No AI cliches: "Elevate", "Seamless", "Unleash", "Next-Gen"
- No emojis in code or markup — use Phosphor or Radix icons

### Performance
- Noise/grain filters on fixed `pointer-events-none` elements only
- `backdrop-blur` only on fixed/sticky elements
- Isolate perpetual animations in memoized client components

## Example: Before vs After

**Before (generic AI output):**
```jsx
<div className="flex justify-center items-center h-screen bg-purple-600">
  <h1 className="text-3xl font-bold text-white">Welcome to Acme</h1>
  <p className="text-white">Elevate your workflow seamlessly</p>
</div>
```

**After (with taste-skill applied):**
```jsx
<section className="min-h-[100dvh] grid grid-cols-1 md:grid-cols-2 gap-0 bg-zinc-950">
  <div className="flex flex-col justify-center px-8 md:px-16 py-24">
    <span className="text-emerald-400 text-sm font-mono tracking-widest uppercase mb-6">
      Meridian Labs
    </span>
    <h1 className="font-display text-4xl md:text-6xl tracking-tighter leading-none text-zinc-50 mb-6">
      Ship faster without<br />the spreadsheet chaos
    </h1>
    <p className="text-zinc-400 text-base leading-relaxed max-w-[52ch]">
      One command replaces the 14-step onboarding doc your team ignores anyway.
    </p>
  </div>
  <div className="relative overflow-hidden">
    {/* High-quality visual asset with subtle fade */}
  </div>
</section>
```
