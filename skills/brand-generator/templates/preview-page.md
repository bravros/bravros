# Preview Page Template

Generate a single-file HTML page that showcases the entire design system. This page serves as both a living reference and a stakeholder demo.

## Requirements

- **Self-contained**: Single HTML file that works when opened directly in a browser
- **Includes the theme**: Embed the full `app.css` content inline (in a `<style>` tag)
- **Theme toggle**: Light/dark switch that toggles `data-theme` attribute
- **Responsive**: Looks good on both desktop and mobile
- **DaisyUI CDN**: Include DaisyUI + Tailwind via CDN for the preview (the actual project will use npm)

## CDN Setup

```html
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{Project Name} — Design System Preview</title>

  <!-- Tailwind 4 + DaisyUI via CDN -->
  <link href="https://cdn.jsdelivr.net/npm/daisyui@5/themes.css" rel="stylesheet" />
  <link href="https://cdn.jsdelivr.net/npm/daisyui@5/full.css" rel="stylesheet" />
  <script src="https://cdn.tailwindcss.com?plugins=typography"></script>

  <!-- Google Fonts -->
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link href="{GOOGLE_FONTS_URL}" rel="stylesheet">

  <style>
    /* Inline the DaisyUI theme definitions here */
    /* Since this is a standalone preview, define themes using CSS selectors */

    :root, [data-theme="light"] {
      --color-primary: ...;
      /* All light theme variables */
    }

    [data-theme="dark"] {
      --color-primary: ...;
      /* All dark theme variables */
    }

    /* Font assignments */
    body { font-family: var(--font-body); }
    h1, h2, h3, h4, h5, h6 { font-family: var(--font-display); }
  </style>
</head>
```

## Page Sections (in order)

### 1. Header + Theme Toggle
```html
<div class="navbar bg-base-100 border-b border-base-200 sticky top-0 z-50">
  <div class="flex-1">
    <span class="text-xl font-display font-bold">{Project Name}</span>
    <span class="badge badge-ghost ml-2">Design System</span>
  </div>
  <div class="flex-none">
    <label class="swap swap-rotate">
      <input type="checkbox" onchange="toggleTheme()" />
      <!-- Sun icon for light -->
      <svg class="swap-on w-6 h-6" ...>...</svg>
      <!-- Moon icon for dark -->
      <svg class="swap-off w-6 h-6" ...>...</svg>
    </label>
  </div>
</div>
```

### 2. Color Palette Grid
Show all brand colors as swatches with their OKLCH/hex values:
- Primary, Secondary, Accent row
- Base 100, 200, 300 row
- Success, Warning, Error, Info row
- Neutral row
- Brand scale (50-900) row

Each swatch: colored square + name + value label

### 3. Typography Showcase
- Display the type scale from Display XL down to Caption
- Show both Display and Body fonts in use
- Include a sample paragraph demonstrating body text readability
- Show font weights available

### 4. Button Showcase
```html
<div class="flex flex-wrap gap-3">
  <button class="btn btn-primary">Primary</button>
  <button class="btn btn-secondary">Secondary</button>
  <button class="btn btn-accent">Accent</button>
  <button class="btn btn-ghost">Ghost</button>
  <button class="btn btn-outline">Outline</button>
  <button class="btn btn-link">Link</button>
</div>
<!-- Also show sizes: btn-xs, btn-sm, btn-md, btn-lg -->
<!-- Also show states: disabled, loading -->
```

### 5. Form Elements
- Text inputs (normal, focus, error, disabled states)
- Select dropdown
- Checkbox and radio
- Toggle switch
- Textarea
- File input

### 6. Card Components
- Basic card
- Card with image
- Card with actions
- Interactive card (hover effect)

### 7. Alerts & Feedback
```html
<div class="alert alert-info">Info alert</div>
<div class="alert alert-success">Success alert</div>
<div class="alert alert-warning">Warning alert</div>
<div class="alert alert-error">Error alert</div>
```
Also show: toasts, badges, progress bars

### 8. Data Display
- Table with zebra striping
- Stats component
- Badge variants
- Tooltip preview

### 9. Navigation Components
- Tabs
- Breadcrumbs
- Pagination
- Steps

### 10. Dark/Light Comparison Strip
At the bottom, show a side-by-side preview of a sample "mini dashboard" in both light and dark mode simultaneously.

## Theme Toggle Script

```javascript
<script>
  function toggleTheme() {
    const html = document.documentElement;
    const current = html.getAttribute('data-theme');
    html.setAttribute('data-theme', current === 'dark' ? 'light' : 'dark');
  }

  // Initialize based on system preference
  if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
    document.documentElement.setAttribute('data-theme', 'dark');
  } else {
    document.documentElement.setAttribute('data-theme', 'light');
  }
</script>
```

## Quality Checklist

Before delivering the preview:
- [ ] Theme toggle works correctly between light and dark
- [ ] All colors are visible and distinguishable in both modes
- [ ] Text is readable on all backgrounds (contrast check)
- [ ] Fonts load correctly from Google Fonts
- [ ] Page is responsive on mobile viewport
- [ ] All DaisyUI component variants render properly
- [ ] No broken layout or overlapping elements
