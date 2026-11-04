package mcpclient

import (
	"encoding/json"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const toolErrorFallback = "MCP tool reported an error"

// CallToolResult retains MCP tool status, structured data and generated media.
// Output is the model-facing text; structured content is folded into it only when the result
// carries no text of its own.
type CallToolResult struct {
	Output               string
	IsError              bool
	StructuredContent    json.RawMessage
	GeneratedAttachments []*models.FileAttachment
}

// ToolReportedError is a failure the MCP server reported in a successful reply (isError: true),
// as opposed to a transport or protocol error. It is the model's to read and recover from, not
// a fault in this system, so callers can tell it apart (errors.As) and log it accordingly. Its
// message is whatever the server wrote and can echo user data: do not log it.
type ToolReportedError struct {
	Message string
}

func (e *ToolReportedError) Error() string { return e.Message }

// Err reports a tool-level failure separately from transport and protocol errors, as a
// *ToolReportedError. Detailed invocation methods return these failures in CallToolResult, not
// their error value.
func (r CallToolResult) Err() error {
	if !r.IsError {
		return nil
	}
	if r.Output == "" {
		return &ToolReportedError{Message: toolErrorFallback}
	}
	return &ToolReportedError{Message: r.Output}
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

	callResult := CallToolResult{
		IsError:              parsed.IsError,
		StructuredContent:    parsed.StructuredContent,
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
	// The spec has a tool that returns structuredContent also return it serialized in a text
	// block, and servers that render for the model (mcp-ts-core, for one) send a formatted summary
	// instead. Either way the text is what the model should read, so appending the JSON as well
	// only duplicates it (2x to 9x the tokens on real replies). Use it only when there is no text;
	// it stays available on CallToolResult.StructuredContent either way.
	if len(parsed.StructuredContent) > 0 && len(textParts) == 0 {
		textParts = append(textParts, "Structured content:\n"+string(parsed.StructuredContent))
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
