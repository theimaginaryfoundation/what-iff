package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/websearch"
)

// vendorOnlyWebSearch returns the configured web search service, or nil under the non-vendor
// LLM backends (mock/local, ADR 0x018), which must make no outbound calls.
func vendorOnlyWebSearch(cfg AgentConfig) *websearch.Service {
	if cfg.LLMBackend != "vendor" {
		return nil
	}
	return cfg.WebSearch
}

var errWebSearchUnavailable = errors.New("web search is not configured")

type webSearchToolInput struct {
	Query      string `json:"query"`
	Objective  string `json:"objective"`
	MaxResults int    `json:"max_results"`
}

type webSearchToolOutput struct {
	Query   string             `json:"query"`
	Results []websearch.Result `json:"results"`
}

// webSearchTool runs the first-party web_search tool (ADR 0x021).
func (a *Agent) webSearchTool(ctx context.Context, input []byte) (string, error) {
	if a.webSearch == nil || a.webSearch.Backend == nil {
		return "", errWebSearchUnavailable
	}
	var in webSearchToolInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("invalid web_search input: %w", err)
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return "", errors.New("web_search needs a non-empty query")
	}
	results, err := a.webSearch.Backend.Search(ctx, websearch.Query{Query: in.Query, Objective: in.Objective, MaxResults: in.MaxResults})
	if err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}
	if results == nil {
		results = []websearch.Result{}
	}
	return marshalToolOutput(webSearchToolOutput{Query: in.Query, Results: results})
}

type fetchPageToolInput struct {
	URL       string `json:"url"`
	Objective string `json:"objective"`
}

// fetchPageTool reads one page through the provider's extract API (ADR 0x021); our servers
// never fetch the model-chosen URL themselves.
func (a *Agent) fetchPageTool(ctx context.Context, input []byte) (string, error) {
	if a.webSearch == nil || a.webSearch.Extractor == nil {
		return "", errWebSearchUnavailable
	}
	var in fetchPageToolInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("invalid fetch_page input: %w", err)
	}
	target, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return "", fmt.Errorf("fetch_page needs a full http(s) URL, got %q", in.URL)
	}
	page, err := a.webSearch.Extractor.Extract(ctx, target.String(), in.Objective)
	if err != nil {
		return "", fmt.Errorf("fetch page failed: %w", err)
	}
	return marshalToolOutput(page)
}

func marshalToolOutput(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode tool output: %w", err)
	}
	return string(raw), nil
}
