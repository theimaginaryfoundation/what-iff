package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type openAIWebSearchSource struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type openAIWebSearchResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Content string `json:"content"`
	Text    string `json:"text"`
}

type openAIWebSearchCallPayload struct {
	Action struct {
		Type    string                  `json:"type"`
		Query   string                  `json:"query"`
		Queries []string                `json:"queries"`
		Sources []openAIWebSearchSource `json:"sources"`
	} `json:"action"`
	Results []openAIWebSearchResult `json:"results"`
}

// webSearchToolCallsFromOpenAIResponses extracts completed native web_search_call items
// from one or more Responses API payloads observed during a turn.
func webSearchToolCallsFromOpenAIResponses(resps ...*responses.Response) []*models.ToolCall {
	var out []*models.ToolCall
	for _, resp := range resps {
		if resp == nil {
			continue
		}
		citations := extractOpenAIURLCitations(resp)
		for _, item := range resp.Output {
			if item.Type != "web_search_call" {
				continue
			}
			ws := item.AsWebSearchCall()
			if ws.Status != responses.ResponseFunctionWebSearchStatusCompleted {
				continue
			}
			output := formatOpenAIWebSearchOutput(ws, citations)
			if output == "" {
				continue
			}
			input := webSearchInputFromCall(ws)
			out = append(out, &models.ToolCall{
				ToolName:   tools.ToolNameWebSearch,
				ToolInput:  input,
				ToolOutput: output,
			})
		}
	}
	return out
}

func webSearchInputFromCall(ws responses.ResponseFunctionWebSearch) string {
	payload := parseOpenAIWebSearchPayload(ws)
	if len(payload.Action.Queries) > 0 {
		return strings.Join(payload.Action.Queries, "; ")
	}
	if q := strings.TrimSpace(payload.Action.Query); q != "" {
		return q
	}
	if q := strings.TrimSpace(ws.Action.Query); q != "" {
		return q
	}
	if len(ws.Action.Queries) > 0 {
		return strings.Join(ws.Action.Queries, "; ")
	}
	return ""
}

func parseOpenAIWebSearchPayload(ws responses.ResponseFunctionWebSearch) openAIWebSearchCallPayload {
	var payload openAIWebSearchCallPayload
	raw := strings.TrimSpace(ws.RawJSON())
	if raw == "" {
		return payload
	}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload
}

func formatOpenAIWebSearchOutput(ws responses.ResponseFunctionWebSearch, citations []openAIWebSearchSource) string {
	payload := parseOpenAIWebSearchPayload(ws)

	var b strings.Builder
	queries := payload.Action.Queries
	if len(queries) == 0 && strings.TrimSpace(payload.Action.Query) != "" {
		queries = []string{payload.Action.Query}
	}
	if len(queries) == 0 {
		action := ws.Action.AsAny()
		if action != nil {
			switch v := action.(type) {
			case responses.ResponseFunctionWebSearchActionSearch:
				queries = v.Queries
				if len(queries) == 0 && strings.TrimSpace(v.Query) != "" {
					queries = []string{v.Query}
				}
			}
		}
	}
	if len(queries) > 0 {
		fmt.Fprintf(&b, "Queries: %s\n", strings.Join(queries, "; "))
	}

	sources := payload.Action.Sources
	if len(sources) == 0 {
		action := ws.Action.AsAny()
		if srch, ok := action.(responses.ResponseFunctionWebSearchActionSearch); ok {
			for _, src := range srch.Sources {
				sources = append(sources, openAIWebSearchSource{URL: src.URL})
			}
		}
	}
	if len(sources) > 0 {
		b.WriteString("Sources:\n")
		for _, src := range sources {
			appendOpenAIWebSearchSourceLine(&b, src)
		}
	}

	if len(payload.Results) > 0 {
		b.WriteString("\nResults:\n")
		for _, r := range payload.Results {
			appendOpenAIWebSearchResultLine(&b, r)
		}
	}

	if len(citations) > 0 && !strings.Contains(b.String(), "Citations:") {
		b.WriteString("\nCitations:\n")
		for _, c := range citations {
			appendOpenAIWebSearchSourceLine(&b, c)
		}
	}

	return strings.TrimSpace(b.String())
}

func appendOpenAIWebSearchSourceLine(b *strings.Builder, src openAIWebSearchSource) {
	b.WriteString(formatWebSearchTitleURLLine(src.Title, src.URL))
	b.WriteByte('\n')
}

func appendOpenAIWebSearchResultLine(b *strings.Builder, r openAIWebSearchResult) {
	b.WriteString(formatWebSearchTitleURLLine(r.Title, r.URL))
	body := strings.TrimSpace(r.Snippet)
	if body == "" {
		body = strings.TrimSpace(r.Content)
	}
	if body == "" {
		body = strings.TrimSpace(r.Text)
	}
	if body != "" {
		fmt.Fprintf(b, "  %s", body)
	}
	b.WriteByte('\n')
}

func extractOpenAIURLCitations(resp *responses.Response) []openAIWebSearchSource {
	if resp == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []openAIWebSearchSource
	for _, item := range resp.Output {
		if item.Type != "message" {
			continue
		}
		msg := item.AsMessage()
		for _, part := range msg.Content {
			if part.Type != "output_text" {
				continue
			}
			text := part.AsOutputText()
			for _, ann := range text.Annotations {
				cite := ann.AsURLCitation()
				if cite.URL == "" && cite.Title == "" {
					continue
				}
				key := cite.URL + "\x00" + cite.Title
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, openAIWebSearchSource{URL: cite.URL, Title: cite.Title})
			}
		}
	}
	return out
}

func formatWebSearchTitleURLLine(title, url string) string {
	title = strings.TrimSpace(title)
	url = strings.TrimSpace(url)
	switch {
	case title != "" && url != "":
		return fmt.Sprintf("- %s (%s)", title, url)
	case url != "":
		return fmt.Sprintf("- %s", url)
	case title != "":
		return fmt.Sprintf("- %s", title)
	default:
		return ""
	}
}
