// Package websearch provides first-party web search and page extraction for agent tools
// (ADR 0x021), backed by Parallel. Backend and Extractor are the seam for tests and for any
// future provider.
package websearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultMaxResults is used when a query doesn't ask for a count.
	DefaultMaxResults = 5
	// MaxResultsLimit caps what the model may request per search.
	MaxResultsLimit = 10
	// snippetMaxRunes and pageMaxRunes bound what reaches the model's context.
	snippetMaxRunes = 600
	pageMaxRunes    = 20000

	defaultTimeout = 20 * time.Second
)

// ErrNotConfigured is returned by New when no API key is set.
var ErrNotConfigured = errors.New("websearch: no backend configured")

// Query is one search request.
type Query struct {
	Query string
	// Objective is an optional natural-language description of what the search is for; backends
	// that support it (Parallel) use it to rank and excerpt, others ignore it.
	Objective  string
	MaxResults int
	// Recency, when set, limits results to content published within that window.
	Recency Recency
}

// Recency is a relative publication window for a search ("this week's news").
type Recency string

const (
	RecencyDay   Recency = "day"
	RecencyWeek  Recency = "week"
	RecencyMonth Recency = "month"
	RecencyYear  Recency = "year"
)

// ParseRecency accepts "", day, week, month or year (case-insensitive).
func ParseRecency(s string) (Recency, error) {
	r := Recency(strings.ToLower(strings.TrimSpace(s)))
	switch r {
	case "", RecencyDay, RecencyWeek, RecencyMonth, RecencyYear:
		return r, nil
	}
	return "", fmt.Errorf("recency must be one of day, week, month or year, got %q", s)
}

// since returns the start of the window ending at now.
func (r Recency) since(now time.Time) time.Time {
	switch r {
	case RecencyDay:
		return now.AddDate(0, 0, -1)
	case RecencyWeek:
		return now.AddDate(0, 0, -7)
	case RecencyMonth:
		return now.AddDate(0, -1, 0)
	case RecencyYear:
		return now.AddDate(-1, 0, 0)
	}
	return time.Time{}
}

// Result is one search hit, already trimmed for model consumption.
type Result struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet,omitempty"`
	PublishedAt string `json:"published,omitempty"`
}

// Page is the readable content of one URL.
type Page struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Content     string `json:"content"`
	PublishedAt string `json:"published,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// Backend runs web searches.
type Backend interface {
	Name() string
	Search(ctx context.Context, q Query) ([]Result, error)
}

// Extractor fetches readable page content through the provider (so our servers never fetch
// model-chosen URLs directly).
type Extractor interface {
	Extract(ctx context.Context, url, objective string) (Page, error)
}

// Config configures the Parallel backend. An empty key leaves web search off.
type Config struct {
	ParallelAPIKey string
	// ParallelMode is the Parallel search mode ("turbo", "fast", "advanced"); default "fast".
	ParallelMode string
	HTTPClient   *http.Client
}

// Service is the configured search backend and page extractor.
type Service struct {
	Backend   Backend
	Extractor Extractor
}

// New builds the Service from cfg. It returns ErrNotConfigured when there is no key.
func New(cfg Config) (*Service, error) {
	key := strings.TrimSpace(cfg.ParallelAPIKey)
	if key == "" {
		return nil, ErrNotConfigured
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	parallel := NewParallel(key, cfg.ParallelMode, client)
	return &Service{Backend: parallel, Extractor: parallel}, nil
}

func normalizeMaxResults(n int) int {
	if n <= 0 {
		return DefaultMaxResults
	}
	if n > MaxResultsLimit {
		return MaxResultsLimit
	}
	return n
}

// truncateRunes caps s at max runes (never splitting a multibyte character), reporting whether
// it trimmed.
func truncateRunes(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s, false
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s, false
	}
	return strings.TrimSpace(string(runes[:max])) + "…", true
}
