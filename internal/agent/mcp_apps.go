package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// MCP Apps (ADR 0x042): an MCP tool that names a ui:// view gets it rendered under the reply. The
// tool call saves where the view comes from and the tool's raw result (ToolCall.App); the chat
// loads the view through MCPAppView and the view's own tool calls go through CallMCPAppTool, both
// re-checked against the account's connector on every request.

// recordMCPApp remembers the MCP App of the tool call id until executeToolUses saves it.
func (c *chatContext) recordMCPApp(toolUseID string, app *models.ToolCallApp) {
	if c == nil || toolUseID == "" || app == nil {
		return
	}
	if c.mcpApps == nil {
		c.mcpApps = map[string]*models.ToolCallApp{}
	}
	c.mcpApps[toolUseID] = app
}

// takeMCPApp returns and forgets the MCP App recorded for the tool call id.
func (c *chatContext) takeMCPApp(toolUseID string) *models.ToolCallApp {
	if c == nil || c.mcpApps == nil {
		return nil
	}
	app := c.mcpApps[toolUseID]
	delete(c.mcpApps, toolUseID)
	return app
}

// mcpAppForResult is the saved App of a tool result with a view. A raw result over
// mcpclient.MaxAppResultBytes is left out: the view still opens, with the input only.
func mcpAppForResult(out mcpclient.CallToolResult) *models.ToolCallApp {
	app := &models.ToolCallApp{ServerID: out.ServerID, ResourceURI: out.UIResourceURI, ToolName: out.ToolName}
	if len(out.Raw) > 0 && len(out.Raw) <= mcpclient.MaxAppResultBytes && json.Valid(out.Raw) {
		app.Result = append(json.RawMessage(nil), out.Raw...)
	}
	return app
}

// MCPAppView is what the chat needs to render a tool call's MCP App.
type MCPAppView struct {
	ToolCallID  uuid.UUID `json:"tool_call_id"`
	ServerName  string    `json:"server_name"`
	ToolName    string    `json:"tool_name"`
	ResourceURI string    `json:"resource_uri"`
	// HTML is the view's document with its Content-Security-Policy already at the top.
	HTML string `json:"html"`
	// CSP is that policy, for display and logging.
	CSP           string          `json:"csp"`
	PrefersBorder *bool           `json:"prefers_border,omitempty"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResult    json.RawMessage `json:"tool_result,omitempty"`
}

var (
	// ErrNoMCPApp: the tool call has no MCP App.
	ErrNoMCPApp = errors.New("this tool call has no app")
	// ErrMCPAppUnavailable: the app's connector is gone or not usable now.
	ErrMCPAppUnavailable = errors.New("this app's connector is not available")
)

// mcpAppCall loads a tool call with an App and its connector, owned by the account.
func (a *Agent) mcpAppCall(ctx context.Context, userID, chatID, toolCallID uuid.UUID) (*models.ToolCall, *models.MCPServer, error) {
	if a.ds == nil || a.mcpClient == nil {
		return nil, nil, ErrMCPAppUnavailable
	}
	tc, err := a.ds.GetChatToolCall(ctx, userID, chatID, toolCallID)
	if err != nil {
		return nil, nil, err
	}
	if tc.App == nil || tc.App.ResourceURI == "" {
		return nil, nil, ErrNoMCPApp
	}
	server, err := a.ds.GetMCPServer(ctx, userID, tc.App.ServerID)
	if errors.Is(err, datastore.ErrMCPServerNotFound) {
		return nil, nil, ErrMCPAppUnavailable
	}
	if err != nil {
		return nil, nil, err
	}
	return tc, server, nil
}

// MCPAppView fetches a tool call's view from its connector, wrapped in its security policy.
func (a *Agent) MCPAppView(ctx context.Context, userID, chatID, toolCallID uuid.UUID) (*MCPAppView, error) {
	tc, server, err := a.mcpAppCall(ctx, userID, chatID, toolCallID)
	if err != nil {
		return nil, err
	}
	res, err := a.mcpClient.ReadAppResource(ctx, server, tc.App.ResourceURI, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMCPAppUnavailable, err)
	}
	policy := mcpclient.AppContentSecurityPolicy(res.CSP)
	input := json.RawMessage(tc.ToolInput)
	if !json.Valid(input) {
		input = json.RawMessage(`{}`)
	}
	return &MCPAppView{
		ToolCallID:    tc.ID,
		ServerName:    server.Name,
		ToolName:      tc.App.ToolName,
		ResourceURI:   tc.App.ResourceURI,
		HTML:          mcpclient.WithCSPMeta(res.HTML, policy),
		CSP:           policy,
		PrefersBorder: res.PrefersBorder,
		ToolInput:     input,
		ToolResult:    tc.App.Result,
	}, nil
}

// CallMCPAppTool runs a tool for a tool call's view, on that view's own connector, and returns the
// raw CallToolResult. The connector decides which tools a view may call (visibility).
func (a *Agent) CallMCPAppTool(ctx context.Context, userID, chatID, toolCallID uuid.UUID, name string, args json.RawMessage) (json.RawMessage, error) {
	_, server, err := a.mcpAppCall(ctx, userID, chatID, toolCallID)
	if err != nil {
		return nil, err
	}
	return a.mcpClient.CallAppTool(ctx, server, name, args, nil)
}
