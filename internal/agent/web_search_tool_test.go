package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/websearch"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type fakeSearchBackend struct {
	gotQuery websearch.Query
	results  []websearch.Result
	err      error
}

func (f *fakeSearchBackend) Name() string { return "fake" }
func (f *fakeSearchBackend) Search(_ context.Context, q websearch.Query) ([]websearch.Result, error) {
	f.gotQuery = q
	return f.results, f.err
}

type fakeExtractor struct {
	gotURL, gotObjective string
	page                 websearch.Page
}

func (f *fakeExtractor) Extract(_ context.Context, url, objective string) (websearch.Page, error) {
	f.gotURL, f.gotObjective = url, objective
	return f.page, nil
}

func TestApplyWebSearchPolicy(t *testing.T) {
	searchOnly := &websearch.Service{Backend: &fakeSearchBackend{}}
	withExtract := &websearch.Service{Backend: &fakeSearchBackend{}, Extractor: &fakeExtractor{}}

	cases := []struct {
		name                 string
		svc                  *websearch.Service
		userDisabledSearch   bool
		wantFirstParty       bool
		wantNative           bool
		wantSearchFnOffered  bool
		wantFetchPageOffered bool
	}{
		{"no backend keeps vendor search", nil, false, false, true, false, false},
		{"backend with extract replaces vendor search", withExtract, false, true, false, true, true},
		{"search-only backend hides fetch_page", searchOnly, false, true, false, true, false},
		{"user toggle turns off both kinds", withExtract, true, false, false, false, false},
		{"user toggle without backend", nil, true, false, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := turnToolPolicy{toolsEnabled: true, disabledTools: map[string]bool{}}
			if tc.userDisabledSearch {
				policy.disabledTools[agenttools.ToolNameWebSearch] = true
			}
			applyWebSearchPolicy(&policy, tc.svc)

			assert.Equal(t, tc.wantFirstParty, policy.firstPartyWebSearch, "firstPartyWebSearch")
			assert.Equal(t, tc.wantNative, policy.nativeWebSearch, "nativeWebSearch")
			assert.Equal(t, tc.wantSearchFnOffered, !policy.disabledTools[agenttools.ToolNameWebSearch], "web_search function offered")
			assert.Equal(t, tc.wantFetchPageOffered, !policy.disabledTools[agenttools.ToolNameFetchPage], "fetch_page offered")
			assert.False(t, policy.firstPartyWebSearch && policy.nativeWebSearch, "never both")
		})
	}
}

func TestVendorOnlyWebSearch(t *testing.T) {
	svc := &websearch.Service{Backend: &fakeSearchBackend{}}
	assert.Same(t, svc, vendorOnlyWebSearch(AgentConfig{LLMBackend: "vendor", WebSearch: svc}))
	assert.Nil(t, vendorOnlyWebSearch(AgentConfig{LLMBackend: "mock", WebSearch: svc}))
	assert.Nil(t, vendorOnlyWebSearch(AgentConfig{LLMBackend: "local", WebSearch: svc}))
}

func TestWebSearchTool(t *testing.T) {
	backend := &fakeSearchBackend{results: []websearch.Result{{Title: "Fox", URL: "https://a.example", Snippet: "s"}}}
	a := &Agent{webSearch: &websearch.Service{Backend: backend}}

	out, err := a.webSearchTool(context.Background(), []byte(`{"query":"  red fox ","objective":"facts","max_results":3,"recency":"week"}`))
	require.NoError(t, err)
	assert.Equal(t, websearch.Query{Query: "red fox", Objective: "facts", MaxResults: 3, Recency: websearch.RecencyWeek}, backend.gotQuery)
	var decoded webSearchToolOutput
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Equal(t, "red fox", decoded.Query)
	assert.Equal(t, backend.results, decoded.Results)

	backend.results = nil
	out, err = a.webSearchTool(context.Background(), []byte(`{"query":"nothing"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"query":"nothing","results":[]}`, out, "empty results encode as [] not null")

	_, err = a.webSearchTool(context.Background(), []byte(`{"query":"  "}`))
	assert.ErrorContains(t, err, "non-empty query")

	_, err = a.webSearchTool(context.Background(), []byte(`{"query":"x","recency":"fortnight"}`))
	assert.ErrorContains(t, err, "day, week, month or year", "a bad recency tells the model the allowed values")

	backend.err = errors.New("HTTP 500")
	_, err = a.webSearchTool(context.Background(), []byte(`{"query":"x"}`))
	assert.ErrorContains(t, err, "HTTP 500")

	_, err = (&Agent{}).webSearchTool(context.Background(), []byte(`{"query":"x"}`))
	assert.ErrorIs(t, err, errWebSearchUnavailable)
}

func TestFetchPageTool(t *testing.T) {
	ex := &fakeExtractor{page: websearch.Page{URL: "https://a.example/x", Title: "X", Content: "body"}}
	a := &Agent{webSearch: &websearch.Service{Backend: &fakeSearchBackend{}, Extractor: ex}}

	out, err := a.fetchPageTool(context.Background(), []byte(`{"url":" https://a.example/x ","objective":"prices"}`))
	require.NoError(t, err)
	assert.Equal(t, "https://a.example/x", ex.gotURL)
	assert.Equal(t, "prices", ex.gotObjective)
	assert.JSONEq(t, `{"url":"https://a.example/x","title":"X","content":"body"}`, out)

	for _, bad := range []string{`{"url":"file:///etc/passwd"}`, `{"url":"a.example"}`, `{"url":""}`, `{"url":"javascript:alert(1)"}`} {
		_, err := a.fetchPageTool(context.Background(), []byte(bad))
		assert.ErrorContains(t, err, "http(s) URL", bad)
	}

	_, err = (&Agent{webSearch: &websearch.Service{Backend: &fakeSearchBackend{}}}).fetchPageTool(context.Background(), []byte(`{"url":"https://a.example"}`))
	assert.ErrorIs(t, err, errWebSearchUnavailable)
}

// countingAdapter reports a fixed number of vendor-native searches.
type countingAdapter struct {
	provider.AgentAdapter
	native int
}

func (c countingAdapter) WebSearchCompletedCount() int { return c.native }

func TestTurnWebSearchCount(t *testing.T) {
	calls := []*models.ToolCall{
		{ToolName: agenttools.ToolNameWebSearch, ToolOutput: "{}"},
		{ToolName: agenttools.ToolNameWebSearch, ToolError: "web search failed: HTTP 500"},
		{ToolName: agenttools.ToolNameFetchPage, ToolOutput: "{}"},
		{ToolName: agenttools.ToolNameWebSearch, ToolOutput: "{}"},
		nil,
	}
	adapter := countingAdapter{native: 7}

	firstParty := &Agent{webSearch: &websearch.Service{Backend: &fakeSearchBackend{}}}
	assert.Equal(t, 2, firstParty.turnWebSearchCount(adapter, calls),
		"first-party bills successful web_search calls only; fetch_page and failures are free, and the adapter is not consulted")

	native := &Agent{}
	assert.Equal(t, 7, native.turnWebSearchCount(adapter, calls), "vendor-native bills what the provider reports")
}

func TestFirstPartyWebSearch(t *testing.T) {
	assert.True(t, (&Agent{webSearch: &websearch.Service{}}).FirstPartyWebSearch())
	assert.False(t, (&Agent{}).FirstPartyWebSearch())
	var nilAgent *Agent
	assert.False(t, nilAgent.FirstPartyWebSearch())
}
