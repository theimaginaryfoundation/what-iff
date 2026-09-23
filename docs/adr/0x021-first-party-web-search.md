# ADR 0x021: First-party web search tools instead of vendor-native web search

- **Status:** Proposed
- **Date:** 2026-09-23
- **Deciders:** What Iff maintainers

## Context

Web search today is a vendor-native tool.
OpenAI and Claude run web search on the provider side, and the agent loop only learns what happened after the turn, when the provider's raw response is rebuilt into tool-call records (`internal/agent/web_search_tool_calls*.go`, `opts.mergeToolCalls` in `runGeneration`).
Every other model we support (Gemini, Mistral, DeepSeek, Qwen, Xiaomi MiMo, GLM, local models) has no web search at all.

That causes four problems.

1. **Inconsistent capability across models.**
   Whether a personality can look something up depends on which vendor backs the thread's model.
   Switching a thread from Claude to GLM silently removes a capability, and the same prompt behaves differently per model.
   This is the biggest problem: What Iff lets users pick models freely, and tools should not change underneath them.
2. **No visibility while it runs.**
   Vendor search executes inside the provider call, so it never passes through the agent loop.
   It cannot appear in the live tool-call timeline being added alongside this work (`Job.progress`, branch `feat/tool-call-progress`) and only shows up once the reply is saved.
3. **Cost.**
   Vendor web search is billed per search on top of tokens, at rates we don't control and can't compare.
   Dedicated search APIs cost $1–7 per 1,000 searches (see Options).
4. **Quality and control.**
   Vendor tools choose their own index, result count, freshness and snippet shape.
   We can't tune them for a companion app, can't cache or dedupe, and can't swap the backend when a better one appears.

The retired and retiring first-party options narrow the field further.
Microsoft retired the Bing Search APIs on 2025-08-11 in favour of search embedded in Azure agents.
Google's Custom Search JSON API is closed to new customers and shuts down on 2027-01-01.

MCP-provided tools (in progress separately) already take the path this ADR proposes: they execute in our agent loop, so every model gets them, and they appear in the live timeline and in saved tool calls.

## Decision

Add first-party search tools that run in our agent loop for every model, and stop depending on vendor-native web search.

- **`web_search`** returns a compact, model-friendly result list: title, URL, snippet or excerpt, and published date when known.
- **`fetch_page`** returns the readable text of one URL, for when a snippet isn't enough.
  It uses a provider extract API (Parallel Extract; Brave has none), so it is only offered when such a backend is configured.
- Both sit behind a small **`SearchBackend`** interface in the agent layer, so the provider is configuration, not code.
  The initial default is **Parallel**, with **Brave** implemented behind the same interface as the alternative and fallback.
- The tools are enabled when a backend is configured with a key.
  Without a key they are hidden from the tool list (like other env-gated tools), not stubbed.
- Under the non-vendor LLM backends (`mock`, `local`; ADR 0x018) the tools make no network calls, so the hermetic E2E suite stays offline.
- Vendor-native web search is turned off for providers that have it once the first-party tools are enabled, so a turn never has two competing search tools.
  Removing the vendor web-search code paths entirely is a follow-up once the new tools have been in use.

## Options considered

Prices are per 1,000 searches and were checked on 2026-09-23.
Benchmark figures come from [openbenchmarks.com](https://openbenchmarks.com/web-search/best-web-search-api-for-ai-agents) (12 providers, three separate task sets, updated 2026-09-22) unless noted.
Each provider wins a different task set, and a separate independent test found the top four statistically indistinguishable, so quality at the top is close and our own bake-off matters more than any single leaderboard.

| Option | Price | For | Against |
|---|---|---|---|
| **Keep vendor-native search** | Vendor per-search fee plus tokens | No work | Only some models get search; invisible until the turn is saved; no control over cost or quality |
| **Parallel** (chosen default) | $1 (Turbo ~0.2s, Fast ~0.7s), $5 (Basic ~1s, Advanced ~3s); 5,000 free per month | Leads the agent-search benchmark (46.5% F1); returns compressed, model-ready excerpts; has an extract endpoint for `fetch_page`; SOC 2 | Younger company; much public praise is from its own blog |
| **Exa** | $7, plus $1 per 1,000 pages of full text; deep modes $12–15 | Best single-query accuracy (99.3%); mature API; deep research modes | About 7× Parallel's cheapest tier; full text billed separately |
| **Brave** (fallback) | $5, with $5 free credit monthly | Independent first-party index (40B+ pages); zero data retention; LLM-context endpoint; tied with the leaders in an independent test | Closer to a classic results page, so more shaping on our side |
| **Perplexity Search API** | $5 flat for raw results | Best on retrieval-answering tasks (77.3%) | Operated by a company whose product is its own answer engine |
| **Tavily** | About $8 per credit; advanced searches cost more | Popular in the LangChain ecosystem | Acquired by Nebius in 2026-02 (up to $400M), so its roadmap now follows Nebius's cloud strategy |
| **Self-hosted SearXNG** | Hosting only | Free and private | Upstream engines rate-limit or block the instance's IP under load; not reliable for production |
| **Bing / Google CSE** | — | — | Bing retired 2025-08-11; Google CSE closed to new customers, shuts down 2027-01-01 |

Sources:
[Parallel pricing](https://parallel.ai/pricing),
[Exa pricing](https://exa.ai/pricing),
[Brave Search API](https://brave.com/learn/best-search-api-2026/),
[Perplexity pricing](https://docs.perplexity.ai/docs/getting-started/pricing),
[Tavily pricing](https://www.tavily.com/pricing),
[Nebius–Tavily announcement](https://nebius.com/newsroom/nebius-announces-agreement-to-acquire-tavily-to-add-agentic-search-to-its-ai-cloud-platform),
[Bing retirement](https://learn.microsoft.com/en-us/lifecycle/announcements/bing-search-api-retirement),
[Google Custom Search](https://developers.google.com/custom-search/v1/overview),
[SearXNG limiter docs](https://docs.searxng.org/admin/searx.limiter.html).

## Consequences

**Better:**
- Every model gets the same search capability, with the same result shape.
- Searches show live in the tool timeline and are saved as ordinary tool calls, identical across providers.
- Cost is low and predictable: 10,000 searches a month is roughly $10–70 depending on tier.
- The backend is swappable by configuration, and a fallback can cover a provider outage.

**Worse or new:**
- A new external dependency and API key to manage (one per backend), plus a second outbound HTTP client alongside the provider clients.
- We own result shaping, error handling and rate limiting instead of the vendor.
- Page fetching reads arbitrary URLs chosen by the model, so `fetch_page` goes through the provider's extract API rather than our servers fetching URLs directly, and output is size-capped.

## Evaluation before choosing the production default

A small script runs the same 20–30 real queries from What Iff conversations through each configured backend and writes the results side by side, so the default is picked on our own traffic rather than published benchmarks.
All candidate providers have free tiers, so the bake-off costs nothing.
