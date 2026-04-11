#!/usr/bin/env python3
# /// script
# requires-python = ">=3.10"
# dependencies = ["yt-dlp"]
# ///
"""
YouTube search CLI — powered by yt-dlp.

Returns structured results with titles, channels, view counts,
subscriber counts, duration, upload dates, and direct links.

Usage:
    uv run scripts/search.py "claude code tutorial" --count 10
    uv run scripts/search.py "react native" --months 3
    uv run scripts/search.py "machine learning" --no-date-filter
    uv run scripts/search.py "AI agents" --json
"""

import argparse
import io
import json
import subprocess
import sys
from datetime import datetime, timedelta

# Force UTF-8 output to handle emoji in video titles
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")


def parse_args():
    parser = argparse.ArgumentParser(
        description="Search YouTube and return structured video results."
    )
    parser.add_argument("query", nargs="+", help="Search query terms")
    parser.add_argument(
        "--count", type=int, default=20, help="Number of results (default: 20)"
    )
    parser.add_argument(
        "--months",
        type=int,
        default=6,
        help="Only show videos from the last N months (default: 6)",
    )
    parser.add_argument(
        "--no-date-filter",
        action="store_true",
        help="Show all results regardless of upload date",
    )
    parser.add_argument(
        "--json", action="store_true", dest="json_output", help="Output results as JSON"
    )
    parser.add_argument(
        "--lang",
        type=str,
        default="en",
        help="YouTube interface language for search results (default: en). "
        "Use a BCP-47 code like 'pt' for Portuguese, 'es' for Spanish, etc.",
    )
    return parser.parse_args()


def format_subscribers(n):
    """Format subscriber count as human-readable (e.g., 45.2K, 1.2M)."""
    if n is None:
        return "N/A"
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n / 1_000:.1f}K"
    return str(n)


def format_views(n):
    """Format view count with commas."""
    if n is None:
        return "N/A"
    return f"{n:,}"


def format_duration(info):
    """Extract human-readable duration from yt-dlp info."""
    if info.get("duration_string"):
        return info["duration_string"]
    dur = info.get("duration")
    if dur is None:
        return "N/A"
    dur = int(dur)
    hours, remainder = divmod(dur, 3600)
    minutes, seconds = divmod(remainder, 60)
    if hours:
        return f"{hours}:{minutes:02d}:{seconds:02d}"
    return f"{minutes}:{seconds:02d}"


def format_date(raw):
    """Convert YYYYMMDD to human-readable date (e.g., Jan 10, 2026)."""
    if not raw or len(raw) != 8:
        return "N/A"
    try:
        dt = datetime.strptime(raw, "%Y%m%d")
        return dt.strftime("%b %d, %Y")
    except ValueError:
        return f"{raw[:4]}-{raw[4:6]}-{raw[6:8]}"


def get_cutoff_date(months):
    """Get the cutoff date as YYYYMMDD string, N months ago from today."""
    if months <= 0:
        return None
    cutoff = datetime.now() - timedelta(days=months * 30)
    return cutoff.strftime("%Y%m%d")


def search_youtube(query, count, months, no_date_filter, lang="en"):
    """Run yt-dlp search and return list of video info dicts."""
    effective_months = 0 if no_date_filter else months
    fetch_count = count * 2 if effective_months > 0 else count
    search_query = f"ytsearch{fetch_count}:{query}"

    cmd = [
        "yt-dlp",
        search_query,
        "--dump-json",
        "--no-download",
        "--no-warnings",
        "--quiet",
        "--extractor-args",
        f"youtube:lang={lang}",
    ]

    # Map common language codes to countries for geo-bypass,
    # which influences YouTube's result ranking toward that locale
    lang_to_country = {
        "en": "US", "pt": "BR", "es": "ES", "fr": "FR",
        "de": "DE", "ja": "JP", "ko": "KR", "it": "IT",
        "ru": "RU", "zh": "CN", "hi": "IN", "ar": "SA",
    }
    country = lang_to_country.get(lang)
    if country:
        cmd.extend(["--geo-bypass-country", country])

    date_label = f", last {effective_months} months" if effective_months > 0 else ""
    print(
        f'Searching YouTube for: "{query}" (top {count} results{date_label})...\n',
        file=sys.stderr,
    )

    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
    except subprocess.TimeoutExpired:
        print("Error: Search timed out after 120 seconds.", file=sys.stderr)
        sys.exit(1)

    if result.returncode != 0 and not result.stdout.strip():
        print(f"Error: yt-dlp failed:\n{result.stderr.strip()}", file=sys.stderr)
        sys.exit(1)

    videos = []
    for line in result.stdout.strip().splitlines():
        if not line.strip():
            continue
        try:
            videos.append(json.loads(line))
        except json.JSONDecodeError:
            continue

    if not videos:
        print("No results found.", file=sys.stderr)
        sys.exit(0)

    # Apply date filter
    cutoff = get_cutoff_date(effective_months)
    if cutoff:
        filtered = [v for v in videos if (v.get("upload_date") or "00000000") >= cutoff]
        skipped = len(videos) - len(filtered)
        videos = filtered
        if skipped > 0:
            print(
                f"(Filtered out {skipped} video(s) older than {effective_months} months)\n",
                file=sys.stderr,
            )

    if not videos:
        print(f"No results found within the last {effective_months} months.", file=sys.stderr)
        sys.exit(0)

    return videos[:count]


def build_result(info):
    """Transform raw yt-dlp info into a clean result dict."""
    video_id = info.get("id", "")
    views = info.get("view_count")
    subs = info.get("channel_follower_count")

    ratio = None
    if subs and views and subs > 0:
        ratio = round(views / subs, 2)

    return {
        "title": info.get("title", "Unknown Title"),
        "channel": info.get("channel", info.get("uploader", "Unknown")),
        "subscribers": subs,
        "subscribers_formatted": format_subscribers(subs),
        "views": views,
        "views_formatted": format_views(views),
        "views_subs_ratio": ratio,
        "duration": format_duration(info),
        "upload_date": format_date(info.get("upload_date", "")),
        "url": f"https://youtube.com/watch?v={video_id}" if video_id else "N/A",
    }


def print_table(results):
    """Print results as a formatted table to stdout."""
    divider = "\u2500" * 60

    for i, r in enumerate(results, 1):
        ratio_str = f"{r['views_subs_ratio']:.2f}x" if r["views_subs_ratio"] else "N/A"
        meta = (
            f"{r['channel']} ({r['subscribers_formatted']} subs)  \u00b7  "
            f"{r['views_formatted']} views ({ratio_str})  \u00b7  "
            f"{r['duration']}  \u00b7  {r['upload_date']}"
        )
        print(divider)
        print(f" {i:>2}. {r['title']}")
        print(f"     {meta}")
        print(f"     {r['url']}")

    print(divider)


def main():
    args = parse_args()
    query = " ".join(args.query)
    videos = search_youtube(query, args.count, args.months, args.no_date_filter, args.lang)
    results = [build_result(v) for v in videos]

    if args.json_output:
        print(json.dumps(results, indent=2, ensure_ascii=False))
    else:
        print_table(results)


if __name__ == "__main__":
    main()
