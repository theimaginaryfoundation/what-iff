package websearch

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const braveBaseURL = "https://api.search.brave.com/res/v1"

// BraveBackend calls the Brave Web Search API.
type BraveBackend struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewBrave(apiKey string, client *http.Client) *BraveBackend {
	return &BraveBackend{apiKey: apiKey, baseURL: braveBaseURL, client: client}
}

func (b *BraveBackend) Name() string { return "brave" }

type braveResponse struct {
	Web struct {
		Results []struct {
			Title         string   `json:"title"`
			URL           string   `json:"url"`
			Description   string   `json:"description"`
			PageAge       string   `json:"page_age"`
			ExtraSnippets []string `json:"extra_snippets"`
		} `json:"results"`
	} `json:"web"`
}

func (b *BraveBackend) Search(ctx context.Context, q Query) ([]Result, error) {
	query := strings.TrimSpace(q.Query)
	if query == "" {
		return nil, fmt.Errorf("brave: empty query")
	}
	limit := normalizeMaxResults(q.MaxResults)
	params := url.Values{}
	params.Set("q", query)
	params.Set("count", strconv.Itoa(limit))
	params.Set("extra_snippets", "true")
	var resp braveResponse
	if err := getJSON(ctx, b.client, b.baseURL+"/web/search?"+params.Encode(), map[string]string{"X-Subscription-Token": b.apiKey}, &resp); err != nil {
		return nil, fmt.Errorf("brave search: %w", err)
	}
	out := make([]Result, 0, min(limit, len(resp.Web.Results)))
	for _, r := range resp.Web.Results {
		if len(out) == limit {
			break
		}
		parts := append([]string{r.Description}, r.ExtraSnippets...)
		snippet, _ := truncateRunes(stripTags(strings.Join(parts, " … ")), snippetMaxRunes)
		out = append(out, Result{Title: stripTags(r.Title), URL: r.URL, Snippet: snippet, PublishedAt: strings.TrimSpace(r.PageAge)})
	}
	return out, nil
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// stripTags removes the <strong> highlighting Brave puts in titles and snippets.
func stripTags(s string) string {
	return strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(s, "")))
}
