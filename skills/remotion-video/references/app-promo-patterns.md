# App Promo Video Patterns

Patterns specifically for promoting mobile apps like Kashy on App Store, Play Store, and social media.

## Phone Mockup Component

The phone mockup is the centerpiece of any app promo. Build a reusable component:

```tsx
import { AbsoluteFill, Img, staticFile, useCurrentFrame, interpolate, spring, useVideoConfig } from "remotion";

type PhoneMockupProps = {
  screenshotSrc: string;
  enterFrom?: "right" | "left" | "bottom";
  delay?: number;
};

export const PhoneMockup: React.FC<PhoneMockupProps> = ({
  screenshotSrc,
  enterFrom = "right",
  delay = 0,
}) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const entrance = spring({
    frame: frame - delay,
    fps,
    config: { damping: 200, stiffness: 100 },
  });

  const directions = {
    right: { x: 300, y: 0 },
    left: { x: -300, y: 0 },
    bottom: { x: 0, y: 400 },
  };

  const { x: startX, y: startY } = directions[enterFrom];
  const translateX = interpolate(entrance, [0, 1], [startX, 0]);
  const translateY = interpolate(entrance, [0, 1], [startY, 0]);

  return (
    <div
      style={{
        transform: `translateX(${translateX}px) translateY(${translateY}px)`,
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        width: "100%",
        height: "100%",
      }}
    >
      <div
        style={{
          width: 280,
          height: 600,
          borderRadius: 40,
          overflow: "hidden",
          border: "4px solid #333",
          boxShadow: "0 20px 60px rgba(0,0,0,0.4)",
          position: "relative",
        }}
      >
        <Img
          src={staticFile(screenshotSrc)}
          style={{ width: "100%", height: "100%", objectFit: "cover" }}
        />
      </div>
    </div>
  );
};
```

## Feature Callout Pattern

Animate feature highlights over screenshots:

```tsx
type FeatureCalloutProps = {
  icon: string;     // emoji or icon path
  title: string;
  description: string;
  index: number;    // for stagger delay
};

export const FeatureCallout: React.FC<FeatureCalloutProps> = ({
  icon, title, description, index,
}) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const staggerDelay = index * Math.round(0.2 * fps);
  const entrance = spring({
    frame: frame - staggerDelay,
    fps,
    config: { damping: 15, stiffness: 100 },
  });

  const opacity = interpolate(entrance, [0, 1], [0, 1]);
  const translateY = interpolate(entrance, [0, 1], [30, 0]);

  return (
    <div style={{
      opacity,
      transform: `translateY(${translateY}px)`,
      display: "flex",
      alignItems: "center",
      gap: 16,
      padding: "12px 20px",
      background: "rgba(255,255,255,0.1)",
      borderRadius: 12,
      backdropFilter: "blur(10px)",
    }}>
      <span style={{ fontSize: 32 }}>{icon}</span>
      <div>
        <div style={{ fontWeight: "bold", fontSize: 18, color: "#fff" }}>{title}</div>
        <div style={{ fontSize: 14, color: "rgba(255,255,255,0.7)" }}>{description}</div>
      </div>
    </div>
  );
};
```

## App Store Badge Row

```tsx
export const StoreBadges: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const leftEntry = spring({ frame, fps, config: { damping: 200 } });
  const rightEntry = spring({ frame: frame - 5, fps, config: { damping: 200 } });

  return (
    <div style={{ display: "flex", gap: 20, justifyContent: "center" }}>
      <div style={{
        transform: `translateX(${interpolate(leftEntry, [0, 1], [-100, 0])}px)`,
        opacity: interpolate(leftEntry, [0, 1], [0, 1]),
      }}>
        <Img src={staticFile("badges/app-store.png")} style={{ height: 50 }} />
      </div>
      <div style={{
        transform: `translateX(${interpolate(rightEntry, [0, 1], [100, 0])}px)`,
        opacity: interpolate(rightEntry, [0, 1], [0, 1]),
      }}>
        <Img src={staticFile("badges/play-store.png")} style={{ height: 50 }} />
      </div>
    </div>
  );
};
```

## Number Counter Animation

Great for social proof scenes (downloads, ratings, users):

```tsx
type CounterProps = {
  from: number;
  to: number;
  suffix?: string;
  prefix?: string;
};

export const Counter: React.FC<CounterProps> = ({ from, to, suffix = "", prefix = "" }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const progress = spring({
    frame,
    fps,
    config: { damping: 100, stiffness: 50 },
  });

  const value = Math.round(interpolate(progress, [0, 1], [from, to]));
  const formatted = value.toLocaleString();

  return (
    <span style={{ fontVariantNumeric: "tabular-nums" }}>
      {prefix}{formatted}{suffix}
    </span>
  );
};
```

## Kashy-Specific Patterns

For Kashy app promos specifically:

### Brand Constants
```tsx
export const KASHY_BRAND = {
  colors: {
    primary: "#7C3AED",     // Purple
    secondary: "#06B6D4",   // Cyan
    dark: "#0F0F1A",        // Background
    light: "#F8FAFC",       // Text
    accent: "#F59E0B",      // Gold accent
  },
  fonts: {
    heading: "Inter",
    body: "Inter",
  },
  logo: "kashy-logo.png",
  appIcon: "kashy-icon.png",
} as const;
```

### Recommended Scene Flow for Kashy
1. **Hook**: Financial pain point → bold text, dark bg
2. **Problem**: Show complexity of existing tools (split screen of multiple apps)
3. **Kashy Reveal**: Logo + tagline with satisfying animation
4. **Dashboard Tour**: Phone mockup showing the main dashboard
5. **Key Feature 1**: Budget tracking → screenshot + callout
6. **Key Feature 2**: AI insights → screenshot + callout
7. **Key Feature 3**: Expense tracking → screenshot + callout
8. **Social Proof**: Ratings + download count with counter animation
9. **CTA**: Download badges + "Free to start"

### Asset Checklist for Kashy Videos
- [ ] App icon (PNG, transparent background)
- [ ] Logo (horizontal and stacked versions)
- [ ] 3-5 app screenshots (most photogenic screens)
- [ ] App Store badge (download from Apple's marketing resources)
- [ ] Google Play badge (download from Google's brand resources)
- [ ] Background music (royalty-free, upbeat)
- [ ] Brand colors confirmed in config
