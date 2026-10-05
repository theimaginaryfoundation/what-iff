package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// offering records names as the tools offered to the model for chatCtx, as the real generation
// paths do, so a test can dispatch them. It returns chatCtx for chaining.
func offering(chatCtx *chatContext, names ...string) *chatContext {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	chatCtx.setOfferedTools(set)
	return chatCtx
}

func TestDispatchToolUse_RefusesToolsThatWereNotOffered(t *testing.T) {
	prev := extraToolHandlersForChat
	t.Cleanup(func() { extraToolHandlersForChat = prev })
	called := 0
	extraToolHandlersForChat = func(_ *Agent, _ *models.Chat) map[string]ExtraToolHandler {
		return map[string]ExtraToolHandler{
			"overlay_tool": func(context.Context, []byte) (string, []*models.FileAttachment, error) {
				called++
				return `{"ok":true}`, nil, nil
			},
		}
	}
	a := &Agent{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	use := func(name string) provider.ToolUse { return provider.ToolUse{ID: "t", Name: name, Input: []byte(`{}`)} }

	// A handler exists for overlay_tool and create_agent_job, but the model was only offered
	// overlay_tool: the other call is refused before any handler runs.
	chatCtx := offering(&chatContext{chat: chat}, "overlay_tool")
	_, _, err := a.dispatchToolUse(context.Background(), chatCtx, use("create_agent_job"))
	require.ErrorContains(t, err, "not available in this conversation")
	out, _, err := a.dispatchToolUse(context.Background(), chatCtx, use("overlay_tool"))
	require.NoError(t, err)
	require.Contains(t, out, "ok")
	require.Equal(t, 1, called)

	// An mcp__ name is subject to the same gate: it must be among the offered specs.
	_, _, err = a.dispatchToolUse(context.Background(), chatCtx, use("mcp__srv__tool"))
	require.ErrorContains(t, err, "not available in this conversation")

	// Nothing recorded (or "no tools offered") refuses everything: fail closed.
	_, _, err = a.dispatchToolUse(context.Background(), &chatContext{chat: chat}, use("overlay_tool"))
	require.ErrorContains(t, err, "not available in this conversation")
	_, _, err = a.dispatchToolUse(context.Background(), offering(&chatContext{chat: chat}), use("overlay_tool"))
	require.ErrorContains(t, err, "not available in this conversation")
	require.Equal(t, 1, called, "no refused call reached a handler")
}

func TestOfferedToolNames_MatchesThePolicyFilter(t *testing.T) {
	specs := []agenttools.FunctionToolSpec{{Name: "a"}, {Name: "b"}, {Name: "mcp__s__t"}}
	got := offeredToolNames(specs, map[string]bool{"b": true})
	require.Equal(t, map[string]struct{}{"a": {}, "mcp__s__t": {}}, got)
}

// The sub-agent and main loops both go through handleAgentLoop, so a model that emits a tool it
// was not offered gets an error result and the loop continues.
func TestHandleAgentLoop_UnofferedToolCallBecomesAnErrorResult(t *testing.T) {
	t.Parallel()
	rawInput, _ := json.Marshal(map[string]any{})
	adapter := &scriptedAdapter{
		callFn: func(step int) (*provider.GenerateResponse, []provider.ToolUse, error) {
			if step == 0 {
				return nil, []provider.ToolUse{{ID: "t1", Name: agenttools.CreateAgentJobToolSpec.Name, Input: rawInput}}, nil
			}
			return &provider.GenerateResponse{ID: "r", Text: "done"}, nil, nil
		},
	}
	a := &Agent{logger: zap.NewNop()}
	chatCtx := offering(&chatContext{chat: &models.Chat{ID: uuid.New(), UserID: uuid.New()}}, agenttools.ListToolSpec.Name)
	_, calls, _, err := a.handleAgentLoop(context.Background(), chatCtx, adapter)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Contains(t, calls[0].ToolError, "not available in this conversation")
}

func toolUse(name string, input []byte) provider.ToolUse {
	return provider.ToolUse{ID: "tool-use-1", Name: name, Input: input}
}
