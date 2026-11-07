package modeltypes

import (
	"encoding/json"

	"github.com/google/uuid"
)

// ToolCallApp marks a tool call whose MCP tool has an MCP App (ADR 0x042): an HTML view the chat
// renders in a sandboxed frame. It names where the view comes from and keeps the tool's raw result,
// which the view is sent when it starts.
type ToolCallApp struct {
	// ServerID is the connector the tool and its view belong to.
	ServerID uuid.UUID `json:"server_id"`
	// ResourceURI is the view's ui:// resource on that connector.
	ResourceURI string `json:"resource_uri"`
	// ToolName is the tool's own name on the connector (not the mcp__ name the model used).
	ToolName string `json:"tool_name"`
	// Result is the tool's CallToolResult as the server sent it (content, structuredContent),
	// capped; empty when it was too large to keep.
	Result json.RawMessage `json:"result,omitempty"`
}
