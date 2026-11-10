package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestElicitationPayload(t *testing.T) {
	server := &models.MCPServer{Name: "GitHub"}
	form := mcpclient.ElicitationRequest{Message: "Which repo?", RequestedSchema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"}}}`)}
	p, ok := elicitationPayload(server, "create_issue", form)
	require.True(t, ok)
	assert.Equal(t, models.AgentPromptKindForm, p.Kind)
	assert.Equal(t, "GitHub", p.ServerName)
	assert.Equal(t, "create_issue", p.ToolName)

	_, ok = elicitationPayload(server, "t", mcpclient.ElicitationRequest{RequestedSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"object"}}}`)})
	assert.False(t, ok, "nested objects are not allowed in elicitation forms")

	p, ok = elicitationPayload(server, "t", mcpclient.ElicitationRequest{Mode: "url", URL: "https://github.com/login/device", Message: "Sign in"})
	require.True(t, ok)
	assert.Equal(t, models.AgentPromptKindURL, p.Kind)
	_, ok = elicitationPayload(server, "t", mcpclient.ElicitationRequest{Mode: "url", URL: "javascript:alert(1)"})
	assert.False(t, ok)
	_, ok = elicitationPayload(server, "t", mcpclient.ElicitationRequest{Mode: "sampling"})
	assert.False(t, ok)
}

func TestMCPElicitor_FormAnswerGoesBackToTheServer(t *testing.T) {
	store := newMemPromptStore()
	a := promptTestAgent(store, time.Minute)
	chatCtx := promptTestChatCtx()
	e := &mcpElicitor{a: a, chatCtx: chatCtx}
	req := mcpclient.ElicitationRequest{Message: "Your name?", RequestedSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name"]}`)}

	done := make(chan mcpclient.ElicitationResult, 1)
	go func() {
		res, err := e.Elicit(withToolUseID(context.Background(), "call_9"), &models.MCPServer{Name: "Demo"}, "greet", req)
		assert.NoError(t, err)
		done <- res
	}()
	p := <-store.created
	assert.Equal(t, models.AgentPromptSourceElicitation, p.Source)
	assert.Equal(t, "call_9", p.ToolCallID)

	_, err := a.AnswerPrompt(context.Background(), chatCtx.userID, chatCtx.chat.ID, p.ID, models.AgentPromptResponse{
		Action: models.AgentPromptAccept, Content: map[string]any{"name": "Gori", "age": float64(3), "extra": "dropped"},
	})
	require.NoError(t, err)
	res := <-done
	assert.Equal(t, "accept", res.Action)
	assert.Equal(t, map[string]any{"name": "Gori", "age": int64(3)}, res.Content)
}

func TestMCPElicitor_InvalidFormAnswerIsRefused(t *testing.T) {
	store := newMemPromptStore()
	a := promptTestAgent(store, time.Minute)
	chatCtx := promptTestChatCtx()
	e := &mcpElicitor{a: a, chatCtx: chatCtx}
	go func() {
		_, _ = e.Elicit(context.Background(), nil, "t", mcpclient.ElicitationRequest{RequestedSchema: json.RawMessage(`{"type":"object","properties":{"n":{"type":"string"}},"required":["n"]}`)})
	}()
	p := <-store.created
	_, err := a.AnswerPrompt(context.Background(), chatCtx.userID, chatCtx.chat.ID, p.ID, models.AgentPromptResponse{Action: models.AgentPromptAccept, Content: map[string]any{}})
	assert.ErrorIs(t, err, ErrInvalidPromptResponse)
	_, _ = a.AnswerPrompt(context.Background(), chatCtx.userID, chatCtx.chat.ID, p.ID, models.AgentPromptResponse{Action: models.AgentPromptDecline})
}

func TestMCPElicitor_ExpiryCancelsAndLimitStopsAsking(t *testing.T) {
	store := newMemPromptStore()
	a := promptTestAgent(store, 20*time.Millisecond)
	chatCtx := promptTestChatCtx()
	e := &mcpElicitor{a: a, chatCtx: chatCtx}
	req := mcpclient.ElicitationRequest{RequestedSchema: json.RawMessage(`{"type":"object","properties":{}}`)}

	res, err := e.Elicit(context.Background(), nil, "t", req)
	require.NoError(t, err)
	assert.Equal(t, "cancel", res.Action)

	chatCtx.elicitations = maxElicitationsPerTurn
	res, err = e.Elicit(context.Background(), nil, "t", req)
	require.NoError(t, err)
	assert.Equal(t, "cancel", res.Action)
	assert.Len(t, store.created, 1, "the limit cancels without asking")
}

func TestElicitationContext_OnlyOnInteractiveTurns(t *testing.T) {
	a := promptTestAgent(newMemPromptStore(), time.Minute)
	chatCtx := promptTestChatCtx()
	ctxWith := a.elicitationContext(context.Background(), chatCtx)
	assert.NotEqual(t, context.Background(), ctxWith)
	chatCtx.promptJob = nil
	ctx := context.Background()
	assert.Equal(t, ctx, a.elicitationContext(ctx, chatCtx))
}
