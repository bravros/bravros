# Platform Specifications

## YouTube
- **Aspect Ratio**: 16:9
- **Resolution**: 1920×1080 (Full HD) or 3840×2160 (4K)
- **FPS**: 30 or 60
- **Max Duration**: Unlimited (but 10-15 minutes is optimal for engagement)
- **Format**: MP4 (H.264)
- **Thumbnail**: 1280×720, separate Still composition
- **Notes**: YouTube Shorts are 9:16, max 60 seconds

## TikTok
- **Aspect Ratio**: 9:16
- **Resolution**: 1080×1920
- **FPS**: 30
- **Max Duration**: 60 seconds (optimal: 15-30s for virality)
- **Format**: MP4 (H.264)
- **Safe Zone**: Keep text/elements within center 80% — top and bottom are covered by UI
- **Notes**: First frame is the thumbnail, hook must be instant (under 1s)

## Instagram Reels
- **Aspect Ratio**: 9:16
- **Resolution**: 1080×1920
- **FPS**: 30
- **Max Duration**: 90 seconds (optimal: 15-30s)
- **Format**: MP4 (H.264)
- **Safe Zone**: Bottom 20% is covered by caption/UI area, keep CTAs above
- **Notes**: Same as TikTok format, cross-post friendly

## Instagram Stories
- **Aspect Ratio**: 9:16
- **Resolution**: 1080×1920
- **FPS**: 30
- **Max Duration**: 15 seconds per story (can chain multiple)
- **Format**: MP4 (H.264)
- **Safe Zone**: Top 14% and bottom 25% may be covered by UI
- **Notes**: Auto-advances after 15s, design for quick consumption

## Instagram Feed
- **Aspect Ratio**: 1:1 or 4:5
- **Resolution**: 1080×1080 (square) or 1080×1350 (portrait)
- **FPS**: 30
- **Max Duration**: 60 seconds
- **Format**: MP4 (H.264)
- **Notes**: 4:5 gets more screen real estate in the feed, prefer it

## Twitter/X
- **Aspect Ratio**: 16:9 or 1:1
- **Resolution**: 1920×1080 or 1080×1080
- **FPS**: 30 or 60
- **Max Duration**: 140 seconds (optimal: under 45s)
- **Format**: MP4 (H.264)
- **Notes**: Videos autoplay muted, add captions/text overlays

## Apple App Store Preview
- **Aspect Ratio**: Varies by device
- **iPhone 6.7"**: 886×1920 (portrait) or 1920×886 (landscape)
- **iPhone 6.5"**: 886×1920 or 1920×886
- **iPhone 5.5"**: 1080×1920 or 1920×1080
- **iPad 12.9"**: 1200×1600 or 1600×1200
- **FPS**: 30
- **Max Duration**: 30 seconds
- **Format**: MP4 (H.264), no audio by default (autoplay is muted)
- **Notes**: Up to 3 preview videos per locale. Show the app in action. Don't use a phone mockup — Apple wraps it in a device frame automatically. Start with the most compelling feature.

## Google Play Store
- **Aspect Ratio**: 16:9 (landscape only)
- **Resolution**: 1920×1080
- **FPS**: 30
- **Max Duration**: 30 seconds (min 30s, max 120s for promo, but listing preview is ~30s)
- **Format**: MP4 (H.264)
- **Notes**: Must be landscape. Link a YouTube video. Can be a full promo or app walkthrough.

## Remotion Composition Presets

Use these when registering compositions:

```tsx
// Platform presets as constants
export const PLATFORMS = {
  youtube:        { width: 1920, height: 1080, fps: 30 },
  youtubeShorts:  { width: 1080, height: 1920, fps: 30 },
  tiktok:         { width: 1080, height: 1920, fps: 30 },
  instagramReel:  { width: 1080, height: 1920, fps: 30 },
  instagramStory: { width: 1080, height: 1920, fps: 30 },
  instagramFeed:  { width: 1080, height: 1350, fps: 30 },
  instagramSquare:{ width: 1080, height: 1080, fps: 30 },
  twitter:        { width: 1920, height: 1080, fps: 30 },
  appStoreIphone: { width: 886,  height: 1920, fps: 30 },
  playStore:      { width: 1920, height: 1080, fps: 30 },
} as const;
```
