# Typography System Reference

## Font Selection Strategy

### From Inspiration Analysis

When analyzing inspiration sites, capture:
- **Display/heading font** — what typeface do they use for headings? Is it serif, sans-serif, slab, geometric?
- **Body font** — what's the primary reading font? How legible is it at small sizes?
- **Weight distribution** — do they favor light/thin weights (elegant) or bold/heavy (assertive)?
- **Character** — round and friendly? Sharp and technical? Classic and trustworthy?

### Font Pairing Principles

Great pairings create contrast while maintaining harmony:

**High contrast pairings** (different font categories):
- Serif display + Sans-serif body (classic, editorial)
- Geometric sans display + Humanist sans body (modern, approachable)
- Slab serif display + Neo-grotesque body (bold, technical)

**Low contrast pairings** (same category, different personality):
- Two sans-serifs with different proportions (e.g., condensed display + regular body)
- Same family, different weights (e.g., Black for display, Regular for body)

### Recommended Google Fonts by Personality

**Modern/Tech SaaS:**
- Display: Inter, Outfit, Satoshi, Space Grotesk, Manrope, Plus Jakarta Sans
- Body: Inter, DM Sans, Nunito Sans, Source Sans 3

**Bold/Consumer:**
- Display: Clash Display, Cabinet Grotesk, Sora, Poppins
- Body: DM Sans, Nunito Sans, Public Sans

**Elegant/Premium:**
- Display: Playfair Display, Cormorant, Libre Baskerville
- Body: Inter, Lato, Source Sans 3

**Playful/Creative:**
- Display: Bricolage Grotesque, Fraunces, Rethink Sans
- Body: Nunito, Quicksand, Rubik

**Corporate/Trust:**
- Display: IBM Plex Sans, Roboto, Noto Sans
- Body: IBM Plex Sans, Roboto, Open Sans

## Modular Type Scale

Generate heading and body text sizes using a consistent ratio:

### Scale Ratios
```
1.200 (Minor Third)    — compact, dashboard-friendly
1.250 (Major Third)    — balanced, most common for SaaS
1.333 (Perfect Fourth)  — spacious, marketing/editorial
1.500 (Perfect Fifth)   — dramatic, hero sections
1.618 (Golden Ratio)    — classic proportion
```

### Scale Generation

Given base = 16px (1rem) and ratio = 1.25:

```
text-xs:    base / ratio² = 10.24px → 0.64rem
text-sm:    base / ratio  = 12.80px → 0.80rem
text-base:  base          = 16.00px → 1.00rem
text-lg:    base × ratio  = 20.00px → 1.25rem
text-xl:    base × ratio² = 25.00px → 1.563rem
text-2xl:   base × ratio³ = 31.25px → 1.953rem
text-3xl:   base × ratio⁴ = 39.06px → 2.441rem
text-4xl:   base × ratio⁵ = 48.83px → 3.052rem
text-5xl:   base × ratio⁶ = 61.04px → 3.815rem
```

### Line Heights

```
Headings:  1.1 - 1.3  (tight, creates visual impact)
Body text: 1.5 - 1.7  (comfortable reading)
Small text: 1.4 - 1.6 (slightly tighter than body)
UI labels: 1.2 - 1.4  (compact, space-efficient)
```

### Letter Spacing

```
Headings (large):    -0.02em to -0.01em  (tighten for visual density)
Body text:            0em (default)
Small/caps text:     +0.02em to +0.05em  (open up for legibility)
ALL CAPS:            +0.05em to +0.10em  (always add tracking)
```

### Font Weight Usage

```
300 (Light):     Decorative large headings, pull quotes
400 (Regular):   Body text, descriptions, form inputs
500 (Medium):    Subheadings, emphasized body text, nav items
600 (Semibold):  Section headings, button text, labels
700 (Bold):      Page titles, key metrics, important UI elements
800+ (Black):    Hero text, marketing headlines (use sparingly)
```

## CSS Implementation

### Google Fonts Import
```css
/* Preconnect for performance */
@import url('https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=Plus+Jakarta+Sans:wght@500;600;700;800&display=swap');
```

### Tailwind 4 Font Tokens
```css
@theme {
  --font-display: "Plus Jakarta Sans", system-ui, sans-serif;
  --font-body: "Inter", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", "Fira Code", monospace;
}
```

### Usage in HTML
```html
<h1 class="font-display text-4xl font-bold tracking-tight">Page Title</h1>
<p class="font-body text-base leading-relaxed">Body text content...</p>
<code class="font-mono text-sm">console.log('hello')</code>
```
