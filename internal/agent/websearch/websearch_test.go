package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParallelSearch(t *testing.T) {
	var gotBody parallelSearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "pk", r.Header.Get("x-api-key"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		_, _ = w.Write([]byte(`{"results":[
			{"url":"https://a.example","title":" Red foxes ","publish_date":"2026-01-02","excerpts":["one","two"]},
			{"url":"https://b.example","title":"B","publish_date":null,"excerpts":[]},
			{"url":"https://c.example","title":"C","excerpts":["x"]}]}`))
	}))
	defer srv.Close()
	p := NewParallel("pk", "", srv.Client())
	p.baseURL = srv.URL

	results, err := p.Search(context.Background(), Query{Query: " red fox ", Objective: "fun facts", MaxResults: 2})
	require.NoError(t, err)
	assert.Equal(t, parallelSearchRequest{Objective: "fun facts", SearchQueries: []string{"red fox"}, Mode: "fast"}, gotBody)
	assert.Equal(t, []Result{
		{Title: "Red foxes", URL: "https://a.example", Snippet: "one … two", PublishedAt: "2026-01-02"},
		{Title: "B", URL: "https://b.example"},
	}, results)
}

func TestParallelSearch_RecencySetsAfterDate(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	p := NewParallel("pk", "", srv.Client())
	p.baseURL = srv.URL
	p.now = func() time.Time { return time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC) }

	_, err := p.Search(context.Background(), Query{Query: "week 3 injuries", Recency: RecencyWeek})
	require.NoError(t, err)
	assert.JSONEq(t, `{"search_queries":["week 3 injuries"],"mode":"fast","advanced_settings":{"source_policy":{"after_date":"2026-09-17"}}}`, string(gotBody))
}

func TestParallelSearch_SourcePolicy(t *testing.T) {
	now := time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		q        Query
		wantBody string
	}{
		{"no filters sends no advanced settings", Query{Query: "q"}, `{"search_queries":["q"],"mode":"fast"}`},
		{"exact date", Query{Query: "q", PublishedAfter: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
			`{"search_queries":["q"],"mode":"fast","advanced_settings":{"source_policy":{"after_date":"2026-09-01"}}}`},
		{"later cutoff wins: recency", Query{Query: "q", Recency: RecencyWeek, PublishedAfter: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
			`{"search_queries":["q"],"mode":"fast","advanced_settings":{"source_policy":{"after_date":"2026-09-17"}}}`},
		{"later cutoff wins: exact date", Query{Query: "q", Recency: RecencyMonth, PublishedAfter: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)},
			`{"search_queries":["q"],"mode":"fast","advanced_settings":{"source_policy":{"after_date":"2026-09-20"}}}`},
		{"domains only", Query{Query: "q", IncludeDomains: []string{"espn.com"}, ExcludeDomains: []string{"reddit.com"}},
			`{"search_queries":["q"],"mode":"fast","advanced_settings":{"source_policy":{"include_domains":["espn.com"],"exclude_domains":["reddit.com"]}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBody, _ = io.ReadAll(r.Body)
				_, _ = w.Write([]byte(`{"results":[]}`))
			}))
			defer srv.Close()
			p := NewParallel("pk", "", srv.Client())
			p.baseURL = srv.URL
			p.now = func() time.Time { return now }

			_, err := p.Search(context.Background(), tc.q)
			require.NoError(t, err)
			assert.JSONEq(t, tc.wantBody, string(gotBody))
		})
	}
}

func TestNormalizeDomains(t *testing.T) {
	got, err := NormalizeDomains([]string{" https://www.ESPN.com/nfl?x=1 ", "espn.com", "news.ycombinator.com"})
	require.NoError(t, err)
	assert.Equal(t, []string{"espn.com", "news.ycombinator.com"}, got)

	got, err = NormalizeDomains(nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	for _, bad := range []string{"localhost", "", "not a domain.com", "user@example.com"} {
		_, err := NormalizeDomains([]string{bad})
		assert.ErrorContains(t, err, "is not a domain", bad)
	}
	_, err = NormalizeDomains(make([]string, MaxDomainFilters+1))
	assert.ErrorContains(t, err, "at most 10 domains")
}

func TestParseRecency(t *testing.T) {
	for in, want := range map[string]Recency{"": "", " Week ": RecencyWeek, "day": RecencyDay, "MONTH": RecencyMonth, "year": RecencyYear} {
		got, err := ParseRecency(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, err := ParseRecency("fortnight")
	assert.ErrorContains(t, err, "day, week, month or year")
}

// The v1 Extract API rejects unknown fields (422 extra_forbidden), so the body is pinned to
// the documented JSON rather than compared struct-to-struct.
func TestParallelExtract_RequestBody(t *testing.T) {
	for _, tc := range []struct {
		name, objective, wantBody string
	}{
		{"no objective asks for the full page", "", `{"urls":["https://a.example"],"advanced_settings":{"full_content":true}}`},
		{"objective relies on focused excerpts", "who won", `{"urls":["https://a.example"],"objective":"who won"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/extract", r.URL.Path)
				gotBody, _ = io.ReadAll(r.Body)
				_, _ = w.Write([]byte(`{"results":[{"url":"https://a.example","title":"A","excerpts":["e1","e2"],"full_content":null}]}`))
			}))
			defer srv.Close()
			p := NewParallel("pk", "", srv.Client())
			p.baseURL = srv.URL

			page, err := p.Extract(context.Background(), "https://a.example", tc.objective)
			require.NoError(t, err)
			assert.JSONEq(t, tc.wantBody, string(gotBody))
			assert.Equal(t, "e1\n\ne2", page.Content, "excerpts stand in when there is no full content")
		})
	}
}

func TestParallelExtract_FullContentIsTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"url":"https://a.example","title":"A","full_content":"` + strings.Repeat("z", pageMaxRunes+10) + `"}]}`))
	}))
	defer srv.Close()
	p := NewParallel("pk", "turbo", srv.Client())
	p.baseURL = srv.URL

	page, err := p.Extract(context.Background(), "https://a.example", "")
	require.NoError(t, err)
	assert.True(t, page.Truncated)
	assert.Equal(t, "A", page.Title)
	assert.Len(t, []rune(page.Content), pageMaxRunes+1)
}

func TestParallelExtract_ErrorsWhenNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[],"errors":[{"url":"https://a.example","error_type":"fetch_error","http_status_code":403,"content":null}]}`))
	}))
	defer srv.Close()
	p := NewParallel("pk", "", srv.Client())
	p.baseURL = srv.URL

	_, err := p.Extract(context.Background(), "https://a.example", "why")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not read https://a.example: fetch_error (HTTP 403)")
}

// A real 422 from Parallel. The summary names each offending field instead of echoing a
// byte-truncated slice of the JSON.
func TestHTTPError_SummarizesParallelValidationErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"type":"error","error":{"ref_id":"96547bf1","message":"Request validation error.","detail":{"errors":[{"type":"extra_forbidden","loc":["body","excerpts"],"msg":"Extra inputs are not permitted","input":true},{"type":"extra_forbidden","loc":["body","full_content"],"msg":"Extra inputs are not permitted","input":false}]}}}`))
	}))
	defer srv.Close()
	p := NewParallel("pk", "", srv.Client())
	p.baseURL = srv.URL

	_, err := p.Extract(context.Background(), "https://a.example", "")
	require.Error(t, err)
	assert.Equal(t, "parallel extract: HTTP 422: Request validation error. body.excerpts: Extra inputs are not permitted body.full_content: Extra inputs are not permitted", err.Error())
}

// Auth failures use a different shape: a top-level message.
func TestHTTPError_SummarizesTopLevelMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":16,"message":"Invalid API key (C.1)"}`))
	}))
	defer srv.Close()
	p := NewParallel("bad", "", srv.Client())
	p.baseURL = srv.URL

	_, err := p.Search(context.Background(), Query{Query: "x"})
	require.Error(t, err)
	assert.Equal(t, "parallel search: HTTP 401: Invalid API key (C.1)", err.Error())
}

func TestHTTPErrorsAreBoundedAndOmitKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(strings.Repeat("bad key ", 200)))
	}))
	defer srv.Close()
	p := NewParallel("secret-key", "", srv.Client())
	p.baseURL = srv.URL

	_, err := p.Search(context.Background(), Query{Query: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401")
	assert.NotContains(t, err.Error(), "secret-key")
	assert.Less(t, len([]rune(err.Error())), maxErrorMessage+100)
	assert.True(t, strings.HasSuffix(err.Error(), "…"), "a cut message says so")
}

func TestNew(t *testing.T) {
	_, err := New(Config{ParallelAPIKey: "  "})
	assert.ErrorIs(t, err, ErrNotConfigured)

	svc, err := New(Config{ParallelAPIKey: "p"})
	require.NoError(t, err)
	assert.Equal(t, "parallel", svc.Backend.Name())
	assert.Same(t, svc.Backend, svc.Extractor, "Parallel serves both search and extract")
	parallel, ok := svc.Backend.(*ParallelBackend)
	require.True(t, ok)
	require.NotNil(t, parallel.client)
	assert.Equal(t, defaultTimeout, parallel.client.Timeout, "no client given means a bounded default, never http.DefaultClient")

	custom := &http.Client{Timeout: time.Second}
	svc, err = New(Config{ParallelAPIKey: "p", HTTPClient: custom})
	require.NoError(t, err)
	assert.Same(t, custom, svc.Backend.(*ParallelBackend).client)
}

func TestNormalizeMaxResultsAndTruncate(t *testing.T) {
	assert.Equal(t, DefaultMaxResults, normalizeMaxResults(0))
	assert.Equal(t, MaxResultsLimit, normalizeMaxResults(99))
	assert.Equal(t, 3, normalizeMaxResults(3))
	s, cut := truncateRunes(strings.Repeat("🦊", 5), 3)
	assert.True(t, cut)
	assert.Equal(t, "🦊🦊🦊…", s)
}
