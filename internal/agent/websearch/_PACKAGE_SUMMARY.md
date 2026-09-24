# Package: `internal/agent/websearch`

## Role

First-party web search and page extraction behind the agent's `web_search` and `fetch_page` tools (ADR 0x021), backed by Parallel.
When it is configured, every model gets the same capability and vendor-native web search is switched off.

## Responsibilities

- `Backend` (search) and `Extractor` (page text) interfaces, implemented by Parallel (`parallel.go`, Search and Extract v1 APIs).
  The interfaces are the seam for agent tests and any future provider.
- `New(Config)` returns a `Service` whose backend and extractor are both Parallel.
  It returns `ErrNotConfigured` when `PARALLEL_API_KEY` is unset, which callers treat as "tools off".
- `Query.Recency` (day/week/month/year) limits results to recently published pages via Parallel's `source_policy.after_date`.
- Trims results for model context: snippets capped at 600 runes, pages at 20k runes, result counts at 1–10 (default 5).

## Dependencies

- Standard library HTTP only; no provider SDKs.
- Used by `internal/agent` (`web_search_tool.go`, tool policy in `tools.go`) and `cmd/websearch-bakeoff`.
- Wired in `internal/server/server.go` from `internal/server/config.go`, only under `LLM_BACKEND=vendor`.

## Non-obvious decisions

- `fetch_page` goes through the provider's extract API, so our servers never fetch model-chosen URLs themselves.
- Parallel's v1 APIs reject unknown request fields (422 `extra_forbidden`), so tests pin request bodies to the documented JSON rather than comparing structs.
- Provider errors are summarised from Parallel's error envelope (message plus field errors), bounded to 300 runes and cut on a rune boundary, and never include the API key.

## Testing

- `websearch_test.go` runs the backend against `httptest` servers: auth header, exact request bodies (including recency and extract options), parsing, limits, truncation, extract errors and error summaries.
- `cmd/websearch-bakeoff` runs sample queries (`scripts/websearch-bakeoff-queries.txt`) against the real API to review quality and latency; it needs a real key and is not part of CI.

## Related documentation

- [ADR 0x021](../../../docs/adr/0x021-first-party-web-search.md)
- [`internal/agent/_PACKAGE_SUMMARY.md`](../_PACKAGE_SUMMARY.md)
