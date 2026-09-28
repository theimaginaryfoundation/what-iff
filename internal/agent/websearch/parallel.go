package websearch

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

const parallelBaseURL = "https://api.parallel.ai/v1"

// ParallelBackend calls the Parallel Search and Extract APIs.
type ParallelBackend struct {
	apiKey  string
	mode    string
	baseURL string
	client  *http.Client
	now     func() time.Time
}

// NewParallel builds a Parallel backend. mode is "turbo", "fast" or "advanced" (default "fast").
func NewParallel(apiKey, mode string, client *http.Client) *ParallelBackend {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "fast"
	}
	return &ParallelBackend{apiKey: apiKey, mode: mode, baseURL: parallelBaseURL, client: client, now: time.Now}
}

func (p *ParallelBackend) Name() string { return "parallel" }

type parallelSearchRequest struct {
	Objective        string                  `json:"objective,omitempty"`
	SearchQueries    []string                `json:"search_queries"`
	Mode             string                  `json:"mode,omitempty"`
	AdvancedSettings *parallelSearchAdvanced `json:"advanced_settings,omitempty"`
}

type parallelSearchAdvanced struct {
	SourcePolicy parallelSourcePolicy `json:"source_policy"`
}

type parallelSourcePolicy struct {
	// AfterDate (YYYY-MM-DD) limits results to content published on or after it. Pages with
	// no known publish date are not excluded.
	AfterDate      string   `json:"after_date,omitempty"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
}

type parallelResult struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	PublishDate *string  `json:"publish_date"`
	Excerpts    []string `json:"excerpts"`
	FullContent *string  `json:"full_content"`
}

type parallelSearchResponse struct {
	Results []parallelResult `json:"results"`
}

func (p *ParallelBackend) Search(ctx context.Context, q Query) ([]Result, error) {
	query := strings.TrimSpace(q.Query)
	if query == "" {
		return nil, fmt.Errorf("parallel: empty query")
	}
	var resp parallelSearchResponse
	req := parallelSearchRequest{Objective: strings.TrimSpace(q.Objective), SearchQueries: []string{query}, Mode: p.mode}
	policy := parallelSourcePolicy{IncludeDomains: q.IncludeDomains, ExcludeDomains: q.ExcludeDomains}
	after := q.PublishedAfter
	if since := q.Recency.since(p.now()); since.After(after) {
		after = since
	}
	if !after.IsZero() {
		policy.AfterDate = after.Format(time.DateOnly)
	}
	if policy.AfterDate != "" || len(policy.IncludeDomains) > 0 || len(policy.ExcludeDomains) > 0 {
		req.AdvancedSettings = &parallelSearchAdvanced{SourcePolicy: policy}
	}
	done := telemetry.Global().TimeDependency(ctx, telemetry.DependencyParallel, "search")
	err := postJSON(ctx, p.client, p.baseURL+"/search", map[string]string{"x-api-key": p.apiKey}, req, &resp)
	done(err)
	if err != nil {
		return nil, fmt.Errorf("parallel search: %w", err)
	}
	limit := normalizeMaxResults(q.MaxResults)
	out := make([]Result, 0, min(limit, len(resp.Results)))
	for _, r := range resp.Results {
		if len(out) == limit {
			break
		}
		snippet, _ := truncateRunes(strings.Join(r.Excerpts, " … "), snippetMaxRunes)
		out = append(out, Result{Title: strings.TrimSpace(r.Title), URL: r.URL, Snippet: snippet, PublishedAt: deref(r.PublishDate)})
	}
	return out, nil
}

// parallelExtractRequest is the v1 Extract body. Excerpts always come back; full page
// text is opt-in under advanced_settings.
type parallelExtractRequest struct {
	URLs             []string                 `json:"urls"`
	Objective        string                   `json:"objective,omitempty"`
	AdvancedSettings *parallelExtractAdvanced `json:"advanced_settings,omitempty"`
}

type parallelExtractAdvanced struct {
	FullContent bool `json:"full_content"`
}

type parallelExtractResponse struct {
	Results []parallelResult `json:"results"`
	Errors  []struct {
		URL            string `json:"url"`
		ErrorType      string `json:"error_type"`
		HTTPStatusCode *int   `json:"http_status_code"`
		Content        string `json:"content"`
	} `json:"errors"`
}

func (p *ParallelBackend) Extract(ctx context.Context, url, objective string) (Page, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return Page{}, fmt.Errorf("parallel: empty url")
	}
	objective = strings.TrimSpace(objective)
	// With an objective the excerpts are already focused on it; without one, ask for the
	// whole page so the model has something to read.
	req := parallelExtractRequest{URLs: []string{url}, Objective: objective}
	if objective == "" {
		req.AdvancedSettings = &parallelExtractAdvanced{FullContent: true}
	}
	var resp parallelExtractResponse
	done := telemetry.Global().TimeDependency(ctx, telemetry.DependencyParallel, "extract")
	err := postJSON(ctx, p.client, p.baseURL+"/extract", map[string]string{"x-api-key": p.apiKey}, req, &resp)
	done(err)
	if err != nil {
		return Page{}, fmt.Errorf("parallel extract: %w", err)
	}
	if len(resp.Results) == 0 {
		if len(resp.Errors) > 0 {
			e := resp.Errors[0]
			reason := strings.TrimSpace(e.ErrorType)
			if e.HTTPStatusCode != nil {
				reason = fmt.Sprintf("%s (HTTP %d)", reason, *e.HTTPStatusCode)
			}
			if reason != "" {
				return Page{}, fmt.Errorf("parallel extract: could not read %s: %s", url, reason)
			}
		}
		return Page{}, fmt.Errorf("parallel extract: no content for %s", url)
	}
	r := resp.Results[0]
	text := deref(r.FullContent)
	if strings.TrimSpace(text) == "" {
		text = strings.Join(r.Excerpts, "\n\n")
	}
	content, truncated := truncateRunes(text, pageMaxRunes)
	return Page{URL: r.URL, Title: strings.TrimSpace(r.Title), Content: content, PublishedAt: deref(r.PublishDate), Truncated: truncated}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
