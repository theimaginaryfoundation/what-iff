package agent

import (
	"context"
	"net/url"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// MCP elicitation (ADR 0x042): while one of its tools runs, an MCP server may ask the person for
// input (a form) or to do something on a page of its own (a link). The client hands each request
// to mcpElicitor, which shows it as a prompt on the turn and waits for the answer exactly as
// ask_user does. A turn nobody can answer has no elicitor: the client cancels the request.

// maxElicitationsPerTurn bounds how many times connectors may ask the person in one turn.
const maxElicitationsPerTurn = 10

// maxElicitationMessageChars bounds the server's message shown on the card.
const maxElicitationMessageChars = 2000

// mcpElicitor answers elicitation requests for one turn.
type mcpElicitor struct {
	a       *Agent
	chatCtx *chatContext
}

// elicitationContext makes MCP tool calls on ctx ask the person when a server elicits, on a turn
// someone can answer.
func (a *Agent) elicitationContext(ctx context.Context, chatCtx *chatContext) context.Context {
	if chatCtx == nil || chatCtx.promptJob == nil {
		return ctx
	}
	return mcpclient.WithElicitationHandler(ctx, &mcpElicitor{a: a, chatCtx: chatCtx})
}

// Elicit shows the request and waits. Anything the card cannot show (a bad schema, a link that is
// not http(s)) is declined without asking; an unanswered prompt is cancelled.
func (e *mcpElicitor) Elicit(ctx context.Context, server *models.MCPServer, toolName string, req mcpclient.ElicitationRequest) (mcpclient.ElicitationResult, error) {
	if e.chatCtx.elicitations >= maxElicitationsPerTurn {
		return mcpclient.ElicitationResult{Action: string(models.AgentPromptCancel)}, nil
	}
	e.chatCtx.elicitations++
	payload, ok := elicitationPayload(server, toolName, req)
	if !ok {
		e.a.logger.Warn("declined an MCP elicitation the client cannot show",
			zap.String("mode", req.Mode), zap.String("tool", toolName))
		return mcpclient.ElicitationResult{Action: string(models.AgentPromptDecline)}, nil
	}
	out, err := e.a.awaitPrompt(ctx, e.chatCtx, models.AgentPromptSourceElicitation, toolUseIDFrom(ctx), payload)
	if err != nil {
		return mcpclient.ElicitationResult{}, err
	}
	if !out.answered() {
		return mcpclient.ElicitationResult{Action: string(models.AgentPromptCancel)}, nil
	}
	res := mcpclient.ElicitationResult{Action: string(out.response.Action)}
	if out.response.Action == models.AgentPromptAccept && payload.Kind == models.AgentPromptKindForm {
		res.Content = out.response.Content
		if res.Content == nil {
			res.Content = map[string]any{}
		}
	}
	return res, nil
}

// elicitationPayload is the card for a request, and false when it cannot be shown safely.
func elicitationPayload(server *models.MCPServer, toolName string, req mcpclient.ElicitationRequest) (models.AgentPromptPayload, bool) {
	payload := models.AgentPromptPayload{
		Message:  agenttools.TruncateRunes(strings.TrimSpace(req.Message), maxElicitationMessageChars),
		ToolName: toolName,
	}
	if server != nil {
		payload.ServerName = strings.TrimSpace(server.Name)
	}
	switch strings.TrimSpace(req.Mode) {
	case "url":
		u, err := url.Parse(strings.TrimSpace(req.URL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return payload, false
		}
		payload.Kind = models.AgentPromptKindURL
		payload.URL = u.String()
		return payload, true
	case "", "form":
		if _, err := parseElicitationSchema(req.RequestedSchema); err != nil {
			return payload, false
		}
		payload.Kind = models.AgentPromptKindForm
		payload.Schema = req.RequestedSchema
		return payload, true
	}
	return payload, false
}
