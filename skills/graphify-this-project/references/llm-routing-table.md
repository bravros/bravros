# LLM routing table — semantic extraction + community labeling

## TL;DR — Default

**For semantic extraction, default to the external 10-terminal opencode + DeepSeek V4 Pro 1M-context swarm** — ~2x better quality than any in-Claude pattern, ~$6-15 for a typical 1500-file Laravel project, and the gap is durable (it names full pipelines and catches latent couplings a small-context worker cannot see).

For community LABELING (after extraction), the table below applies — labeling is a structured task (sample → 2-4 word output), so frontier reasoning models are overkill. Cost columns matter only for direct-API runtimes; under a Claude Code subscription they're reference-only.

## Decision matrix — community LABELING (post-extraction)

| Runtime | Recommended model(s) | Reasoning |
|---|---|---|
| **Anywhere (default)** | single **Kimi K2.6** call OR single **DeepSeek V4 Pro** call with all communities batched into one prompt | Cheapest + fastest path. ~$0.05-0.20 in API. Single LLM call replaces a 20-worker swarm |
| **Claude Code (no API key)** | spawn N parallel **Haiku 4.5** sub-agents via Agent tool | Zero cost on Pro/Max plans, ~15-20s per agent. Quality is fine for the structured labeling task even though it's much weaker than DeepSeek for extraction |
| **Claude Code (rate-limited fallback)** | spawn N parallel **Sonnet 4.6** sub-agents | If Haiku rate-limits hit. ~3× more token-efficient |
| **Anthropic API direct** | claude-sonnet-4-6 ($3/$15 per 1M) in batches | Same as above without the Agent tool overhead |
| **Gemini frontier (AI Studio, Codex-Gemini)** | **gemini-2.0-flash** or **gemini-3-flash-preview** ($0.075/$0.30 per 1M) | Free tier alone covers most projects (1500 RPD); cheapest paid option |
| **OpenAI Codex / ChatGPT** | **gpt-4o-mini** or **gpt-5-nano** ($0.15/$0.60 range) | Mini-tier is correct for structured labeling; frontier overkill |
| **Kimi (Moonshot)** | **kimi-k2.6** (their best, ~$0.10/$0.40 per 1M) | Quality > cost at this tier; their pricing is already low so use the best they offer. **Caveats:** (a) Moonshot has separate `.ai` (international) and `.cn` (China) endpoints — keys are region-specific, a key for one rejects the other. (b) Always verify with `curl -H "Authorization: Bearer $MOONSHOT_API_KEY" https://api.moonshot.ai/v1/models` BEFORE launching a long extraction; a 401 means wrong region or invalid key, regenerate at console.moonshot.ai. (c) Default model name in graphify is `kimi-k2.6`; for direct OpenAI-client calls try `kimi-k2-turbo-preview` |
| **DeepSeek** | **deepseek-v3.2-exp** (latest, $0.14/$0.28 per 1M) | Their best model is still cheaper than US frontier mid-tier |
| **Ollama / local** | **qwen2.5-coder:7b** or **llama3.1:8b** (free, slower) | For air-gapped or zero-budget setups |

## Cost estimates (full project semantic + labeling)

For a ~1500-file Laravel project (paylog) with ~1300 communities:

| Backend | Extract phase | Label phase | Total | Wall time | Quality |
|---|---:|---:|---:|---|---|
| **🥇 External DeepSeek V4 Pro swarm (DEFAULT)** | ~$6-15 | ~$0.05 | **~$6-15** | ~30-60 min (10 terminals) | **Best — names full pipelines, catches latent couplings** |
| Gemini Flash 2.0 | ~$0.07 | ~$0.02 | ~$0.09 | ~5-10 min | Decent — below DeepSeek but above Haiku |
| Kimi-K2.6 (256K context) | ~$0.10 | ~$0.05 | ~$0.15 | ~5-10 min | Below DeepSeek (smaller context limits layer-wide view) |
| Haiku 4.5 swarm via Claude Code | $0 (subscription) | $0 | **$0** | ~10-15 min (waves) | **~2x worse than DeepSeek — produces "dense square" viz with no real architectural insight** |
| Sonnet 4.6 swarm via Claude Code | $0 (subscription) | $0 | $0 | ~5-8 min | Better than Haiku, still below DeepSeek 1M |

## Key sourcing

The skill reads its API key from the env var `<BACKEND>_API_KEY` (e.g. `GEMINI_API_KEY`, `KIMI_API_KEY`). Sourcing precedence:

1. **op_lazy resolver in shell rc** (preferred) — `op_lazy GEMINI_API_KEY "op://Vault/Item/credential"`. Resolves on demand, never plaintext on disk.
2. **op run --env-file=.env** — `.env` contains `op://` references, `op run` injects at process start.
3. **Plaintext export** in `~/.zshrc.local` — last resort, only if 1Password isn't available.

Never write the plaintext key to a tracked file. Never echo it in chat — even partial.

## Backend swap logic (skill behavior)

The skill should:
1. Detect the running runtime (Claude Code, OpenCode, Gemini CLI, etc.) — usually via env vars or process introspection
2. Check which `<BACKEND>_API_KEY` is set in env
3. Pick the row from the matrix above
4. If no key is set, prompt the user with the matrix's recommended choices and offer to wire op_lazy

For the **labeling-swarm step** specifically, the skill should:
- Under Claude Code → spawn N parallel Agents (this is "free" inside Claude Code)
- Under any direct-API runtime → make N parallel HTTP calls via the openai-compatible client (graphify already vendors `openai` package)
