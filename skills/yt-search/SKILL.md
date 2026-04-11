---
name: yt-search
description: |
  **YouTube Search**: Search YouTube directly and get structured results — titles, channels, subscriber counts, view counts, duration, upload dates, and links. Uses yt-dlp under the hood with date filtering (last 6 months by default).
  - MANDATORY TRIGGERS: youtube search, search youtube, find youtube videos, yt search, youtube results
  - ALSO TRIGGER: when the user wants to find videos on a topic, research YouTube content, look up tutorials on YouTube, compare YouTube channels, find trending videos, or mentions "search for videos" even without explicitly saying "YouTube"
  - Use proactively when the user asks anything like "find me videos about X", "what's on YouTube about Y", "search for tutorials on Z", or "look up videos".
metadata:
  tags: youtube, search, yt-dlp, video-search, research
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

# YouTube Search

Search YouTube from Claude and get structured, filterable results. This skill wraps `yt-dlp` search via a standalone Python CLI script, so results are always fresh and don't require any API keys.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

## How It Works

The search script lives at `scripts/search.py` within this skill's directory. It uses `yt-dlp` (installed automatically via `uv`) to query YouTube, then formats results into a readable table or JSON.

Each result includes: title, channel name, subscriber count, view count, views/subscribers ratio, duration, upload date, and a direct YouTube link.

## Running a Search

Always use `uv run` to execute the script — this handles dependency installation automatically (no venv, no pip install needed):

```bash
uv run <skill-path>/scripts/search.py "<query>" [options]
```

Replace `<skill-path>` with the absolute path to this skill's directory.

### Options

| Flag               | Default | Description                                      |
|--------------------|---------|--------------------------------------------------|
| `--count N`        | 20      | Number of results to return                      |
| `--months N`       | 6       | Only show videos from the last N months          |
| `--no-date-filter` | —       | Show all results regardless of upload date       |
| `--json`           | —       | Output as JSON array instead of formatted table  |
| `--lang CODE`      | en      | YouTube interface language (e.g., `pt`, `es`)    |

### Examples

```bash
# Basic search — top 20 results from the last 6 months
uv run <skill-path>/scripts/search.py "claude code tutorial"

# Fewer results, shorter time window
uv run <skill-path>/scripts/search.py "react native expo" --count 5 --months 3

# All-time results as JSON (useful for further processing)
uv run <skill-path>/scripts/search.py "machine learning" --no-date-filter --json

# Research a niche topic
uv run <skill-path>/scripts/search.py "livewire 3 testing" --count 10
```

## Language Preference

By default, the script uses `--lang en` which sets YouTube's interface locale to English and geo-bypasses to US. This nudges results toward English content, but YouTube's algorithm may still surface popular non-English videos for generic queries.

To maximize English-only results, **also phrase the query in English** — this is the strongest signal YouTube respects. For example, use "react native tutorial" rather than just "react native" if the user wants English tutorials.

If the user explicitly requests content in another language, pass the appropriate language code:

```bash
# Portuguese results
uv run <skill-path>/scripts/search.py "tutorial laravel" --lang pt

# Spanish results
uv run <skill-path>/scripts/search.py "tutorial react" --lang es
```

Unless the user asks for a specific language, always use the default (`en`) and phrase queries in English.

## Presenting Results

When showing results to the user, present the formatted table output directly — it's already designed to be readable. If the user needs the data for further analysis or processing, use `--json` and work with the structured output.

The views/subscribers ratio is included as a signal of how well a video performed relative to the channel's size — a ratio above 1.0 means the video reached beyond the channel's subscriber base, which often indicates high-quality or trending content.

## Troubleshooting

If `uv run` fails with a dependency error, the most common cause is a network issue. The script's inline metadata tells `uv` to install `yt-dlp` automatically, so there's nothing to install manually.

If results seem sparse, try:
- Broadening the query terms
- Increasing `--months` or using `--no-date-filter`
- Increasing `--count` to fetch more results
