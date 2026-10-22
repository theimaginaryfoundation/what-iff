package mcpclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const toolErrorFallback = "MCP tool reported an error"

// CallToolResult retains MCP tool status, structured data and generated media.
type CallToolResult struct {
	// Output is model-visible text followed by full structured JSON when present.
	// It is intentionally complete, not a compact summary or a machine-only value;
	// use StructuredContent for machine-readable data and bound downstream logging.
	Output               string
	IsError              bool
	StructuredContent    json.RawMessage
	GeneratedAttachments []*models.FileAttachment
}

// Err reports a tool-level failure separately from transport and protocol errors.
// Detailed invocation methods return these failures in CallToolResult, not their error value.
// The error rendering trims whitespace without modifying Output.
func (r CallToolResult) Err() error {
	if !r.IsError {
		return nil
	}
	output := strings.TrimSpace(r.Output)
	if output == "" {
		return errors.New(toolErrorFallback)
	}
	return errors.New(output)
}

func (c *Client) parseToolCallResult(result json.RawMessage) (CallToolResult, bool) {
	var parsed struct {
		Content           []map[string]any `json:"content"`
		IsError           bool             `json:"isError"`
		StructuredContent json.RawMessage  `json:"structuredContent"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return CallToolResult{}, false
	}
	structured := bytes.TrimSpace(parsed.StructuredContent)
	if bytes.Equal(structured, []byte("null")) {
		structured = nil
	}

	callResult := CallToolResult{
		IsError:              parsed.IsError,
		StructuredContent:    structured,
		GeneratedAttachments: make([]*models.FileAttachment, 0),
	}
	var textParts []string
	for _, block := range parsed.Content {
		blockType := strings.ToLower(strings.TrimSpace(anyString(block["type"])))
		switch blockType {
		case "text":
			if text := strings.TrimSpace(anyString(block["text"])); text != "" {
				textParts = append(textParts, text)
			}
		case "image", "audio", "blob":
			if att := c.attachmentFromContentBlock(blockType, block); att != nil {
				callResult.GeneratedAttachments = append(callResult.GeneratedAttachments, att)
			}
		}
	}
	if len(structured) > 0 {
		textParts = append(textParts, "Structured content:\n"+string(structured))
	}
	callResult.Output = strings.Join(textParts, "\n")
	if callResult.IsError && callResult.Output == "" {
		callResult.Output = toolErrorFallback
	}
	if callResult.Output != "" || len(callResult.GeneratedAttachments) > 0 {
		return callResult, true
	}
	return CallToolResult{}, false
}
