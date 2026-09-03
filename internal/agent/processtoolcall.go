package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// getAgentToolsList returns the list of agent tools for the given context.
// disabledTools, if non-nil, removes tools whose names appear in the set.
func getAgentToolsList(disabledTools map[string]bool, includeMoodTools bool) []responses.ToolUnionParam {
	specs := tools.AgentFunctionToolSpecs(includeMoodTools)
	out := make([]tools.FunctionToolSpec, 0, len(specs))
	for _, spec := range specs {
		if disabledTools[spec.Name] {
			continue
		}
		out = append(out, spec)
	}
	return tools.OpenAIFunctionTools(out)
}

// dispatchToolUse routes a provider-agnostic ToolUse to the appropriate tool
// implementation. Both the OpenAI and Claude paths converge here since tool
// inputs are normalized to json.RawMessage before reaching this function.
func (a *Agent) dispatchToolUse(ctx context.Context, chatCtx *chatContext, use provider.ToolUse) (string, []*models.FileAttachment, error) {
	handler, ok := a.toolHandlers(chatCtx)[use.Name]
	if !ok {
		return "", nil, fmt.Errorf("unknown tool: %s", use.Name)
	}
	if !json.Valid(use.Input) {
		return "", nil, fmt.Errorf("invalid arguments for tool %q: malformed JSON", use.Name)
	}
	return handler(ctx, use.Input)
}

type toolHandler func(context.Context, []byte) (string, []*models.FileAttachment, error)

func (a *Agent) toolHandlers(chatCtx *chatContext) map[string]toolHandler {
	handlers := map[string]toolHandler{
		tools.UpdateScratchpadToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.scratchpadTool.UpdateScratchpadTool(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.CreateMemoryToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.memoryTool.CreateMemoryTool(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.ListToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.listTool.List(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.ListMoodsToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.listMoodsTool(ctx, chatCtx, input)
			return out, nil, err
		},
		tools.ChangeMoodToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.changeMoodTool(ctx, chatCtx, input)
			return out, nil, err
		},
		tools.RunSubagentToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.runSubagentTool(ctx, chatCtx, input)
			return out, nil, err
		},
		tools.CreateAgentJobToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, err := a.createAgentJobTool(ctx, chatCtx.chat, input)
			return out, nil, err
		},
		tools.GenerateImageToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			return a.generateImageTool(ctx, chatCtx.chat, input)
		},
		tools.RecallToolSpec.Name: func(ctx context.Context, input []byte) (string, []*models.FileAttachment, error) {
			out, memories, attachments, err := a.recallTool.Recall(ctx, chatCtx.chat, input)
			if err == nil && len(memories) > 0 {
				chatCtx.addLoadedMemories(memories)
			}
			return out, attachments, err
		},
	}
	if extraToolHandlersForChat != nil {
		for name, h := range extraToolHandlersForChat(a, chatCtx.chat) {
			if h == nil {
				continue
			}
			handlers[name] = toolHandler(h)
		}
	}
	return handlers
}
