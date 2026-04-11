---
name: frontend-design
description: Create distinctive, production-grade frontend interfaces with high design quality. Use this skill whenever the user asks to build web components, pages, layouts, or applications — whether in Laravel (Tailwind + DaisyUI + Livewire) or React. Triggers on requests involving UI building, page design, component creation, dashboard layouts, landing pages, forms, or any visual interface work.
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

Build distinctive, production-grade frontend interfaces that avoid generic "AI slop" aesthetics. Produce real working code with exceptional attention to detail and creative choices.

## Stack Detection

Detect the stack from the project before coding:

**Laravel project** (composer.json exists):
- **Tailwind CSS** (latest) — utility-first, use `@apply` sparingly
- **DaisyUI** — component classes on top of Tailwind (`btn`, `card`, `modal`, `drawer`, etc.)
- **Vite** — asset pipeline (`@vite(['resources/css/app.css', 'resources/js/app.js'])`)
- **Livewire** — dynamic components (`wire:click`, `wire:model`, `wire:submit`)
- **Alpine.js** — lightweight reactivity (`x-data`, `x-show`, `x-transition`)
- **Blade templates** — `resources/views/`, `@extends`, `@section`, `{{ }}` syntax

**React project** (package.json with react):
- Free to use any UI library (Chakra, Radix, shadcn, MUI, custom)
- Tailwind is preferred but not required
- Motion (framer-motion) for animations
- Flexible — match the project's existing patterns

**Static / Other**: Use Tailwind + vanilla JS. Include via CDN if no build system.

## Design Thinking

Before coding, understand the context and commit to a bold aesthetic direction:

- **Purpose**: What problem does this interface solve? Who uses it?
- **Tone**: Pick a strong direction — brutally minimal, maximalist, retro-futuristic, organic/natural, luxury/refined, playful, editorial/magazine, brutalist/raw, art deco/geometric, soft/pastel, industrial/utilitarian, or something uniquely fitting. Use these as inspiration but design something true to the project's character.
- **Constraints**: Technical requirements, accessibility, performance.
- **Differentiation**: What makes this unforgettable? What's the one thing someone remembers?

Choose a clear conceptual direction and execute it with precision. Bold maximalism and refined minimalism both work — intentionality matters, not intensity.

## Implementation

### Laravel / DaisyUI

### Livewire Version Check (MANDATORY)

Livewire 3 and Livewire 4 patterns are not interchangeable.

Before writing or editing any Livewire component, detect the major version and follow the project's established conventions.

How to detect quickly:

```bash
LIVEWIRE_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.livewire 2>/dev/null)
DAISYUI_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.daisyui 2>/dev/null)
TAILWIND_VERSION=$(~/.claude/bin/bravros detect-stack --versions --field versions.tailwindcss 2>/dev/null)
```

Rules:

- If the repo already has Livewire components, match their style (attributes vs rules, lifecycle methods, directory layout).
- Do not introduce Livewire 4-only APIs into a Livewire 3 project (and vice-versa).
- If unclear, read 2-3 existing components + their tests and mirror the pattern.

Use DaisyUI's component system as the foundation, then customize heavily:

```html
<!-- DaisyUI base + Tailwind customization -->
<div class="card bg-base-200 shadow-xl hover:shadow-2xl transition-all duration-300">
  <div class="card-body">
    <h2 class="card-title text-primary font-display tracking-tight">
      {{ $title }}
    </h2>
    <p class="text-base-content/70 leading-relaxed">{{ $description }}</p>
    <div class="card-actions justify-end">
      <button class="btn btn-primary btn-sm gap-2" wire:click="save">
        Save Changes
      </button>
    </div>
  </div>
</div>
```

**DaisyUI themes**: Use `data-theme` for consistent theming. Customize in `tailwind.config.js`:

```js
// tailwind.config.js
module.exports = {
  plugins: [require('daisyui')],
  daisyui: {
    themes: [
      {
        brand: {
          "primary": "#your-color",
          "secondary": "#your-color",
          "accent": "#your-color",
          "neutral": "#your-color",
          "base-100": "#your-color",
        },
      },
    ],
  },
}
```

**Alpine.js for micro-interactions**:

```html
<div x-data="{ open: false }" class="relative">
  <button @click="open = !open" class="btn btn-ghost">
    Menu
  </button>
  <div x-show="open" x-transition.opacity.duration.200ms
       @click.away="open = false"
       class="absolute mt-2 menu bg-base-200 rounded-box shadow-lg w-52 p-2">
    <!-- items -->
  </div>
</div>
```

**Livewire for dynamic components**:

```html
<div class="space-y-4">
  <input type="text" wire:model.live.debounce.300ms="search"
         class="input input-bordered w-full" placeholder="Search..." />

  <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
    @foreach($results as $item)
      <div class="card bg-base-100 shadow" wire:key="{{ $item->id }}">
        <!-- card content -->
      </div>
    @endforeach
  </div>
</div>
```

### React

Free to use the project's existing patterns. Default preferences:

- Tailwind for styling when available
- Motion library (framer-motion) for animations
- Component composition over inheritance
- CSS variables for theme consistency

### Fonts

Use distinctive fonts loaded via Google Fonts or CDN. In Laravel, add to the layout:

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=YOUR+FONT&display=swap" rel="stylesheet">
```

Then reference in Tailwind config:

```js
theme: {
  extend: {
    fontFamily: {
      display: ['Your Display Font', 'serif'],
      body: ['Your Body Font', 'sans-serif'],
    },
  },
}
```

## Aesthetics Guidelines

### Typography
Choose fonts that are beautiful, unique, and interesting. Avoid generic fonts (Arial, Inter, Roboto, system fonts). Pair a distinctive display font with a refined body font. DaisyUI gives you `font-sans` base — override it with character.

### Color & Theme
Commit to a cohesive aesthetic. Use DaisyUI's theme system for consistency — customize the semantic colors (`primary`, `secondary`, `accent`, `base-*`) to match the vision. Dominant colors with sharp accents outperform timid, evenly-distributed palettes.

### Motion & Interaction
Use CSS transitions and Alpine.js `x-transition` for micro-interactions. Focus on high-impact moments: a well-orchestrated page load with staggered reveals creates more delight than scattered animations. In React, use Motion library for complex sequences.

### Spatial Composition
Unexpected layouts. Asymmetry. Overlap. Grid-breaking elements. Generous negative space OR controlled density. DaisyUI provides grid/flex utilities — use them as a starting point, then break the grid intentionally.

### Backgrounds & Visual Details
Create atmosphere and depth rather than defaulting to solid colors. Gradient meshes, noise textures, geometric patterns, layered transparencies, dramatic shadows, decorative borders, grain overlays. Tailwind's gradient utilities (`bg-gradient-to-*`) and backdrop filters (`backdrop-blur-*`) are your friends.

## What to Avoid

- Generic AI aesthetics (purple gradients on white, predictable card grids)
- Overused fonts (Inter, Roboto, Space Grotesk across every generation)
- Cookie-cutter layouts that lack context-specific character
- Cliched color schemes — every design should feel unique
- Using DaisyUI defaults without customization — always push the components further
- Bare `btn btn-primary` without contextual styling — DaisyUI is the skeleton, not the skin

## Remember

Match implementation complexity to the aesthetic vision. Maximalist designs need elaborate code. Minimalist designs need precision in spacing, typography, and subtle details. Every design should feel genuinely crafted for its context — not templated.

Vary between light and dark themes, different fonts, different aesthetics. Never converge on common choices across generations. Show what's possible when committing fully to a distinctive vision.
