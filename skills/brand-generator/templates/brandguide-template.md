# Brand Guide Template

When generating the brandguide.md, follow this structure. Write with a professional, agency-quality tone — clear, actionable, and confident. Replace all bracketed placeholders with generated content.

## Template Structure

```markdown
# {Project Name} — Brand Guidelines

> {One-sentence brand essence: what the product does and what it stands for.}

---

## Brand Overview

{2-3 paragraphs describing the brand identity, its personality, target audience, and the design philosophy. What inspired this design direction? What emotions should the brand evoke? What differentiates it visually from competitors?}

---

## Color System

### Primary Palette

| Role | Color | OKLCH | Hex | Usage |
|------|-------|-------|-----|-------|
| Primary | {swatch} | `oklch(...)` | `#...` | Main actions, links, key UI elements |
| Secondary | {swatch} | `oklch(...)` | `#...` | Supporting elements, secondary actions |
| Accent | {swatch} | `oklch(...)` | `#...` | Highlights, badges, decorative elements |

### Neutral & Surface Colors

| Role | Light Mode | Dark Mode | Usage |
|------|-----------|-----------|-------|
| Base 100 | `oklch(...)` | `oklch(...)` | Page background |
| Base 200 | `oklch(...)` | `oklch(...)` | Card surfaces, elevated areas |
| Base 300 | `oklch(...)` | `oklch(...)` | Borders, dividers, subtle backgrounds |
| Base Content | `oklch(...)` | `oklch(...)` | Primary text color |
| Neutral | `oklch(...)` | `oklch(...)` | Muted elements, disabled states |

### Semantic Colors

| State | Color | Usage |
|-------|-------|-------|
| Success | `oklch(...)` | Confirmations, completed actions, positive metrics |
| Warning | `oklch(...)` | Caution states, pending actions, attention needed |
| Error | `oklch(...)` | Errors, destructive actions, critical alerts |
| Info | `oklch(...)` | Informational messages, help text, tips |

### Color Usage Guidelines

**Do:**
- Use Primary for the single most important action on any screen
- Use semantic colors consistently (success = always green family, error = always red family)
- Maintain WCAG AA contrast ratios (4.5:1 for text, 3:1 for large text and UI elements)
- Use base colors for layered surfaces (100 → 200 → 300 for increasing elevation)

**Don't:**
- Use Primary for everything — it should feel special
- Mix semantic colors (don't use error red for decorative elements)
- Use pure white (#fff) or pure black (#000) — always use the base colors which have brand tinting
- Rely on color alone to convey meaning — always pair with icons or text

---

## Typography

### Font Families

| Role | Font | Weights | Usage |
|------|------|---------|-------|
| Display | {Font Name} | {weights} | Headings, hero text, page titles |
| Body | {Font Name} | {weights} | Body text, descriptions, form inputs |
| Mono | {Font Name} | {weights} | Code, technical values, data |

### Type Scale

| Token | Size | Weight | Line Height | Letter Spacing | Usage |
|-------|------|--------|-------------|---------------|-------|
| Display XL | {size} | Bold (700) | 1.1 | -0.02em | Hero sections, marketing headlines |
| Display LG | {size} | Bold (700) | 1.15 | -0.02em | Page titles |
| Heading | {size} | Semibold (600) | 1.2 | -0.01em | Section headings |
| Subheading | {size} | Medium (500) | 1.3 | 0em | Subsection headings, card titles |
| Body LG | {size} | Regular (400) | 1.6 | 0em | Lead paragraphs, featured text |
| Body | {size} | Regular (400) | 1.5 | 0em | Standard body text |
| Body SM | {size} | Regular (400) | 1.5 | 0em | Secondary text, captions |
| Caption | {size} | Medium (500) | 1.4 | 0.01em | Labels, metadata, timestamps |

### Typography Guidelines

**Do:**
- Use Display font for headings and Body font for everything else
- Maintain consistent hierarchy — don't skip heading levels
- Use font weight to create emphasis, not font size alone

**Don't:**
- Use more than 2 font families on a single screen
- Set body text smaller than 14px (0.875rem)
- Use all caps except for short labels and buttons
- Center-align body text longer than 2 lines

---

## Spacing & Layout

### Spacing Scale

Based on a {base}px base unit:

| Token | Value | Usage |
|-------|-------|-------|
| xs | {value} | Tight gaps: icon-to-text, inline elements |
| sm | {value} | Small gaps: between related items, form field padding |
| md | {value} | Medium gaps: between sections within a card |
| lg | {value} | Large gaps: between cards, section spacing |
| xl | {value} | Extra large: page section breaks |
| 2xl | {value} | Maximum: hero spacing, major section breaks |

### Layout Principles

- **Content width**: Max {width}px for reading content, {width}px for dashboards
- **Card padding**: Use `md` (inside cards) to `lg` (inside larger containers)
- **Grid gaps**: Use `md` for tight grids, `lg` for spacious layouts
- **Consistent vertical rhythm**: Use the spacing scale for all vertical margins

---

## Component Styling

### Buttons

| Variant | Class | When to Use |
|---------|-------|-------------|
| Primary | `btn btn-primary` | Main page action (1 per view) |
| Secondary | `btn btn-secondary` | Supporting actions |
| Accent | `btn btn-accent` | Special/promotional actions |
| Ghost | `btn btn-ghost` | Tertiary actions, navigation |
| Outline | `btn btn-outline` | Alternative to ghost for more visibility |

### Border Radius

| Component | Radius | CSS Variable |
|-----------|--------|-------------|
| Buttons, Inputs | {value} | `--radius-field` |
| Cards, Modals | {value} | `--radius-box` |
| Badges, Toggles | {value} | `--radius-selector` |
| Avatars | Full (pill) | `rounded-full` |

### Shadows & Elevation

| Level | Usage | CSS |
|-------|-------|-----|
| None | Flat elements, within cards | — |
| SM | Cards at rest, dropdowns | `shadow-sm` |
| MD | Cards on hover, popovers | `shadow-md` |
| LG | Modals, drawers | `shadow-lg` |

---

## Accessibility

### Contrast Ratios

All color combinations in this system have been verified against WCAG AA standards:

| Pair | Ratio | Grade |
|------|-------|-------|
| Primary on Base 100 | {ratio}:1 | {AA/AAA} |
| Primary Content on Primary | {ratio}:1 | {AA/AAA} |
| Base Content on Base 100 | {ratio}:1 | {AA/AAA} |
| Base Content on Base 200 | {ratio}:1 | {AA/AAA} |
| {additional pairs...} | | |

### Focus States

All interactive elements must have a visible focus indicator:
- Use `outline-primary` with `outline-offset-2`
- Never remove focus outlines without providing an alternative

---

## Voice & Tone (Full Brand Identity Mode)

{Include this section only if the user requested Full Brand Identity mode.}

### Brand Personality

{3-5 personality traits with descriptions. Example:}
- **{Trait 1}** — {description}
- **{Trait 2}** — {description}
- **{Trait 3}** — {description}

### Writing Principles

{Guidelines for how copy should feel. Examples:}
- Write like you're talking to a smart colleague, not a textbook
- Be direct — say what you mean in as few words as possible
- Technical terms are fine when the audience expects them; jargon is not
- Error messages should tell users what happened AND what to do about it

### Tone by Context

| Context | Tone | Example |
|---------|------|---------|
| Marketing | {tone} | "{example}" |
| Dashboard UI | {tone} | "{example}" |
| Error messages | {tone} | "{example}" |
| Success messages | {tone} | "{example}" |
| Onboarding | {tone} | "{example}" |

---

## Quick Reference

### DaisyUI Classes Cheatsheet

```html
<!-- Primary action -->
<button class="btn btn-primary">Get Started</button>

<!-- Card with elevation -->
<div class="card bg-base-100 shadow-sm border border-base-200">
  <div class="card-body">Content</div>
</div>

<!-- Alert variants -->
<div class="alert alert-info">Informational</div>
<div class="alert alert-success">Success</div>
<div class="alert alert-warning">Warning</div>
<div class="alert alert-error">Error</div>

<!-- Badge variants -->
<span class="badge badge-primary">Primary</span>
<span class="badge badge-secondary">Secondary</span>
<span class="badge badge-accent">Accent</span>

<!-- Input with label -->
<label class="form-control">
  <div class="label"><span class="label-text">Email</span></div>
  <input type="email" class="input input-bordered" />
</label>
```

### CSS Variable Reference

```css
/* Access any design token in custom CSS */
.my-component {
  color: var(--color-primary);
  background: var(--color-base-100);
  border-radius: var(--radius-box);
  font-family: var(--font-display);
  box-shadow: var(--shadow-md);
}
```
```

## Tone Guidelines for Writing the Brand Guide

- Write as a senior designer briefing a development team
- Be specific and prescriptive — "use X" not "consider using X"
- Include concrete do's and don'ts with reasoning
- Keep it scannable — tables, code blocks, and clear headers
- The guide should be usable by someone who has never seen the design system before
