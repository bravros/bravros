# app.css Template

When generating the app.css file, follow this exact structure. Replace all placeholder values with the generated design tokens.

## Template

```css
/* ==========================================================================
   {PROJECT_NAME} Design System
   Generated from brand inspiration analysis

   Tailwind CSS 4 + DaisyUI 5
   ========================================================================== */

@import "tailwindcss";
@plugin "daisyui";

/* ==========================================================================
   Google Fonts
   ========================================================================== */

@import url('{GOOGLE_FONTS_URL}');

/* ==========================================================================
   LIGHT THEME
   ========================================================================== */

@plugin "daisyui/theme" {
  name: "{project-slug}-light";
  default: true;
  color-scheme: light;

  /* Brand Colors */
  --color-primary: {PRIMARY_LIGHT};
  --color-primary-content: {PRIMARY_CONTENT_LIGHT};

  --color-secondary: {SECONDARY_LIGHT};
  --color-secondary-content: {SECONDARY_CONTENT_LIGHT};

  --color-accent: {ACCENT_LIGHT};
  --color-accent-content: {ACCENT_CONTENT_LIGHT};

  /* Neutral */
  --color-neutral: {NEUTRAL_LIGHT};
  --color-neutral-content: {NEUTRAL_CONTENT_LIGHT};

  /* Base / Surfaces */
  --color-base-100: {BASE_100_LIGHT};
  --color-base-200: {BASE_200_LIGHT};
  --color-base-300: {BASE_300_LIGHT};
  --color-base-content: {BASE_CONTENT_LIGHT};

  /* Semantic State Colors */
  --color-info: {INFO_LIGHT};
  --color-info-content: {INFO_CONTENT_LIGHT};

  --color-success: {SUCCESS_LIGHT};
  --color-success-content: {SUCCESS_CONTENT_LIGHT};

  --color-warning: {WARNING_LIGHT};
  --color-warning-content: {WARNING_CONTENT_LIGHT};

  --color-error: {ERROR_LIGHT};
  --color-error-content: {ERROR_CONTENT_LIGHT};

  /* Component Sizing */
  --radius-selector: {RADIUS_SELECTOR};
  --radius-field: {RADIUS_FIELD};
  --radius-box: {RADIUS_BOX};
  --size-selector: {SIZE_SELECTOR};
  --size-field: {SIZE_FIELD};
  --border: {BORDER_WIDTH};
  --depth: {DEPTH};
  --noise: {NOISE};
}

/* ==========================================================================
   DARK THEME
   ========================================================================== */

@plugin "daisyui/theme" {
  name: "{project-slug}-dark";
  prefersdark: true;
  color-scheme: dark;

  /* Brand Colors */
  --color-primary: {PRIMARY_DARK};
  --color-primary-content: {PRIMARY_CONTENT_DARK};

  --color-secondary: {SECONDARY_DARK};
  --color-secondary-content: {SECONDARY_CONTENT_DARK};

  --color-accent: {ACCENT_DARK};
  --color-accent-content: {ACCENT_CONTENT_DARK};

  /* Neutral */
  --color-neutral: {NEUTRAL_DARK};
  --color-neutral-content: {NEUTRAL_CONTENT_DARK};

  /* Base / Surfaces */
  --color-base-100: {BASE_100_DARK};
  --color-base-200: {BASE_200_DARK};
  --color-base-300: {BASE_300_DARK};
  --color-base-content: {BASE_CONTENT_DARK};

  /* Semantic State Colors */
  --color-info: {INFO_DARK};
  --color-info-content: {INFO_CONTENT_DARK};

  --color-success: {SUCCESS_DARK};
  --color-success-content: {SUCCESS_CONTENT_DARK};

  --color-warning: {WARNING_DARK};
  --color-warning-content: {WARNING_CONTENT_DARK};

  --color-error: {ERROR_DARK};
  --color-error-content: {ERROR_CONTENT_DARK};

  /* Component Sizing (same as light, or adjust) */
  --radius-selector: {RADIUS_SELECTOR};
  --radius-field: {RADIUS_FIELD};
  --radius-box: {RADIUS_BOX};
  --size-selector: {SIZE_SELECTOR};
  --size-field: {SIZE_FIELD};
  --border: {BORDER_WIDTH};
  --depth: {DEPTH_DARK};
  --noise: {NOISE};
}

/* ==========================================================================
   EXTENDED DESIGN TOKENS (Tailwind 4)
   ========================================================================== */

@theme {
  /* Typography */
  --font-display: {FONT_DISPLAY};
  --font-body: {FONT_BODY};
  --font-mono: {FONT_MONO};

  /* Extended Color Scales (brand-specific) */
  --color-brand-50: {BRAND_50};
  --color-brand-100: {BRAND_100};
  --color-brand-200: {BRAND_200};
  --color-brand-300: {BRAND_300};
  --color-brand-400: {BRAND_400};
  --color-brand-500: {BRAND_500};
  --color-brand-600: {BRAND_600};
  --color-brand-700: {BRAND_700};
  --color-brand-800: {BRAND_800};
  --color-brand-900: {BRAND_900};

  /* Shadow Scale */
  --shadow-xs: 0 1px 2px 0 rgb(0 0 0 / 0.03);
  --shadow-sm: 0 1px 3px 0 rgb(0 0 0 / 0.06), 0 1px 2px -1px rgb(0 0 0 / 0.06);
  --shadow-md: 0 4px 6px -1px rgb(0 0 0 / 0.08), 0 2px 4px -2px rgb(0 0 0 / 0.06);
  --shadow-lg: 0 10px 15px -3px rgb(0 0 0 / 0.08), 0 4px 6px -4px rgb(0 0 0 / 0.04);
  --shadow-xl: 0 20px 25px -5px rgb(0 0 0 / 0.08), 0 8px 10px -6px rgb(0 0 0 / 0.04);

  /* Animation */
  --ease-smooth: cubic-bezier(0.16, 1, 0.3, 1);
  --ease-bounce: cubic-bezier(0.34, 1.56, 0.64, 1);
}

/* ==========================================================================
   BASE STYLES
   ========================================================================== */

@layer base {
  html {
    @apply scroll-smooth antialiased;
    font-family: var(--font-body);
  }

  body {
    @apply bg-base-100 text-base-content;
  }

  h1, h2, h3, h4, h5, h6 {
    font-family: var(--font-display);
    @apply tracking-tight;
  }

  /* Focus ring styling for accessibility */
  :focus-visible {
    @apply outline-2 outline-offset-2 outline-primary;
  }
}

/* ==========================================================================
   COMPONENT OVERRIDES (optional brand-specific tweaks)
   ========================================================================== */

@layer components {
  /* Interactive card with brand hover effect */
  .card-brand {
    @apply bg-base-100 border border-base-200 rounded-box;
    @apply shadow-sm hover:shadow-md;
    @apply transition-all duration-200;
    transition-timing-function: var(--ease-smooth);
  }

  /* Gradient primary button variant */
  .btn-gradient {
    @apply btn border-0 text-primary-content;
    background: linear-gradient(
      135deg,
      var(--color-primary) 0%,
      var(--color-secondary) 100%
    );
  }
  .btn-gradient:hover {
    filter: brightness(1.1);
  }
}
```

## Implementation Notes

- Replace all `{PLACEHOLDER}` values with generated tokens
- The Google Fonts URL should include all weights needed for both display and body fonts
- `--depth` can be higher in light mode (1) and slightly lower in dark mode (0.5-0.8) for a more subtle feel
- `--noise` is optional — set to 0 unless the brand benefits from a textured/grain look
- The extended color scale (`brand-50` through `brand-900`) provides granular access beyond DaisyUI's semantic names
- Shadow values may need adjustment for dark themes (lighter shadows on dark backgrounds don't work well — consider reducing opacity or using colored shadows)
