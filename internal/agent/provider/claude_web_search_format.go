package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// ClaudeInLoopWebSearchContextHeader prefixes replay-safe web search text injected
// between agent-loop rounds when native web_search_tool_result blocks cannot be resubmitted.
const ClaudeInLoopWebSearchContextHeader = "[Web search results from the preceding assistant turn]\n"

// FormatClaudeWebSearchResultBlock renders a native web search result block as plain text
// for persistence and in-loop replay. Anthropic returns title/url/page_age plus
// encrypted_content (opaque page text held on Anthropic's side for native multi-turn replay).
func FormatClaudeWebSearchResultBlock(ws anthropic.WebSearchToolResultBlock) string {
	if errResult := ws.Content.AsResponseWebSearchToolResultError(); errResult.ErrorCode != "" {
		return formatClaudeWebSearchError(string(errResult.ErrorCode))
	}
	results := claudeWebSearchResultsFromContent(ws.Content)
	if len(results) > 0 {
		return FormatClaudeWebSearchResults(results)
	}
	raw := strings.TrimSpace(ws.Content.RawJSON())
	if raw == "" || raw == "null" || raw == "[]" {
		return ""
	}
	return formatClaudeWebSearchOpaqueContentNotice()
}

// FormatClaudeBetaWebSearchResultBlock renders a beta web search result block as plain text.
func FormatClaudeBetaWebSearchResultBlock(ws anthropic.BetaWebSearchToolResultBlock) string {
	if errResult := ws.Content.AsResponseWebSearchToolResultError(); errResult.ErrorCode != "" {
		return formatClaudeWebSearchError(string(errResult.ErrorCode))
	}
	results := claudeBetaWebSearchResultsFromContent(ws.Content)
	if len(results) > 0 {
		return FormatClaudeWebSearchResults(results)
	}
	raw := strings.TrimSpace(ws.Content.RawJSON())
	if raw == "" || raw == "null" || raw == "[]" {
		return ""
	}
	return formatClaudeWebSearchOpaqueContentNotice()
}

func formatClaudeWebSearchError(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "Web search failed"
	}
	return fmt.Sprintf("Web search failed (%s)", code)
}

func formatClaudeWebSearchOpaqueContentNotice() string {
	return "Web search completed. Full page text is encrypted by Anthropic and is not shown here."
}

func formatClaudeWebSearchEncryptedOnlyNotice(count int) string {
	if count <= 0 {
		return formatClaudeWebSearchOpaqueContentNotice()
	}
	if count == 1 {
		return "Web search completed (1 result). Full page text is encrypted by Anthropic and is not shown here."
	}
	return fmt.Sprintf("Web search completed (%d results). Full page text is encrypted by Anthropic and is not shown here.", count)
}

// IsClaudeWebSearchEncryptedPlaceholder reports whether s is a summary line for encrypted
// payloads without inline page text (as opposed to a Results: list with titles/URLs).
func IsClaudeWebSearchEncryptedPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if s == formatClaudeWebSearchOpaqueContentNotice() {
		return true
	}
	return strings.HasPrefix(s, "Web search completed") && strings.Contains(s, "encrypted by Anthropic")
}

func claudeWebSearchResultsFromContent(content anthropic.WebSearchToolResultBlockContentUnion) []anthropic.WebSearchResultBlock {
	if results := content.AsWebSearchResultBlockArray(); len(results) > 0 {
		return results
	}
	if len(content.OfWebSearchResultBlockArray) > 0 {
		return content.OfWebSearchResultBlockArray
	}
	return parseClaudeWebSearchResultsRaw(content.RawJSON())
}

func claudeBetaWebSearchResultsFromContent(content anthropic.BetaWebSearchToolResultBlockContentUnion) []anthropic.WebSearchResultBlock {
	if results := content.AsBetaWebSearchResultBlockArray(); len(results) > 0 {
		converted := make([]anthropic.WebSearchResultBlock, 0, len(results))
		for _, r := range results {
			converted = append(converted, anthropic.WebSearchResultBlock{
				Title:            r.Title,
				URL:              r.URL,
				PageAge:          r.PageAge,
				EncryptedContent: r.EncryptedContent,
			})
		}
		return converted
	}
	return parseClaudeWebSearchResultsRaw(content.RawJSON())
}

func parseClaudeWebSearchResultsRaw(raw string) []anthropic.WebSearchResultBlock {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}

	var arr []anthropic.WebSearchResultBlock
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) > 0 {
		return arr
	}

	var single anthropic.WebSearchResultBlock
	if err := json.Unmarshal([]byte(raw), &single); err == nil && claudeWebSearchResultHasData(single) {
		return []anthropic.WebSearchResultBlock{single}
	}

	// Some payloads wrap results under "content".
	var wrapped struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil && len(wrapped.Content) > 0 {
		if nested := parseClaudeWebSearchResultsRaw(string(wrapped.Content)); len(nested) > 0 {
			return nested
		}
	}

	return nil
}

func claudeWebSearchResultHasData(r anthropic.WebSearchResultBlock) bool {
	return strings.TrimSpace(r.Title) != "" ||
		strings.TrimSpace(r.URL) != "" ||
		strings.TrimSpace(r.EncryptedContent) != "" ||
		strings.TrimSpace(r.PageAge) != ""
}

// FormatClaudeWebSearchResults formats parsed web search hits as a bullet list.
func FormatClaudeWebSearchResults(results []anthropic.WebSearchResultBlock) string {
	var b strings.Builder
	var listed, encryptedOnly int
	for _, r := range results {
		title := strings.TrimSpace(r.Title)
		url := strings.TrimSpace(r.URL)
		enc := strings.TrimSpace(r.EncryptedContent)
		if title == "" && url == "" {
			if enc != "" {
				encryptedOnly++
			}
			continue
		}
		if listed == 0 {
			b.WriteString("Results:\n")
		}
		listed++
		if title != "" && url != "" {
			fmt.Fprintf(&b, "- %s (%s)\n", title, url)
		} else if url != "" {
			fmt.Fprintf(&b, "- %s\n", url)
		} else {
			fmt.Fprintf(&b, "- %s\n", title)
		}
		if age := strings.TrimSpace(r.PageAge); age != "" {
			fmt.Fprintf(&b, "  page age: %s\n", age)
		}
	}
	if listed == 0 && encryptedOnly > 0 {
		return formatClaudeWebSearchEncryptedOnlyNotice(encryptedOnly)
	}
	return strings.TrimSpace(b.String())
}
