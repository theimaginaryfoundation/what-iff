package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/websearch"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
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

// FirstPartyWebSearch reports whether this agent runs the first-party web_search/fetch_page
// tools (ADR 0x021). When false, the user's web search toggle maps to vendor-native search.
func (a *Agent) FirstPartyWebSearch() bool {
	return a != nil && a.webSearch != nil
}

type webSearchToolInput struct {
	Query      string `json:"query"`
	Objective  string `json:"objective"`
	MaxResults int    `json:"max_results"`
	Recency    string `json:"recency"`
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
	recency, err := websearch.ParseRecency(in.Recency)
	if err != nil {
		return "", err
	}
	results, err := a.webSearch.Backend.Search(ctx, websearch.Query{Query: in.Query, Objective: in.Objective, MaxResults: in.MaxResults, Recency: recency})
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

// turnWebSearchCount returns the turn's billable web searches for metering. First-party
// search bills each successful web_search call (fetch_page is not a search); vendor-native
// search bills what the provider reports. Exactly one of the two can run in a turn.
func (a *Agent) turnWebSearchCount(adapter provider.AgentAdapter, toolCalls []*models.ToolCall) int {
	if !a.FirstPartyWebSearch() {
		return adapter.WebSearchCompletedCount()
	}
	n := 0
	for _, tc := range toolCalls {
		if tc != nil && tc.ToolName == agenttools.ToolNameWebSearch && tc.ToolError == "" {
			n++
		}
	}
	return n
}

func marshalToolOutput(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode tool output: %w", err)
	}
	return string(raw), nil
}
