---
name: brand-generator
description: |
  **Design System & Brand Generator**: Generates complete DaisyUI + Tailwind 4 design systems from brand inspiration URLs. Creates production-ready app.css themes (light + dark), comprehensive brand guidelines, color tokens, typography scales, and live HTML preview pages.
  - MANDATORY TRIGGERS: brand, design system, theme, DaisyUI theme, Tailwind theme, brand guide, brand guidelines, color palette, app.css, design tokens
  - ALSO TRIGGER when the user: provides URLs for "inspiration" or "reference", wants to create a visual identity for a new project, mentions generating a color scheme or theme for a SaaS/app, asks to analyze competitor websites for design patterns, wants light/dark theme generation, mentions "look and feel" for their application
  - Use this skill proactively whenever the user is starting a new project and mentions design, theming, or branding — even if they don't explicitly say "brand guide". If they paste URLs and mention wanting a similar style, this skill is what they need.
---

# Brand Generator — Design System from Inspiration

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

This skill turns brand inspiration URLs into a complete, production-ready design system for DaisyUI + Tailwind 4 projects. Think of it as having a branding agency on call — it analyzes competitor/inspiration sites, extracts their design DNA, and generates a unique visual identity for your project.

## What It Produces

The skill has two modes (ask the user which they want):

**Visual Design System** (default):
- `app.css` — Production-ready Tailwind 4 + DaisyUI theme with light and dark modes
- `brandguide.md` — Comprehensive brand guidelines document
- `preview.html` — Live preview page showing all components with the theme applied
- `tokens.css` — Standalone design tokens file (colors, spacing, typography, shadows)

**Full Brand Identity** (when requested):
- Everything above, plus:
- Voice & tone guidelines
- Logo direction and usage rules
- Naming conventions and copywriting guidelines
- Photography/illustration style direction

## Workflow

### Step 1/9: Gather Inspiration URLs

Ask the user for:
1. **Inspiration URLs** (1-5 websites they admire the design of)
2. **Project name** (what's the product called?)
3. **Project type** (SaaS dashboard, marketing site, e-commerce, etc.)
4. **Mode** — Visual Design System only, or Full Brand Identity?
5. **Any preferences** — colors they love/hate, fonts they want, mood (corporate, playful, minimal, bold)

### Step 2/9: Analyze Inspiration Sites

For each URL, extract design intelligence using a two-pass approach:

**Pass 1 — Scrape with Firecrawl** (preferred) or WebFetch (fallback):
Read the page content and look for:
- CSS custom properties and color values in `<style>` tags and inline styles
- Font family declarations (`font-family`, Google Fonts links, `@font-face`)
- Spacing patterns and layout approaches
- Component patterns (buttons, cards, inputs)

**Pass 2 — Screenshot with browser tools** (if available):
Take screenshots and analyze visually:
- Overall color temperature (warm/cool/neutral)
- Typography hierarchy and weight usage
- Whitespace and breathing room
- Visual density and information architecture
- Rounded vs. sharp corners
- Shadow depth and style

Compile findings into an **Inspiration Analysis** — a structured summary of what you observed across all sites. Look for patterns (what do they all share?) and differentiators (what makes each unique?).

### Step 3/9: Generate the Color Palette

This is the creative heart of the skill. Don't just copy colors from the inspiration sites — use them as a jumping-off point to create something **original and cohesive**.

Read `references/color-system.md` for the complete color generation methodology.

Key principles:
- **Use OKLCH color space** throughout — it's perceptually uniform and works beautifully for generating harmonious palettes
- **Start with a primary color** that captures the brand's energy (derived from inspiration analysis)
- **Derive the full palette** mathematically from the primary using hue shifts, chroma adjustments, and lightness scales
- **Generate semantic colors** (success, warning, error, info) that harmonize with the brand palette
- **Create light AND dark variants** that feel cohesive, not just "inverted"
- **Verify WCAG AA contrast** for all text-on-background combinations (4.5:1 minimum)

### Step 4/9: Define Typography

Read `references/typography-system.md` for detailed guidance.

Choose fonts that:
- Reflect the brand's personality (from inspiration analysis)
- Are available via Google Fonts or system fonts (for zero-cost implementation)
- Have a good range of weights (at minimum: regular, medium, semibold, bold)
- Pair well together (display + body)

Generate a **modular type scale** using a ratio (1.25 for compact UIs, 1.333 for spacious layouts).

### Step 5/9: Generate the Design Tokens

Beyond colors and typography, define:
- **Spacing scale** — based on a base unit (typically 4px or 8px)
- **Border radius scale** — from sharp to pill-shaped
- **Shadow scale** — from subtle elevation to dramatic depth
- **Animation tokens** — standard durations and easing curves

### Step 6/9: Build the app.css

Read `templates/app-css.md` for the exact template structure.

The generated `app.css` must:
- Import Tailwind CSS 4 and DaisyUI correctly
- Define two DaisyUI themes using `@plugin "daisyui/theme"` syntax
- Include extended Tailwind 4 design tokens via `@theme`
- Set up proper light/dark switching (both `prefersdark` and `data-theme` support)
- Include base styles and any custom component overrides
- Be well-commented so developers understand every section

### Step 7/9: Write the Brand Guide

Read `templates/brandguide-template.md` for the full template.

The brand guide should read like it was written by an experienced design agency — professional, clear, and actionable. It should include:
- Color palette with hex/oklch values, usage guidance, and do's/don'ts
- Typography system with scale, pairings, and hierarchy
- Spacing and layout principles
- Component styling notes (how buttons, cards, forms should look)
- Accessibility notes (contrast ratios verified)
- Voice & tone (if Full Brand Identity mode)

### Step 8/9: Generate the Preview Page

Create a single-file HTML page that demonstrates the design system in action. This page should:
- Import the generated `app.css`
- Show a color palette grid with all brand colors
- Demonstrate typography scale and font pairings
- Show DaisyUI components styled with the theme (buttons, cards, inputs, alerts, badges, modals)
- Include a theme toggle (light/dark switch)
- Be self-contained and ready to open in a browser

Read `templates/preview-page.md` for the component showcase structure.

### Step 9/9: Deliver Everything

Save all outputs to the user's workspace folder:
```
{project-name}-design-system/
├── app.css              # Production-ready theme
├── brandguide.md        # Brand guidelines document
├── preview.html         # Interactive component preview
└── tokens.css           # Standalone design tokens
```

Present the preview.html as a link so the user can immediately see their new design system in action.

## Integration with Other Skills

This skill works well in combination with:
- **cf-pages-deploy** — Deploy the preview page to Cloudflare Pages for sharing with stakeholders
- **pptx** — Generate a brand presentation deck using the brand colors and typography
- **docx** — Create a professional brand guidelines Word document
- **pdf** — Export the brand guide as a polished PDF

When another skill would enhance the output (e.g., the user wants "a brand deck I can present"), suggest it and chain the skills together.

## Important Notes

- **Originality matters** — Never directly copy a color palette. Always transform and make it unique.
- **Accessibility is non-negotiable** — Every color combination must pass WCAG AA at minimum.
- **DaisyUI compatibility** — All theme variables must work with DaisyUI's component library out of the box.
- **Tailwind 4 syntax** — Use the new CSS-first configuration, not the legacy JS config approach.
- **OKLCH color space** — Use OKLCH for all color definitions (better perceptual uniformity than hex/HSL).
