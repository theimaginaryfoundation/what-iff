package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestHTTPError_SummarizesBraveDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"ErrorResponse","error":{"id":"x","status":429,"code":"RATE_LIMITED","detail":"Request rate limit exceeded for plan."}}`))
	}))
	defer srv.Close()
	b := NewBrave("bk", srv.Client())
	b.baseURL = srv.URL

	_, err := b.Search(context.Background(), Query{Query: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 429: Request rate limit exceeded for plan.")
}

func TestBraveSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/web/search", r.URL.Path)
		assert.Equal(t, "bk", r.Header.Get("X-Subscription-Token"))
		assert.Equal(t, "red fox", r.URL.Query().Get("q"))
		assert.Equal(t, "3", r.URL.Query().Get("count"))
		assert.Equal(t, "true", r.URL.Query().Get("extra_snippets"))
		_, _ = w.Write([]byte(`{"web":{"results":[
			{"title":"The <strong>Red Fox</strong>","url":"https://a.example","description":"Foxes &amp; <strong>kits</strong>","page_age":"2026-03-04T00:00:00","extra_snippets":["more"]}]}}`))
	}))
	defer srv.Close()
	b := NewBrave("bk", srv.Client())
	b.baseURL = srv.URL

	results, err := b.Search(context.Background(), Query{Query: "red fox", MaxResults: 3})
	require.NoError(t, err)
	assert.Equal(t, []Result{{Title: "The Red Fox", URL: "https://a.example", Snippet: "Foxes & kits … more", PublishedAt: "2026-03-04T00:00:00"}}, results)
}

func TestHTTPErrorsAreBoundedAndOmitKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(strings.Repeat("bad key ", 200)))
	}))
	defer srv.Close()
	b := NewBrave("secret-key", srv.Client())
	b.baseURL = srv.URL

	_, err := b.Search(context.Background(), Query{Query: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401")
	assert.NotContains(t, err.Error(), "secret-key")
	assert.Less(t, len([]rune(err.Error())), maxErrorMessage+100)
	assert.True(t, strings.HasSuffix(err.Error(), "…"), "a cut message says so")
}

type stubBackend struct {
	name    string
	results []Result
	err     error
	calls   int
}

func (s *stubBackend) Name() string { return s.name }
func (s *stubBackend) Search(context.Context, Query) ([]Result, error) {
	s.calls++
	return s.results, s.err
}

func TestFallbackBackend(t *testing.T) {
	primary := &stubBackend{name: "p", err: errors.New("down")}
	secondary := &stubBackend{name: "s", results: []Result{{URL: "https://s.example"}}}
	f := &fallbackBackend{primary: primary, secondary: secondary}

	got, err := f.Search(context.Background(), Query{Query: "x"})
	require.NoError(t, err)
	assert.Equal(t, secondary.results, got)

	primary.err = nil
	primary.results = []Result{{URL: "https://p.example"}}
	got, err = f.Search(context.Background(), Query{Query: "x"})
	require.NoError(t, err)
	assert.Equal(t, primary.results, got)
	assert.Equal(t, 1, secondary.calls, "secondary only used when primary fails")

	primary.err, secondary.err = errors.New("down"), errors.New("also down")
	_, err = f.Search(context.Background(), Query{Query: "x"})
	assert.ErrorContains(t, err, "also down")
}

func TestNew_SelectsBackends(t *testing.T) {
	_, err := New(Config{})
	assert.ErrorIs(t, err, ErrNotConfigured)

	_, err = New(Config{Provider: "bing", BraveAPIKey: "b"})
	assert.ErrorContains(t, err, "unknown provider")

	svc, err := New(Config{BraveAPIKey: "b"})
	require.NoError(t, err)
	assert.Equal(t, "brave", svc.Backend.Name())
	assert.Nil(t, svc.Extractor, "Brave has no extract API")

	svc, err = New(Config{ParallelAPIKey: "p", BraveAPIKey: "b"})
	require.NoError(t, err)
	assert.Equal(t, "parallel+brave", svc.Backend.Name())
	assert.NotNil(t, svc.Extractor)

	svc, err = New(Config{Provider: "brave", ParallelAPIKey: "p", BraveAPIKey: "b"})
	require.NoError(t, err)
	assert.Equal(t, "brave+parallel", svc.Backend.Name())
	assert.NotNil(t, svc.Extractor, "extract still available through Parallel")

	svc, err = New(Config{Provider: "brave", ParallelAPIKey: "p"})
	require.NoError(t, err)
	assert.Equal(t, "parallel", svc.Backend.Name(), "falls back to the keyed backend")
}

func TestNormalizeMaxResultsAndTruncate(t *testing.T) {
	assert.Equal(t, DefaultMaxResults, normalizeMaxResults(0))
	assert.Equal(t, MaxResultsLimit, normalizeMaxResults(99))
	assert.Equal(t, 3, normalizeMaxResults(3))
	s, cut := truncateRunes(strings.Repeat("🦊", 5), 3)
	assert.True(t, cut)
	assert.Equal(t, "🦊🦊🦊…", s)
}
