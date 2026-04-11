---
name: cf-browser
description: >
  Fetch webpage content as markdown, HTML, or screenshot using Cloudflare Browser Rendering
  via Docker MCP — zero cost, no API key needed. Use this skill when the user wants to
  quickly grab page content, convert a URL to markdown, get HTML source, or take a screenshot.
  Triggers on "cf-browser", "cloudflare browser", "fetch page free", "grab this page",
  or when firecrawl credits are low and a free alternative is preferred. Also useful as a
  lightweight alternative to firecrawl for simple single-page fetches. Does NOT replace
  firecrawl for search, crawling, or multi-page extraction — only for single URL content.
allowed-tools:
  - mcp__MCP_DOCKER__get_url_markdown
  - mcp__MCP_DOCKER__get_url_html_content
  - mcp__MCP_DOCKER__get_url_screenshot
  - mcp__MCP_DOCKER__set_active_account
  - mcp__MCP_DOCKER__accounts_list
---

**Read `skills/_shared/language-progress.md` before starting** — respond in the user's language throughout, show emoji step progress.

## Model Requirement

This skill runs well on **Sonnet**. No deep reasoning required — mechanical/scripted operations.

# Cloudflare Browser Rendering

Fetch webpage content using Cloudflare's Browser Rendering API via Docker MCP. Zero cost — no API credits consumed.

## When to Use This vs Firecrawl

| Need | Use |
|---|---|
| Single page → markdown | **cf-browser** (free) or firecrawl |
| Single page → screenshot | **cf-browser** (free) or firecrawl |
| Single page → HTML source | **cf-browser** (free) |
| Web search | firecrawl only |
| Crawl entire site section | firecrawl only |
| Map site URLs | firecrawl only |
| Structured data extraction | firecrawl only |
| Interactive pages (login, clicks) | firecrawl browser only |
| Firecrawl credits running low | **cf-browser** as fallback |

**Rule of thumb:** For a single URL fetch, prefer cf-browser (free). For anything beyond that, use firecrawl.

## Available Tools

### 1. Get Markdown (most common)

Fetches a page and converts it to clean markdown. Best for reading content.

```
mcp__MCP_DOCKER__get_url_markdown({ url: "https://example.com" })
```

### 2. Get HTML Source

Returns the raw HTML of the page. Useful for inspecting structure or parsing specific elements.

```
mcp__MCP_DOCKER__get_url_html_content({ url: "https://example.com" })
```

### 3. Take Screenshot

Captures a visual screenshot of the page. Optional viewport size.

```
mcp__MCP_DOCKER__get_url_screenshot({
  url: "https://example.com",
  viewport: { width: 1280, height: 800 }
})
```

Default viewport is 800x600. Use 1280x800 for desktop-like screenshots, 390x844 for mobile.

## Process

1. **Parse the request** — determine which tool (markdown, HTML, or screenshot) fits best
2. **Call the tool** with the URL
3. **Handle output:**
   - For markdown: present inline or save to `.firecrawl/` if large (reuse the same output directory for consistency)
   - For HTML: save to file, present a summary
   - For screenshots: the image is returned directly
4. **If 429 error** (rate limited): fall back to firecrawl scrape

## Output Conventions

Follow the same conventions as firecrawl skills:
- Save large outputs to `.firecrawl/` with `-cf` suffix to distinguish: `.firecrawl/{site}-{path}-cf.md`
- Add `.firecrawl/` to `.gitignore` if not already present
- For inline results, present directly in the conversation

## Limitations

- **Single pages only** — cannot search, crawl, or map sites
- **No JS interaction** — cannot click buttons, fill forms, or handle login
- **Rate limits** — some sites may return 429 errors (Cloudflare's free tier)
- **No content filtering** — returns full page including nav/footer (no `--only-main-content` equivalent)
- **Account setup** — may need to set active Cloudflare account on first use (`accounts_list` → `set_active_account`)

## Account Setup

If tools return an error about no active account:

1. List accounts: `mcp__MCP_DOCKER__accounts_list()`
2. Set active: `mcp__MCP_DOCKER__set_active_account({ accountId: "..." })`

This only needs to happen once per session.
