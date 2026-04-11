# Short-Form Content Best Practices

## The Science of Short Attention

Short-form video (TikTok, Reels, Shorts) plays by different rules than traditional video. The algorithm rewards **watch-through rate** above all. Every second must earn the next second.

## The 3-Second Rule

You have 3 seconds to convince someone not to swipe. The first 3 seconds should:

1. **Create a pattern interrupt** — something visually unexpected
2. **Promise value** — "Here's how..." or "This changed everything"
3. **Trigger curiosity** — an incomplete statement or surprising visual

### First-Frame Techniques

```tsx
// DON'T: Slow fade in (they'll scroll before they see it)
const opacity = interpolate(frame, [0, 30], [0, 1]);

// DO: Immediate presence with spring pop
const scale = spring({ frame, fps, config: { damping: 10, stiffness: 200 } });
// Element is visible from frame 0, just pops into final size
```

## Pacing Guidelines

| Duration | Scenes | Avg Scene Length | Transitions |
|----------|--------|-----------------|-------------|
| 15s | 3-4 | 3-5s | Snappy (0.2-0.3s) |
| 30s | 5-7 | 3-5s | Mix of snappy and smooth |
| 60s | 8-12 | 4-6s | Varied (keep it interesting) |

## Caption Styling for Short-Form

Captions are not optional — they're a core design element.

### TikTok-Style Captions

```tsx
// Large, centered, bold text with background
<div style={{
  position: "absolute",
  bottom: "30%",
  left: "50%",
  transform: "translateX(-50%)",
  textAlign: "center",
}}>
  <span style={{
    fontSize: 48,
    fontWeight: 900,
    color: "#fff",
    padding: "8px 16px",
    backgroundColor: "rgba(0,0,0,0.7)",
    borderRadius: 8,
    lineHeight: 1.3,
  }}>
    {currentWord}
  </span>
</div>
```

### Word-by-Word Highlight

The most engaging caption style — highlight the current word:

```tsx
// Use @remotion/captions for word-level timing
// Load remotion-best-practices/rules/display-captions.md for implementation
```

## Transition Speed

Short-form videos use faster transitions than long-form:

```tsx
// Short-form: snappy 0.3s transitions
linearTiming({ durationInFrames: Math.round(0.3 * fps) })

// vs Long-form: smooth 0.5-1s transitions
linearTiming({ durationInFrames: Math.round(0.7 * fps) })
```

## Vertical Layout Patterns

### Safe Zones (9:16)

```
┌──────────────────┐
│   TOP UI ZONE    │ ← Status bar, don't put content here (top 8%)
│──────────────────│
│                  │
│   MAIN CONTENT   │ ← Your video content (center 64%)
│     AREA         │
│                  │
│──────────────────│
│  CAPTION ZONE    │ ← Where captions go (68-80% from top)
│──────────────────│
│  BOTTOM UI ZONE  │ ← Covered by TikTok/Reels UI (bottom 20%)
└──────────────────┘
```

### Text Sizing for Mobile

```tsx
// Headlines: 56-72px (readable at arm's length)
// Body text: 36-48px (needs to be readable on small phones)
// Captions: 40-56px (bold, high contrast)
// Never go below 28px — invisible on phone screens
```

## Music & Sound

Short-form videos that perform well almost always have:

1. **A beat-synced hook** — text appears on the beat
2. **Sound effects on transitions** — swoosh, pop, whoosh
3. **Trending audio** (for organic content) — but for branded content, use royalty-free

### Beat-Syncing Pattern

```tsx
// If BPM is 120, one beat = 0.5 seconds = 15 frames at 30fps
const BEAT_FRAMES = Math.round((60 / BPM) * fps);

// Align scene transitions to beat boundaries
const scene1Duration = BEAT_FRAMES * 4;  // 4 beats
const scene2Duration = BEAT_FRAMES * 8;  // 8 beats
```

## Content Formulas That Work

### The "Did You Know" Formula
```
Hook: "Did you know [surprising fact]?"
Punch: [Visual proof / demonstration]
Payoff: [Takeaway + CTA]
```

### The "Before/After" Formula
```
Hook: [Show the "before" state]
Punch: [Show the transformation]
Payoff: [Reveal the "after" + how they can get it]
```

### The "3 Things" Formula
```
Hook: "3 things about [topic] you need to know"
Point 1: [Quick visual + text]
Point 2: [Quick visual + text]
Point 3: [Quick visual + text]
Payoff: [CTA or summary]
```

### The "Watch This" Formula
```
Hook: "Watch what happens when..."
Demo: [The thing happening in real-time]
Payoff: [Result + reaction/CTA]
```
