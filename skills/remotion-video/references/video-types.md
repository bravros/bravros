# Video Type Templates

## App Promo Video

The classic app store / social media promo. Works for Kashy or any mobile app.

### Structure (30 seconds)

```
SCENE 1 — Hook (0:00–0:03)
  Purpose: Stop the scroll. Present the core value proposition in one line.
  Visual: Bold text animation on gradient/dark background
  Example: "Your finances. Finally simple."
  Animation: Spring-in from bottom, slight scale overshoot

SCENE 2 — Problem (0:03–0:07)
  Purpose: Resonate with the pain point
  Visual: Relatable scenario or pain-point text
  Example: "Tired of juggling 5 apps to track your money?"
  Animation: Fade in with subtle shake/glitch effect

SCENE 3 — Solution (0:07–0:12)
  Purpose: Introduce the app
  Visual: App logo + name with tagline
  Example: "Meet Kashy" + logo animation
  Animation: Scale up with spring, logo particles or reveal

SCENE 4 — Feature Showcase (0:12–0:22)
  Purpose: Show 2-3 key features with app screenshots
  Visual: Phone mockup with real screenshots, feature callouts
  Sub-scenes (3-4 seconds each):
    - Feature 1: Screenshot + highlight annotation
    - Feature 2: Screenshot + highlight annotation
    - Feature 3: Screenshot + highlight annotation
  Animation: Slide transitions between features, callout spring-in

SCENE 5 — Social Proof (0:22–0:26)
  Purpose: Build trust
  Visual: Star rating, review quotes, download count
  Example: "★★★★★ 50,000+ happy users"
  Animation: Counter animation for numbers, fade for text

SCENE 6 — CTA (0:26–0:30)
  Purpose: Drive action
  Visual: App Store + Play Store badges, QR code optional
  Example: "Download free today"
  Animation: Badges slide in from sides, pulse effect on CTA text
```

### Tips for App Promos
- Show REAL screenshots — mockups feel generic
- Use the app's actual brand colors throughout
- Phone mockup component: use a clean frame with rounded corners and shadow
- For App Store previews: skip the phone mockup (Apple adds the device frame)
- Include the app icon in at least 2 scenes

---

## Product Demo Video

A walkthrough of specific product features.

### Structure (60 seconds)

```
SCENE 1 — Intro (0:00–0:05)
  Visual: Product name + "How to [do X]"
  Animation: Clean fade-in

SCENE 2-6 — Walkthrough Steps (0:05–0:50)
  Each step: 8-10 seconds
  Visual: Screen recording or screenshots with cursor/highlight animations
  Text: Step number + brief instruction
  Animation: Zoom into relevant UI area, highlight callouts

SCENE 7 — Summary (0:50–0:55)
  Visual: Recap the key steps as bullet points
  Animation: List items spring in sequentially

SCENE 8 — CTA (0:55–1:00)
  Visual: "Try it now" + link/badge
  Animation: Scale pulse on CTA
```

---

## Explainer Video

Break down a concept or process.

### Structure (45–60 seconds)

```
SCENE 1 — Question Hook (0:00–0:05)
  Visual: Big question text
  Example: "What if managing your money was actually fun?"
  Animation: Typewriter or word-by-word reveal

SCENE 2 — Context (0:05–0:15)
  Visual: The current state / problem visualization
  Animation: Chart, icons, or illustrated concepts

SCENE 3 — Solution (0:15–0:30)
  Visual: How it works, step by step
  Animation: Sequential reveals, connecting lines/arrows

SCENE 4 — Benefits (0:30–0:45)
  Visual: Key benefits with icons
  Animation: Grid reveal or carousel

SCENE 5 — CTA (0:45–0:60)
  Visual: Clear next step
  Animation: Emphatic spring-in
```

---

## Social Short (TikTok/Reels/Shorts)

Maximum engagement in minimum time.

### Structure (15–30 seconds)

```
SCENE 1 — Hook (0:00–0:02)
  THE MOST IMPORTANT PART. If you lose them here, nothing else matters.
  Patterns that work:
    - Bold statement: "This app saved me $500/month"
    - Question: "Why is nobody talking about this?"
    - Visual pattern interrupt: unexpected animation or contrast
  Animation: Immediate — no fade-in. Spring or snap.

SCENE 2 — Punch (0:02–0:12)
  The meat. Show, don't tell. Quick cuts, fast transitions.
  - For app content: rapid feature montage
  - For tips: numbered points with quick transitions
  - For stories: the conflict/journey
  Animation: Fast slides, 0.3s transitions max

SCENE 3 — Payoff (0:12–0:15/0:25)
  The reward for watching. Satisfying conclusion.
  - Reveal, transformation, or clear takeaway
  - CTA if appropriate (but don't force it)
  Animation: Satisfying snap or spring to final state
```

### Short-Form Rules
- Captions are MANDATORY — 85% of social video is watched muted
- Use large, bold text — phones are small screens
- Cut ruthlessly — if a scene doesn't earn its seconds, remove it
- Pattern interrupts every 3-5 seconds keep attention
- Vertical video (9:16) ONLY for TikTok/Reels/Shorts

---

## Long-Form to Short-Form Clip

Extracting highlight clips from longer content.

### Process

1. **Identify the highlight** — find the most compelling 15-60 second segment
2. **Add context** — the clip needs to stand alone, so add intro context if needed
3. **Brand it** — add branded frame (lower third, watermark, channel name)
4. **Add captions** — mandatory for social distribution
5. **Add CTA** — "Watch the full video" or "Follow for more"

### Structure

```
SCENE 1 — Context Card (0:00–0:02)
  Visual: Topic/title card with brand
  Animation: Quick snap-in

SCENE 2 — The Clip (0:02–duration-3s)
  Visual: The extracted video segment
  Overlay: Captions (always), lower third (optional)
  Animation: Subtle zoom or pan to keep it dynamic

SCENE 3 — End Card (last 3 seconds)
  Visual: CTA + brand + where to find full content
  Animation: Slide up from bottom
```

### Technical Implementation

```tsx
// Use <Video> with trim props for the clip
<Video
  src={staticFile("source-video.mp4")}
  trimBefore={startTimeInSeconds * fps}
  trimAfter={(videoTotalDuration - endTimeInSeconds) * fps}
  volume={1}
/>

// Layer captions on top
<AbsoluteFill>
  <Video ... />
  <Sequence from={0} layout="none">
    <CaptionOverlay captions={captionData} />
  </Sequence>
  <Sequence from={0} layout="none">
    <LowerThird brandName="Kashy" />
  </Sequence>
</AbsoluteFill>
```
