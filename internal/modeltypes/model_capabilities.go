package modeltypes

// ModelCapabilities describes model-specific features that affect both UI
// presentation and runtime tool exposure. Tools is an explicit allowlist when
// non-empty; an empty list preserves the legacy tool_support behavior for
// existing model rows until they are configured more precisely.
type ModelCapabilities struct {
	ToolCalling bool     `json:"tool_calling"`
	Vision      bool     `json:"vision"`
	MCP         bool     `json:"mcp"`
	Tools       []string `json:"tools"`
}
