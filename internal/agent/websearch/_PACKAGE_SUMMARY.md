# Package: `internal/agent/websearch`

## Role

First-party web search and page extraction behind the agent's `web_search` and `fetch_page` tools (ADR 0x021).
It replaces vendor-native web search so every model gets the same capability, and the provider is configuration rather than code.

## Responsibilities

- `Backend` (search) and `Extractor` (page text) interfaces, with Parallel (`parallel.go`, search and extract) and Brave (`brave.go`, search only) implementations.
- `New(Config)` picks the primary backend (`WEB_SEARCH_PROVIDER`, default Parallel), wraps the other keyed backend as a fallback, and exposes Parallel as the extractor when keyed.
  It returns `ErrNotConfigured` when no key is set, which callers treat as "tools off".
- Trims results for model context: snippets capped at 600 runes, pages at 20k runes, result counts at 1–10 (default 5), and HTML highlighting stripped from Brave text.

## Dependencies

- Standard library HTTP only; no provider SDKs.
- Used by `internal/agent` (`web_search_tool.go`, tool policy in `tools.go`) and `cmd/websearch-bakeoff`.
- Wired in `internal/server/server.go` from `internal/server/config.go`, only under `LLM_BACKEND=vendor`.

## Non-obvious decisions

- `fetch_page` goes through the provider's extract API, so our servers never fetch model-chosen URLs themselves.
- The fallback only runs when the primary errors, and not when the request context is already cancelled.
- Provider error bodies are truncated to 300 bytes before they reach errors (and so tool output), and never include the API key.
- Absent backends are kept as nil interfaces, not interfaces holding nil pointers, so selection and fallback checks stay correct.

## Testing

- `websearch_test.go` runs each backend against `httptest` servers: auth headers, request bodies, parsing, limits, truncation, extract errors, bounded HTTP errors, fallback and provider selection.
- `cmd/websearch-bakeoff` compares keyed backends on real queries (`scripts/websearch-bakeoff-queries.txt`); it needs real keys and is not part of CI.

## Related documentation

- [ADR 0x021](../../../docs/adr/0x021-first-party-web-search.md)
- [`internal/agent/_PACKAGE_SUMMARY.md`](../_PACKAGE_SUMMARY.md)
