---
name: remotion-video
description: |
  **Remotion Video Pipeline**: Full VDLC workflow for creating videos with React/Remotion — plan → script → storyboard → compose → preview → render. Handles app promos, product demos, social shorts, explainers, and long-to-short clips.
  - MANDATORY TRIGGERS: video, remotion, create video, make video, plan-video, app promo, product demo, explainer video, social media video, short-form, video clip, render video, motion graphics
  - ALSO TRIGGER: any video content (promos, demos, ads, reels, shorts, stories), TikTok/Reels/YouTube Shorts, long-form to short clips, Remotion setup, video rendering pipeline
  - Use proactively when user mentions anything video-related — "I need a promo" or "make content for social media."
metadata:
  tags: remotion, video, react, animation, production, pipeline, promo, shorts, social-media, vdlc
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# Remotion Video Production Pipeline

This skill orchestrates the full **Video Development Lifecycle (VDLC)** — from idea to rendered MP4. It wraps around the `remotion-best-practices` skill for technical implementation while providing the creative and structural pipeline that turns prompts into professional videos.

## Philosophy

Video production follows a lifecycle just like software development. Jumping straight to code produces mediocre results. The best videos come from treating each phase deliberately:

1. **Plan** — Define what the video is, who it's for, and what it should achieve
2. **Brand & Assets** — Extract real brand identity and gather all assets before touching code
3. **Script** — Write the narrative, dialogue, and on-screen text
4. **Storyboard** — Map scenes to a visual timeline with timing, transitions, and per-scene asset paths
5. **Compose** — Write the Remotion React components (design system first, then scenes)
6. **Preview** — Run the Remotion Studio and iterate on the visuals
7. **Render** — Export the final MP4/WebM

Each phase produces artifacts that feed the next. You can enter at any phase if earlier artifacts already exist.

## Phase 0: Environment Setup

Before any video work, ensure the environment is ready.

### Check for Remotion

```bash
# Check if a Remotion project exists in the current directory
ls package.json 2>/dev/null && grep -q "remotion" package.json 2>/dev/null
```

### If Remotion is NOT installed (empty directory)

```bash
# Create a new Remotion project
npx create-video@latest --blank

# Install dependencies
npm install

# Install the remotion-best-practices skill (if not already present)
npx skills add remotion-dev/skills --skill remotion-best-practices -y
```

When initializing, select these defaults:
- Template: **Blank** (we build from scratch for maximum control)
- TailwindCSS: **Yes** (essential for rapid styling)
- Skills: **Yes** (installs remotion-best-practices automatically)

### If directory is not empty (fallback)

`npx create-video` requires an empty directory and its `--yes` flag doesn't reliably skip interactive prompts. If you already have assets, planning files, or other content in the directory, set up Remotion manually:

```bash
# 1. Initialize npm
npm init -y

# 2. Install all dependencies
npm i remotion @remotion/cli @remotion/transitions @remotion/tailwind react react-dom zod
npm i -D typescript @types/react @types/react-dom tailwindcss @tailwindcss/vite

# 3. Create these files manually:
#    - tsconfig.json (with "jsx": "react-jsx")
#    - remotion.config.ts (with enableTailwind webpack override)
#    - src/index.ts (entry point, exports Root)
#    - src/Root.tsx (composition registry)

# 4. Add scripts to package.json:
#    "dev": "npx remotion studio"
#    "build": "npx remotion render [CompositionId]"
#    "render": "npx remotion render [CompositionId]"
```

### If Remotion IS installed

Verify the project structure has these essentials:
- `src/Root.tsx` — composition registry
- `src/` — component directory
- `public/` — static assets (images, audio, fonts)
- `remotion.config.ts` or equivalent

### Start the dev environment

Always remind the user to start the Remotion Studio in a separate terminal:

```bash
npm run dev
```

This gives them a live preview at `http://localhost:3000` where they can scrub through the timeline.

## Phase 1: Plan

Every video starts with a brief. Gather this information before writing anything:

### Video Brief Template

```
VIDEO BRIEF
───────────────────────────────────
Type:        [promo | demo | explainer | ad | social-short | clip | story]
Platform:    [youtube | tiktok | instagram-reel | instagram-story | twitter | app-store | general]
Duration:    [in seconds — e.g., 15s, 30s, 60s, 90s]
Aspect:      [16:9 | 9:16 | 1:1 | 4:5]
Audience:    [who is watching?]
Goal:        [what should the viewer do after watching?]
Tone:        [professional | playful | urgent | minimal | bold | cinematic]
Brand:       [website URL for brand extraction, or colors/fonts/logo if known]
Assets:      [screenshots, images, videos, audio files available]
Voiceover:   [yes/no — if yes, script will include VO text]
Music:       [yes/no — genre preference or specific track]
───────────────────────────────────
```

### Platform Presets

Load the appropriate reference for platform-specific constraints:

| Platform | Aspect | Resolution | Max Duration | Reference |
|----------|--------|-----------|-------------|-----------|
| YouTube | 16:9 | 1920×1080 | unlimited | `references/platforms.md` |
| TikTok / Reels / Shorts | 9:16 | 1080×1920 | 60-90s | `references/platforms.md` |
| Instagram Story | 9:16 | 1080×1920 | 15s | `references/platforms.md` |
| Instagram Feed | 1:1 or 4:5 | 1080×1080 or 1080×1350 | 60s | `references/platforms.md` |
| App Store Preview | varies | see reference | 30s | `references/platforms.md` |
| Twitter/X | 16:9 | 1920×1080 | 140s | `references/platforms.md` |

## Phase 1.5: Brand Extraction & Asset Pipeline

This phase happens before any composition work. Getting the visual identity and assets right upfront prevents costly redesign cycles later. Composing with placeholder colors and generic fonts is the single biggest waste of time in video production — the result always looks ugly and requires a full rewrite.

### Brand Extraction

If the client has a website, extract the real brand identity before writing any code:

1. **Scrape brand identity** — use `firecrawl scrape --format branding` (or the MCP tool) on the client's website
2. **Extract and document:**
   - Colors: primary, accent, background, text (exact hex values)
   - Fonts: display/impact, heading, body (family names)
   - Logo: URL to SVG/PNG
   - Button styles: CTA colors, border-radius, patterns
   - Personality: tone, energy level, target audience
3. **Download brand fonts** — Google Fonts TTF files from `https://fonts.gstatic.com/s/{font}/{version}/{file}.ttf`, or from the site itself
4. **Download logo** — SVG preferred, PNG as fallback, save to `public/images/logo.svg`
5. **Create `config.ts`** with REAL brand values — not guesses:

```ts
export const BRAND = {
  colors: {
    primary: '#EXTRACTED',   // from brand scrape
    accent: '#EXTRACTED',    // CTA/highlight color
    darkBg: '#EXTRACTED',    // dark background
    white: '#FFFFFF',
    // ...other extracted colors
  },
  fonts: {
    display: 'ExtractedFont',         // Impact text, prices, slogans
    heading: 'ExtractedFont',         // Headings, labels, badges
    body: 'ExtractedFont, system-ui', // Body text, captions
  },
  fps: 30,
  duration: 55,
  durationInFrames: 1650,
  width: 1080,
  height: 1920,
} as const;

export const SCENES = { /* timing per scene */ } as const;
export const toFrames = (seconds: number) => Math.round(seconds * BRAND.fps);
```

6. **Create `fonts.ts`** — Remotion cannot use CSS files for fonts. Use JavaScript injection:

```tsx
import { staticFile } from "remotion";

export const loadFonts = () => {
  const style = document.createElement("style");
  style.innerHTML = `
    @font-face {
      font-family: "YourDisplayFont";
      src: url("${staticFile("fonts/YourDisplayFont.ttf")}") format("truetype");
      font-weight: normal;
    }
    @font-face {
      font-family: "YourHeadingFont";
      src: url("${staticFile("fonts/YourHeadingFont-Bold.ttf")}") format("truetype");
      font-weight: 700;
    }
  `;
  document.head.appendChild(style);
};
```

Call `loadFonts()` at the top level of your main composition file, outside the component function.

### Asset Pipeline

Follow this structured process to gather all visual assets:

1. **Scrape & inventory** — use firecrawl to scrape all images from client websites (product photos, lifestyle shots, icons, badges, customer reviews)
2. **Download at highest resolution** — always grab the largest available size
3. **Convert formats** — use `sips -s format png file.webp --out file.png` to convert webp/avif to PNG (Remotion works best with PNG)
4. **Identify gaps** — compare storyboard needs vs. available assets
5. **Generate missing images** — write AI image prompts optimized for the user's preferred tool (see below)
6. **Organize** — place in `public/images/{source}/` with descriptive names

```
public/
├── images/
│   ├── {brand}/         # Downloaded from client website
│   ├── {brand2}/        # Downloaded from second source
│   ├── icons/           # SVG icons, badges, seals
│   └── generated/       # AI-generated images
├── fonts/               # Brand fonts (TTF/OTF)
└── audio/               # Music, sound effects
```

Rename downloaded files from hash names (e.g., `1-DXCx-mep.png`) to descriptive names (e.g., `product-front.png`) for maintainability.

### AI Image Generation Prompts

When assets need to be generated with AI tools:

- **Always ask** which tool the user will use (Midjourney, DALL-E, Nano Banana 2 / Gemini, etc.)
- **Adapt prompt style per tool:**
  - **Nano Banana 2 / Gemini**: Narrative descriptions with specific lens/aperture (e.g., "85mm f/2.8"), lighting direction, spatial relationships, and material descriptions. NOT keyword tags — it's a thinking model.
  - **Midjourney**: Keyword/tag style with `--ar` flags and style parameters
  - **DALL-E**: Natural language descriptions work well

For each prompt, always include:
- Output filename and save path (e.g., `generated/application-closeup.png`)
- Reference image(s) from existing assets with file path (e.g., 📎 `public/images/brand/product-front.png`)
- The prompt text optimized for the user's chosen tool

## Phase 2: Script

Write the video script based on the brief. The script is a structured document that maps **what happens** at each moment.

### Script Format

```
SCENE 1 — Intro Hook (0:00–0:03)
  Visual: [what appears on screen]
  Text: [on-screen text, if any]
  VO: [voiceover narration, if any]
  Audio: [music cue, sound effect]
  Animation: [entrance type — fade, slide, spring, etc.]

SCENE 2 — Problem Statement (0:03–0:08)
  Visual: ...
  Text: ...
  ...
```

### Script Guidelines

- **Hook in the first 2 seconds** — for social content, the first frame must grab attention
- **One idea per scene** — don't overload visual real estate
- **Write VO text naturally** — it'll be fed to ElevenLabs TTS if voiceover is enabled
- **Time in seconds** — the compose phase converts to frames using fps
- **Note transitions** — specify how scenes connect (fade, slide, wipe, cut)

### Caption Strategy for Social Videos

Captions are the PRIMARY content delivery for TikTok/Reels — 80% of viewers watch on mute.

**Rules:**
- Every scene MUST have a CaptionBar component
- Caption appears within the first 5-10 frames of each scene
- Emphasis words (max 2-3 per caption) get highlighted in the brand accent color
- Position: fixed bottom area, ~200px from bottom (TikTok safe zone)
- Style: glassmorphism panel, bold sans-serif, 44-48px
- Word reveal: progressive (word by word, 3-4 frames apart)

**Pick emphasis words that are:**
- The product/brand name
- Strong verbs/adjectives (NÃO, TODAS, NADA)
- Price points
- Action words from CTA

### Video Type Templates

For common video types, load the specific template from `references/video-types.md`:

- **App Promo**: Hook → Problem → Solution (app demo) → Features → Social proof → CTA
- **Product Demo**: Intro → Feature walkthrough → Key moments → CTA
- **Explainer**: Question → Context → Solution → How it works → CTA
- **Social Short**: Hook → Punch → Payoff (keep under 30s for maximum engagement)
- **Long-to-Short Clip**: Identify highlight → Add context overlay → Brand frame → CTA

## Phase 3: Storyboard

Convert the script into a technical storyboard. Each scene must include its exact asset paths so compose agents can work independently without cross-referencing.

### Storyboard Scene Format

```markdown
### Scene 3: APRESENTAÇÃO (0:07-0:13, 180 frames)
**Assets:**
- `images/brand/product-box.png` → product hero (enters from right)
- `images/brand/lifestyle-photo.png` → endorsement photo (scale in at frame 120)
- `images/icons/panda.svg` → next to "Cara de panda" text

**Elements:**
- Product image enters from right with spring
- Three pain-point texts with StrikethroughText at frames 40, 60, 80
- Endorsement photo scales in at frame 120

**Caption:** "Description text here."
**Emphasis words:** ["keyword1", "keyword2"]
**Transition → Fade (15 frames)**
```

This format gives each compose agent everything it needs: timing, assets, elements, and caption text.

### Storyboard Schema (JSON)

```json
{
  "composition": {
    "id": "MyVideo",
    "width": 1080,
    "height": 1920,
    "fps": 30,
    "durationInFrames": null
  },
  "scenes": [
    {
      "id": "intro-hook",
      "durationSeconds": 3,
      "assets": ["images/brand/logo.svg", "images/brand/product.png"],
      "elements": [
        {
          "type": "text",
          "content": "Stop scrolling.",
          "animation": "spring-in",
          "style": { "fontSize": 72, "fontWeight": "bold", "color": "#ffffff" }
        },
        {
          "type": "background",
          "value": "gradient",
          "colors": ["#1a1a2e", "#16213e"]
        }
      ],
      "caption": { "text": "Caption text here.", "emphasis": ["scrolling"] },
      "transition": { "type": "fade", "durationFrames": 15 }
    }
  ],
  "audio": {
    "music": { "src": "public/music/background.mp3", "volume": 0.3 },
    "voiceover": { "enabled": true, "voice": "elevenlabs-voice-id" }
  }
}
```

## Pre-Compose Checklist

Before writing any scene or component code, verify ALL of these:

- [ ] Brand colors extracted from real website and saved in `config.ts` (NOT guessed)
- [ ] Brand fonts downloaded to `public/fonts/` and `fonts.ts` created with `@font-face` declarations
- [ ] Logo downloaded to `public/images/`
- [ ] All website assets downloaded, converted to PNG, and organized in `public/images/`
- [ ] AI-generated images completed or marked as motion-graphics-only
- [ ] Storyboard has asset paths per scene (not in a separate inventory)
- [ ] `config.ts` has scene timing, brand constants, font families, and `toFrames()` helper
- [ ] `npm run dev` starts Remotion Studio successfully

**Do NOT proceed to compose until all items are checked.** Composing with placeholder styling wastes significant time because the result invariably needs a full redesign.

## Phase 4: Compose

Now write the actual Remotion code. This is where `remotion-best-practices` rules are consumed.

### Rule: Design System Before Scenes

Establish the complete design system in `config.ts` and `fonts.ts` before writing any scene code. Every scene and component must reference `BRAND.colors.*` and `BRAND.fonts.*` — never hardcode colors or font names. This prevents the "generic first pass, redesign everything" anti-pattern that wastes entire rounds of work.

```ts
// Good — every visual decision comes from BRAND
<span style={{ fontFamily: BRAND.fonts.display, color: BRAND.colors.accent }}>

// Bad — hardcoded values that will need changing
<span style={{ fontFamily: "Arial", color: "#FFD700" }}>
```

### Architecture Pattern

Every video follows this component structure:

```
src/
├── Root.tsx                    # Composition registry
├── [VideoName]/
│   ├── index.tsx               # Main composition (TransitionSeries)
│   ├── config.ts               # BRAND colors, fonts, timing, toFrames()
│   ├── fonts.ts                # @font-face loading via JS
│   ├── scenes/
│   │   ├── HookScene.tsx       # Scene 1
│   │   ├── ProblemScene.tsx    # Scene 2
│   │   ├── SolutionScene.tsx   # Scene 3
│   │   └── CTAScene.tsx        # Final scene
│   └── components/
│       ├── AnimatedText.tsx    # Word-by-word spring reveal
│       ├── CaptionBar.tsx      # TikTok-safe subtitle bar
│       └── ...                 # Other reusable components
public/
├── images/                     # All visual assets
├── fonts/                      # Brand fonts (TTF/OTF)
├── audio/                      # Music, sound effects
└── voiceover/                  # Generated TTS files
```

### Standard Component Library

These components solve recurring needs across most videos. Create only the ones your video requires:

| Component | Purpose | Common In |
|-----------|---------|-----------|
| **AnimatedText** | Word-by-word spring reveal with emphasis highlights | All text-heavy scenes |
| **CaptionBar** | TikTok-safe subtitle bar (glassmorphism, bottom-third) | Every scene for social video |
| **FeatureBadge** | Icon + text pill with spring-in (glassmorphism or solid) | Feature/benefit scenes |
| **ProductShot** | Product image with entrance + float animation + glow | Product showcase scenes |
| **StrikethroughText** | Text with animated crossout line | Comparison/contrast scenes |
| **PriceTag** | Animated price display (strike/hero variants with glow) | Pricing scenes |
| **BeforeAfter** | Slider reveal comparison (clip-path wipe) | Transformation scenes |
| **ParticleEffect** | Falling/floating particles (uses `random()` from remotion) | Emphasis moments |
| **GlassmorphismPanel** | Frosted glass container (`backdrop-filter: blur()`) | Overlay content |

All components must:
- Accept a `startFrame` prop for timing control
- Reference `BRAND.colors.*` and `BRAND.fonts.*` from config
- Use `staticFile()` for any image assets
- Use `Img` from remotion (not `<img>`)

### Compose Phase — Parallel Agent Strategy

For videos with 6+ scenes, split compose work across parallel agents for speed:

1. **Agent 1: Shared Components** — all reusable components from the table above
2. **Agent 2: Scenes 1-N/2** — first half of scenes
3. **Agent 3: Scenes N/2+1-N** — second half of scenes

Each agent receives:
- The full `config.ts` content (brand values, timing)
- Complete storyboard for their scenes (with asset paths)
- Component interfaces (props) even if components aren't built yet — agents import them assuming they exist

After all agents complete:
4. Wire all scenes into main `index.tsx` with `TransitionSeries`
5. Run `npx tsc --noEmit` to catch interface mismatches between components and scenes
6. Fix any prop mismatches (most common: a scene passes a prop the component doesn't accept)

### Critical Remotion Rules (always apply)

These are non-negotiable patterns. They come from `remotion-best-practices` but are worth emphasizing because getting them wrong breaks rendering:

1. **ALL animations use `useCurrentFrame()` + `interpolate()`** — CSS transitions and Tailwind animation classes are FORBIDDEN (they break during render)
2. **Use `spring()` for organic motion** — it feels natural and professional
3. **Always `extrapolateRight: "clamp"`** — prevents values from overshooting
4. **Always premount `<Sequence>` components** — `premountFor={1 * fps}` prevents flash-of-unstyled-content
5. **Use `staticFile()` for assets** — never hardcode `/public/` paths
6. **Use `random(seed)` not `Math.random()`** — deterministic rendering requires deterministic randomness
7. **Time in seconds, convert to frames** — `const frames = seconds * fps` using `useVideoConfig()`

### Composition Registration

Register every video in `src/Root.tsx`:

```tsx
import { Composition, Folder } from "remotion";
import { MyVideo } from "./MyVideo";

export const RemotionRoot = () => {
  return (
    <Folder name="Project">
      <Composition
        id="MyVideo"
        component={MyVideo}
        durationInFrames={30 * 30} // 30 seconds at 30fps
        fps={30}
        width={1080}
        height={1920}
      />
    </Folder>
  );
};
```

### Scene Assembly with TransitionSeries

For multi-scene videos, always use `<TransitionSeries>` for professional transitions:

```tsx
import { TransitionSeries, linearTiming } from "@remotion/transitions";
import { fade } from "@remotion/transitions/fade";
import { slide } from "@remotion/transitions/slide";

export const MyVideo = () => {
  const { fps } = useVideoConfig();

  return (
    <AbsoluteFill>
      <TransitionSeries>
        <TransitionSeries.Sequence durationInFrames={3 * fps}>
          <IntroHook />
        </TransitionSeries.Sequence>
        <TransitionSeries.Transition
          presentation={slide({ direction: "from-right" })}
          timing={linearTiming({ durationInFrames: Math.round(0.5 * fps) })}
        />
        <TransitionSeries.Sequence durationInFrames={5 * fps}>
          <ProblemStatement />
        </TransitionSeries.Sequence>
        <TransitionSeries.Transition
          presentation={fade()}
          timing={linearTiming({ durationInFrames: Math.round(0.5 * fps) })}
        />
        <TransitionSeries.Sequence durationInFrames={8 * fps}>
          <AppDemo />
        </TransitionSeries.Sequence>
      </TransitionSeries>
    </AbsoluteFill>
  );
};
```

### Voiceover Integration

If the video has voiceover, follow this pattern:

1. **Generate audio first** — run the voiceover generation script (see `remotion-best-practices/rules/voiceover.md`)
2. **Use `calculateMetadata`** to dynamically size the composition to match audio
3. **Sync scenes to audio durations** — each scene's `durationInFrames` comes from its corresponding audio file

### For Long-Form to Short-Form Clips

When extracting clips from existing video content:

1. Use the `<Video>` tag with `trimBefore` and `trimAfter` props to select segments
2. Add branded overlays (lower thirds, captions, CTA cards) using `<Sequence>` layering
3. Use `<AbsoluteFill>` to stack branded frame on top of source video
4. Add captions using `@remotion/captions` — load `remotion-best-practices/rules/subtitles.md`
5. Use FFmpeg for pre-processing if needed — load `remotion-best-practices/rules/ffmpeg.md`

## Phase 5: Preview

After composing, iterate in the Remotion Studio:

```bash
# Studio should already be running from Phase 0
# Open http://localhost:3000 in the browser
# Use the timeline scrubber to review each frame
```

### Preview Checklist

- [ ] Text is readable at target resolution
- [ ] Animations feel smooth (no jarring cuts)
- [ ] Timing matches the script
- [ ] Brand colors and fonts are consistent (match the source website)
- [ ] Audio syncs with visuals
- [ ] No elements overflow the canvas
- [ ] Transitions feel natural
- [ ] Captions are visible and readable on every scene (for social videos)

## Phase 6: Render

Export the final video:

```bash
# Render to MP4 (default, best compatibility)
npx remotion render [CompositionId]

# Render specific format
npx remotion render [CompositionId] --codec h264     # MP4
npx remotion render [CompositionId] --codec vp8       # WebM
npx remotion render [CompositionId] --codec prores     # ProRes (high quality)

# Render a still frame (thumbnail)
npx remotion still [CompositionId] --frame=0

# Render with specific props
npx remotion render [CompositionId] --props='{"variant":"tiktok"}'
```

### Multi-Platform Rendering

For videos that need multiple aspect ratios, use parametrized compositions:

```tsx
// Register multiple compositions from the same component
<Folder name="MyVideo">
  <Composition id="MyVideo-16x9" width={1920} height={1080} ... />
  <Composition id="MyVideo-9x16" width={1080} height={1920} ... />
  <Composition id="MyVideo-1x1"  width={1080} height={1080} ... />
</Folder>
```

Then render all:

```bash
npx remotion render MyVideo-16x9
npx remotion render MyVideo-9x16
npx remotion render MyVideo-1x1
```

### Lambda Rendering (for production/batch)

For high-volume rendering or faster exports, use Remotion Lambda:

```bash
# Deploy Lambda function (one-time)
npx remotion lambda functions deploy

# Deploy the site
npx remotion lambda sites create src/index.ts

# Render on Lambda
npx remotion lambda render [CompositionId]
```

## Quick-Start Commands

For users who want to skip the full VDLC and get something rendered fast:

| Command | What it does |
|---------|-------------|
| `plan-video` | Guides through the brief, produces a video plan |
| `script-video` | Takes a plan, writes the scene-by-scene script |
| `compose-video` | Takes a script, generates all Remotion components |
| `render-video` | Renders the current composition to MP4 |
| `make-video` | Full pipeline — plan through render in one flow |

## When to Load Additional References

- **Platform-specific constraints**: Read `references/platforms.md`
- **Video type templates and structures**: Read `references/video-types.md`
- **Prompting guide for iterating on visuals**: Read `references/prompting-guide.md`
- **App promo patterns (Kashy, app store videos)**: Read `references/app-promo-patterns.md`
- **Short-form content best practices**: Read `references/short-form-guide.md`

## Integration with remotion-best-practices

This skill orchestrates the workflow. For technical implementation details, always defer to the `remotion-best-practices` skill rules:

- **Animations**: `remotion-best-practices/rules/animations.md`
- **Transitions**: `remotion-best-practices/rules/transitions.md`
- **Audio**: `remotion-best-practices/rules/audio.md`
- **Captions/Subtitles**: `remotion-best-practices/rules/subtitles.md`
- **Text effects**: `remotion-best-practices/rules/text-animations.md`
- **3D content**: `remotion-best-practices/rules/3d.md`
- **Charts/data viz**: `remotion-best-practices/rules/charts.md`
- **Voiceover (ElevenLabs)**: `remotion-best-practices/rules/voiceover.md`
- **FFmpeg operations**: `remotion-best-practices/rules/ffmpeg.md`
- **Parameters/Zod schemas**: `remotion-best-practices/rules/parameters.md`

Think of this skill as the director, and `remotion-best-practices` as the technical crew.
