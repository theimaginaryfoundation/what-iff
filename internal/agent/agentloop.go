package agent

import (
	"context"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// handleAgentLoop drives the multi-turn tool-calling loop for any provider.
// It accepts a pre-configured AgentAdapter (which holds all SDK-specific state)
// and iterates until the model produces a final text response or maxToolCallRounds
// is exhausted.
func (a *Agent) handleAgentLoop(
	ctx context.Context,
	chatCtx *chatContext,
	adapter provider.AgentAdapter,
) (*provider.GenerateResponse, []*models.ToolCall, []*models.FileAttachment, error) {
	var allToolCalls []*models.ToolCall
	var allGeneratedAttachments []*models.FileAttachment

	for round := 0; round < maxToolCallRounds; round++ {
		logInferenceCall(ctx, a.logger, chatCtx, adapter, round)
		result, toolUses, err := adapter.Call(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		if result != nil && len(toolUses) > 0 {
			return nil, nil, nil, fmt.Errorf("adapter invariant violated: Call returned both a result and tool uses")
		}

		if len(toolUses) == 0 {
			// Model produced a final answer — no tool calls requested.
			allGeneratedAttachments = a.appendPostToolLoopGeneratedAttachments(ctx, chatCtx, allGeneratedAttachments)
			return result, allToolCalls, allGeneratedAttachments, nil
		}

		toolResults, modelToolCalls, generatedAttachments := a.executeToolUses(ctx, chatCtx, toolUses)
		allToolCalls = append(allToolCalls, modelToolCalls...)
		allGeneratedAttachments = append(allGeneratedAttachments, generatedAttachments...)
		adapter.AppendToolResults(toolResults)

		if round == maxToolCallRounds-1 {
			// Max rounds reached — strip tools and force a prose response.
			final, err := adapter.ForceFinalResponse(ctx)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to force final response: %w", err)
			}
			allGeneratedAttachments = a.appendPostToolLoopGeneratedAttachments(ctx, chatCtx, allGeneratedAttachments)
			return final, allToolCalls, allGeneratedAttachments, nil
		}
	}

	return nil, nil, nil, fmt.Errorf("agent loop ended without a final response after %d rounds", maxToolCallRounds)
}

func (a *Agent) appendPostToolLoopGeneratedAttachments(ctx context.Context, chatCtx *chatContext, existing []*models.FileAttachment) []*models.FileAttachment {
	if chatCtx == nil || chatCtx.chat == nil || additionalGeneratedAttachmentsForChat == nil {
		return existing
	}
	load := additionalGeneratedAttachmentsForChat(a, chatCtx.chat)
	if load == nil {
		return existing
	}
	extra := load(ctx)
	if len(extra) == 0 {
		return existing
	}
	return append(existing, extra...)
}

// executeToolUses executes all tool uses and returns provider-agnostic results
// plus the model-level ToolCall records for DB persistence.
func (a *Agent) executeToolUses(ctx context.Context, chatCtx *chatContext, uses []provider.ToolUse) ([]provider.ToolResult, []*models.ToolCall, []*models.FileAttachment) {
	results := make([]provider.ToolResult, len(uses))
	generatedAttachments := make([][]*models.FileAttachment, len(uses))
	for i, use := range uses {
		results[i], generatedAttachments[i] = a.executeToolUseWithRecovery(ctx, chatCtx, use)
		a.notifyToolUseGeneratedAttachments(chatCtx, use, generatedAttachments[i])
		results[i].Images = toolResultImagesFromAttachments(generatedAttachments[i])
	}

	modelToolCalls := make([]*models.ToolCall, len(results))
	for i, r := range results {
		if r.IsErr {
			a.logger.Error("tool call failed",
				zap.String("tool_id", r.ID),
				zap.String("tool_name", uses[i].Name),
				zap.String("error", r.Output),
			)
		}
		tc := &models.ToolCall{
			ToolName:  uses[i].Name,
			ToolInput: string(uses[i].Input),
		}
		if r.IsErr {
			tc.ToolError = r.Output
		} else {
			tc.ToolOutput = r.Output
		}
		modelToolCalls[i] = tc
	}

	flatAttachments := make([]*models.FileAttachment, 0)
	for _, perTool := range generatedAttachments {
		flatAttachments = append(flatAttachments, perTool...)
	}
	return results, modelToolCalls, flatAttachments
}

func (a *Agent) notifyToolUseGeneratedAttachments(chatCtx *chatContext, use provider.ToolUse, attachments []*models.FileAttachment) {
	if chatCtx == nil || chatCtx.chat == nil || onToolUseGeneratedAttachmentsForChat == nil || len(attachments) == 0 {
		return
	}
	onToolUseGeneratedAttachmentsForChat(a, chatCtx.chat, use, attachments)
}

// executeToolUseWithRecovery executes a single tool use with panic recovery.
func (a *Agent) executeToolUseWithRecovery(ctx context.Context, chatCtx *chatContext, use provider.ToolUse) (result provider.ToolResult, attachments []*models.FileAttachment) {
	result.ID = use.ID

	func() {
		defer func() {
			if r := recover(); r != nil {
				result.IsErr = true
				// Expose only a stable, opaque message to the model — full panic
				// detail stays in the log to avoid leaking internal stack info.
				result.Output = fmt.Sprintf("tool %q encountered an internal error (id: %s)", use.Name, use.ID)
				a.logger.Error("tool call panicked",
					zap.String("tool_id", use.ID),
					zap.String("tool_name", use.Name),
					zap.Any("panic", r),
				)
			}
		}()

		output, generatedAttachments, err := a.dispatchToolUse(ctx, chatCtx, use)
		if err != nil {
			result.IsErr = true
			result.Output = err.Error()
		} else {
			result.Output = output
			attachments = generatedAttachments
		}
	}()

	return result, attachments
}

func logInferenceCall(ctx context.Context, logger *zap.Logger, chatCtx *chatContext, adapter provider.AgentAdapter, round int) {
	var chatMessageID, userID, personalityID, model string
	memoriesCount := 0
	if chatCtx != nil {
		model = chatCtx.model
		memoriesCount = len(chatCtx.memories)
	}
	if chatCtx != nil && chatCtx.chat != nil {
		chatMessageID = chatCtx.chat.ID.String()
		userID = chatCtx.chat.UserID.String()
		personalityID = chatCtx.chat.PersonalityID.String()
	}
	logger.Info("making inference call",
		zap.String("chat_message_id", chatMessageID),
		zap.String("user_id", userID),
		zap.String("personality_id", personalityID),
		zap.String("model", model),
		zap.Int("memories_count", memoriesCount),
		zap.Int("round", round),
	)
}
