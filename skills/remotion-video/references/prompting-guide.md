# Prompting Guide for Remotion Video Iteration

This guide helps you write effective prompts when iterating on video visuals with Claude.

## The Four-Layer Prompting Pattern

When asking Claude to create or modify a Remotion video, structure your request in these layers for the best results:

### Layer 1: Sequence Content
Define what happens narratively.

```
"Create a 30-second Kashy app promo. Scene 1 is a hook: bold white text
'Your Money, Your Rules' on a dark gradient. Scene 2 shows the app screenshot
in a phone mockup. Scene 3 lists 3 features with icons. Scene 4 is a CTA
with App Store and Play Store badges."
```

### Layer 2: Continuous Motion
Add subtle ongoing movement that makes the video feel alive.

```
"Add a slow gradient shift on the background — cycle between #1a1a2e and
#16213e over the full video length. Add a subtle floating particle effect
behind all scenes."
```

### Layer 3: Entrance/Exit Effects
Define how elements appear and disappear.

```
"The hook text should spring in from the bottom with slight overshoot.
The phone mockup should slide in from the right with a spring animation.
Feature icons should stagger in with 0.2s delays between each."
```

### Layer 4: Branding & Polish
Apply consistent visual identity.

```
"Use Kashy's brand purple (#7C3AED) as accent color. All text should be
Inter font, bold for headlines, regular for body. Include the Kashy logo
watermark in the bottom-right corner at 30% opacity."
```

## Iteration Prompts

Once you have a working video, use these patterns to refine:

### Timing Adjustments
```
"Scene 2 feels too long — reduce it from 8 seconds to 5. Scene 4 needs
more breathing room — extend to 6 seconds."
```

### Animation Refinement
```
"The text entrance on Scene 1 feels too slow. Use a snappier spring with
higher stiffness. The phone mockup slide should be smoother — try a spring
with damping: 200."
```

### Adding Elements
```
"Add a sound effect (swoosh) when the phone mockup enters. Add subtle
confetti particles in the CTA scene."
```

### Multi-Platform Adaptation
```
"I need this video in three formats: 9:16 for TikTok, 16:9 for YouTube,
and 1:1 for Instagram feed. Parametrize the layout to adapt to each
aspect ratio."
```

## Common Mistakes to Avoid

1. **Being too vague**: "Make a cool video" → Claude doesn't know what "cool" means to you
2. **Not specifying duration**: Always include target length
3. **Forgetting the platform**: 16:9 vs 9:16 changes everything about layout
4. **Ignoring audio**: Even if there's no voiceover, mention music/SFX preferences
5. **Not mentioning brand assets**: If you have logos, colors, fonts — say so upfront

## Example Full Prompt

```
Create a 30-second Kashy app promo video for Instagram Reels (9:16, 1080×1920).

Brand: Kashy purple (#7C3AED), dark background (#0F0F1A), Inter font.
Assets: App screenshots in public/screenshots/, logo at public/kashy-logo.png

Structure:
1. Hook (0-3s): "Stop overspending." — bold white text, spring-in from bottom
2. Pain (3-7s): "You deserve better than spreadsheets" — fade in with red accent
3. Solution (7-10s): Kashy logo reveal with particle burst
4. Features (10-22s): 3 app screenshots in phone mockup, slide transitions
   - Smart budgets
   - Real-time tracking
   - AI insights
5. Social proof (22-26s): "★★★★★ 50K+ users" — counter animation
6. CTA (26-30s): "Download free" + App Store/Play Store badges

Music: Upbeat electronic, volume 0.3
No voiceover.
Captions on all text scenes.
```
