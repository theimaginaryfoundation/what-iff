package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// TestOpenAIAdapterAppendToolResults verifies that AppendToolResults correctly
// formats tool results as function-call-output input items, including default
// messages for empty outputs and error handling.
func TestOpenAIAdapterAppendToolResults(t *testing.T) {
	tests := []struct {
		name             string
		results          []provider.ToolResult
		expectedCount    int
		validateMessages func(t *testing.T, messages []responses.ResponseInputItemUnionParam)
	}{
		{
			name: "successful result with output",
			results: []provider.ToolResult{
				{ID: "call1", Output: `{"result": "success"}`, IsErr: false},
			},
			expectedCount: 1,
			validateMessages: func(t *testing.T, messages []responses.ResponseInputItemUnionParam) {
				assert.Equal(t, "call1", messages[0].OfFunctionCallOutput.CallID)
				assert.Equal(t, `{"result": "success"}`, messages[0].OfFunctionCallOutput.Output.OfString.Value)
			},
		},
		{
			name: "successful result with empty output uses default message",
			results: []provider.ToolResult{
				{ID: "call2", Output: "", IsErr: false},
			},
			expectedCount: 1,
			validateMessages: func(t *testing.T, messages []responses.ResponseInputItemUnionParam) {
				assert.Equal(t, "call2", messages[0].OfFunctionCallOutput.CallID)
				assert.Equal(t, `{"message":"Tool executed successfully with no output"}`, messages[0].OfFunctionCallOutput.Output.OfString.Value)
			},
		},
		{
			name: "result with error",
			results: []provider.ToolResult{
				{ID: "call3", Output: "tool execution failed", IsErr: true},
			},
			expectedCount: 1,
			validateMessages: func(t *testing.T, messages []responses.ResponseInputItemUnionParam) {
				assert.Equal(t, "call3", messages[0].OfFunctionCallOutput.CallID)
				assert.Equal(t, "tool execution failed", messages[0].OfFunctionCallOutput.Output.OfString.Value)
			},
		},
		{
			name: "error result with empty message uses default error message",
			results: []provider.ToolResult{
				{ID: "call4", Output: "", IsErr: true},
			},
			expectedCount: 1,
			validateMessages: func(t *testing.T, messages []responses.ResponseInputItemUnionParam) {
				assert.Equal(t, "call4", messages[0].OfFunctionCallOutput.CallID)
				assert.Equal(t, "unknown error occurred", messages[0].OfFunctionCallOutput.Output.OfString.Value)
			},
		},
		{
			name: "multiple mixed results",
			results: []provider.ToolResult{
				{ID: "call5", Output: `{"status": "ok"}`, IsErr: false},
				{ID: "call6", Output: "failed", IsErr: true},
				{ID: "call7", Output: "", IsErr: false},
			},
			expectedCount: 3,
			validateMessages: func(t *testing.T, messages []responses.ResponseInputItemUnionParam) {
				assert.Equal(t, "call5", messages[0].OfFunctionCallOutput.CallID)
				assert.Equal(t, `{"status": "ok"}`, messages[0].OfFunctionCallOutput.Output.OfString.Value)

				assert.Equal(t, "call6", messages[1].OfFunctionCallOutput.CallID)
				assert.Equal(t, "failed", messages[1].OfFunctionCallOutput.Output.OfString.Value)

				assert.Equal(t, "call7", messages[2].OfFunctionCallOutput.CallID)
				assert.Equal(t, `{"message":"Tool executed successfully with no output"}`, messages[2].OfFunctionCallOutput.Output.OfString.Value)
			},
		},
		{
			name:          "empty results",
			results:       []provider.ToolResult{},
			expectedCount: 0,
			validateMessages: func(t *testing.T, messages []responses.ResponseInputItemUnionParam) {
				assert.Empty(t, messages)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := provider.NewOpenAIAdapter(nil, responses.ResponseNewParams{})
			adapter.AppendToolResults(tt.results)
			messages := adapter.CurrentParams().Input.OfInputItemList
			assert.Equal(t, tt.expectedCount, len(messages))
			if tt.validateMessages != nil {
				tt.validateMessages(t, messages)
			}
		})
	}
}

type scriptedAdapter struct {
	callStep         int
	callFn           func(step int) (*provider.GenerateResponse, []provider.ToolUse, error)
	forceFinalFn     func() (*provider.GenerateResponse, error)
	appendedBatches  [][]provider.ToolResult
	forceFinalCalled int
}

func (s *scriptedAdapter) Call(_ context.Context) (*provider.GenerateResponse, []provider.ToolUse, error) {
	resp, uses, err := s.callFn(s.callStep)
	s.callStep++
	return resp, uses, err
}

func (s *scriptedAdapter) AppendToolResults(results []provider.ToolResult) {
	s.appendedBatches = append(s.appendedBatches, results)
}

func (s *scriptedAdapter) ForceFinalResponse(_ context.Context) (*provider.GenerateResponse, error) {
	s.forceFinalCalled++
	if s.forceFinalFn == nil {
		return &provider.GenerateResponse{ID: "forced", Text: "forced-final"}, nil
	}
	return s.forceFinalFn()
}

func (s *scriptedAdapter) WebSearchCompletedCount() int { return 0 }

func (s *scriptedAdapter) SetTextDeltaHandler(_ func(delta string)) {}

func TestHandleAgentLoop_ExecutesToolRoundThenReturnsFinal(t *testing.T) {
	t.Parallel()

	rawInput, _ := json.Marshal(map[string]any{"x": 1})
	adapter := &scriptedAdapter{
		callFn: func(step int) (*provider.GenerateResponse, []provider.ToolUse, error) {
			switch step {
			case 0:
				return nil, []provider.ToolUse{{ID: "tool-1", Name: "unknown_tool", Input: rawInput}}, nil
			default:
				return &provider.GenerateResponse{ID: "r-final", Text: "final-answer"}, nil, nil
			}
		},
	}

	a := &Agent{logger: zap.NewNop()}
	result, toolCalls, generatedAttachments, err := a.handleAgentLoop(context.Background(), &chatContext{}, adapter)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "final-answer", result.Text)
	assert.Len(t, toolCalls, 1)
	assert.Equal(t, "unknown_tool", toolCalls[0].ToolName)
	assert.NotEmpty(t, toolCalls[0].ToolError)
	assert.Empty(t, generatedAttachments)
	assert.Len(t, adapter.appendedBatches, 1)
	assert.Equal(t, 0, adapter.forceFinalCalled)
}

func TestHandleAgentLoop_ForceFinalAfterMaxRounds(t *testing.T) {
	t.Parallel()

	rawInput, _ := json.Marshal(map[string]any{"x": 1})
	adapter := &scriptedAdapter{
		callFn: func(step int) (*provider.GenerateResponse, []provider.ToolUse, error) {
			return nil, []provider.ToolUse{{ID: fmt.Sprintf("tool-%d", step), Name: "unknown_tool", Input: rawInput}}, nil
		},
		forceFinalFn: func() (*provider.GenerateResponse, error) {
			return &provider.GenerateResponse{ID: "forced", Text: "forced-final"}, nil
		},
	}

	a := &Agent{logger: zap.NewNop()}
	result, toolCalls, generatedAttachments, err := a.handleAgentLoop(context.Background(), &chatContext{}, adapter)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "forced-final", result.Text)
	assert.Len(t, toolCalls, maxToolCallRounds)
	assert.Len(t, adapter.appendedBatches, maxToolCallRounds)
	assert.Empty(t, generatedAttachments)
	assert.Equal(t, 1, adapter.forceFinalCalled)
}

func TestExecuteToolUseWithRecovery_RecoversPanics(t *testing.T) {
	a := &Agent{logger: zap.NewNop()}
	args, err := json.Marshal(map[string]any{
		"schedule_input": "daily at 9am",
		"prompt":         "run task",
	})
	assert.NoError(t, err)

	ctx := context.Background()
	use := provider.ToolUse{
		ID:    "panic-tool-id",
		Name:  "create_agent_job",
		Input: args,
	}

	result, attachments := a.executeToolUseWithRecovery(ctx, &chatContext{}, use)
	assert.True(t, result.IsErr)
	assert.Contains(t, result.Output, `tool "create_agent_job" encountered an internal error`)
	assert.Contains(t, result.Output, "panic-tool-id")
	assert.Empty(t, attachments)
}

func TestHandleAgentLoop_AppendsPostToolLoopAttachments(t *testing.T) {
	prev := additionalGeneratedAttachmentsForChat
	t.Cleanup(func() { additionalGeneratedAttachmentsForChat = prev })

	additionalGeneratedAttachmentsForChat = func(_ *Agent, _ *models.Chat) func(context.Context) []*models.FileAttachment {
		return func(context.Context) []*models.FileAttachment {
			return []*models.FileAttachment{{Name: "final.txt", FileType: "text/plain", FileContent: "aGk="}}
		}
	}

	adapter := &scriptedAdapter{
		callFn: func(step int) (*provider.GenerateResponse, []provider.ToolUse, error) {
			return &provider.GenerateResponse{ID: "r-final", Text: "final-answer"}, nil, nil
		},
	}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}
	a := &Agent{logger: zap.NewNop()}

	result, _, generatedAttachments, err := a.handleAgentLoop(context.Background(), &chatContext{chat: chat}, adapter)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, generatedAttachments, 1)
	assert.Equal(t, "final.txt", generatedAttachments[0].Name)
}

func TestExecuteToolUses_NotifiesPerToolGeneratedAttachments(t *testing.T) {
	prev := onToolUseGeneratedAttachmentsForChat
	prevExtra := extraToolHandlersForChat
	t.Cleanup(func() { onToolUseGeneratedAttachmentsForChat = prev })
	t.Cleanup(func() { extraToolHandlersForChat = prevExtra })

	seen := make([]string, 0, 2)
	onToolUseGeneratedAttachmentsForChat = func(_ *Agent, _ *models.Chat, use provider.ToolUse, atts []*models.FileAttachment) {
		seen = append(seen, use.Name)
		assert.NotEmpty(t, atts)
	}
	extraToolHandlersForChat = func(_ *Agent, _ *models.Chat) map[string]ExtraToolHandler {
		return map[string]ExtraToolHandler{
			"fake_tool": func(context.Context, []byte) (string, []*models.FileAttachment, error) {
				return `{"ok":true}`, []*models.FileAttachment{{Name: "out.txt", FileType: "text/plain", FileContent: "aGk="}}, nil
			},
		}
	}

	a := &Agent{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}
	uses := []provider.ToolUse{
		{ID: "u1", Name: "fake_tool", Input: []byte(`{}`)},
	}
	ctx := &chatContext{chat: chat}

	_, _, attachments := a.executeToolUses(context.Background(), ctx, uses)
	assert.NotEmpty(t, attachments)
	assert.Equal(t, []string{"fake_tool"}, seen)
}
