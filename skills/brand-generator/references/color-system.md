# Color System Reference

## OKLCH Color Space

All colors in the design system use OKLCH (Oklab Lightness, Chroma, Hue) because:
- **Perceptually uniform** — equal steps in values produce equal perceived changes
- **Better for palette generation** — hue shifts don't affect perceived brightness
- **Native CSS support** — `oklch()` works in all modern browsers
- **DaisyUI 5 default** — DaisyUI uses OKLCH internally

### OKLCH Structure
```
oklch(L% C H)
L = Lightness (0% = black, 100% = white)
C = Chroma (0 = gray, ~0.4 = maximum saturation)
H = Hue (0-360 degrees, like a color wheel)
```

### Hue Reference
```
0°   = Pink/Red
30°  = Red/Orange
60°  = Orange/Yellow
90°  = Yellow
120° = Yellow-Green
150° = Green
180° = Cyan/Teal
210° = Blue-Cyan
240° = Blue
270° = Blue-Violet
300° = Purple/Magenta
330° = Pink
```

## Palette Generation Methodology

### 1. Choose the Primary Color

From the inspiration analysis, identify the dominant brand energy:
- **What emotion should the brand evoke?** (trust → blue family, energy → orange/red, growth → green, innovation → purple)
- **What hue range do the inspiration sites favor?** Use this as a starting region, then shift to create originality
- **How saturated/bold should it be?** SaaS dashboards often use moderate chroma (0.15-0.25); marketing sites go bolder (0.25-0.35)

Recommended primary color ranges:
```
Conservative SaaS:   oklch(50-60% 0.15-0.20 H)
Modern SaaS:         oklch(55-65% 0.20-0.28 H)
Bold/Consumer:       oklch(60-70% 0.25-0.35 H)
```

### 2. Derive Secondary and Accent Colors

Use **harmonic relationships** from the primary hue:

**Complementary** (opposite on wheel, +180°): High contrast, use sparingly
**Analogous** (neighboring, ±30-60°): Harmonious, natural feeling
**Triadic** (±120°): Vibrant, balanced diversity
**Split-complementary** (±150°): Softer than complementary, still dynamic

Recommendations:
- **Secondary**: Analogous shift (±40-60° from primary), slightly lower chroma
- **Accent**: Complementary or split-complementary shift, can be higher chroma for pop

### 3. Generate Base Colors (Backgrounds/Surfaces)

Light theme base colors:
```css
--color-base-100: oklch(98-99% 0.005-0.02 H);  /* Page background */
--color-base-200: oklch(95-97% 0.01-0.025 H);   /* Card/elevated surface */
--color-base-300: oklch(90-94% 0.015-0.03 H);   /* Borders, dividers */
--color-base-content: oklch(15-25% 0.02-0.05 H); /* Main text */
```

Dark theme base colors:
```css
--color-base-100: oklch(15-22% 0.01-0.03 H);    /* Page background */
--color-base-200: oklch(22-28% 0.015-0.035 H);   /* Card/elevated surface */
--color-base-300: oklch(28-35% 0.02-0.04 H);    /* Borders, dividers */
--color-base-content: oklch(90-96% 0.01-0.02 H); /* Main text */
```

The hue (H) should be tinted toward the primary color — this creates brand cohesion even in neutral surfaces.

### 4. Generate Semantic Colors

Semantic colors should harmonize with the brand palette but remain universally recognizable:

```
Success:  Hue 140-160 (green family), Chroma 0.15-0.25
Warning:  Hue 70-90 (yellow/amber family), Chroma 0.15-0.25
Error:    Hue 20-35 (red family), Chroma 0.20-0.30
Info:     Hue 210-240 (blue family), Chroma 0.15-0.25
```

Adjust the exact hues so they don't clash with brand colors. If the primary is blue (H≈240), shift info toward cyan (H≈210).

### 5. Generate Neutral Color

Neutral should be a desaturated version of the primary:
```css
--color-neutral: oklch(35-50% 0.02-0.06 primary-H);
--color-neutral-content: oklch(95-98% 0.005-0.01 primary-H);
```

### 6. Content Colors (Text on Color)

For each color, generate a `-content` variant that provides readable text:
- On **dark** backgrounds: content should be `oklch(95-99% low-chroma H)`
- On **light** backgrounds: content should be `oklch(15-25% low-chroma H)`
- **Always verify** the contrast ratio meets WCAG AA (4.5:1 for normal text)

### WCAG Contrast Verification

For each color pair (background + content), calculate approximate contrast:
- WCAG AA: 4.5:1 for normal text, 3:1 for large text (18pt+)
- WCAG AAA: 7:1 for normal text, 4.5:1 for large text

Quick OKLCH lightness check (approximate):
- If background L > 60%: content should be L < 40% (or lower)
- If background L < 40%: content should be L > 80%
- The wider the gap, the better the contrast

## Color Scale Generation

For extended palettes (50-900 scale), generate from a base color:

```
50:  oklch(97%  C*0.15  H)
100: oklch(94%  C*0.25  H)
200: oklch(88%  C*0.40  H)
300: oklch(80%  C*0.60  H)
400: oklch(70%  C*0.80  H)
500: oklch(60%  C*1.00  H)  ← base color
600: oklch(52%  C*0.95  H)
700: oklch(44%  C*0.85  H)
800: oklch(35%  C*0.70  H)
900: oklch(25%  C*0.50  H)
```

Adjust these multipliers based on the specific hue — some hues (yellow) have naturally higher lightness at full chroma.
