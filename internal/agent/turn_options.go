package agent

import (
	"context"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Per-turn options (models.TurnOptions) for a user turn: an extension that starts a turn with
// ChatMessage.TurnOptions set gets its pre/post context placed in this turn's model context
// (messageContextBuilder.build), its model used for this turn only, and its output cap on every
// provider request the turn makes. Every ordinary turn has nil options, and then each function
// here leaves the turn exactly as it was.

// prepareUserTurnContext is prepareChatContext for a user's turn: on the chat's own model, or on
// the model the turn's options name, with the options kept on the turn's context.
func (a *Agent) prepareUserTurnContext(ctx context.Context, userID uuid.UUID, chatMessage *models.ChatMessage, memoryProgress *memoryLoadProgress) (*chatContext, error) {
	opts := chatMessage.TurnOptions
	var model *models.Model
	if id := opts.TurnModelID(); id != nil {
		m, err := a.jobModel(ctx, *id)
		if err != nil {
			return nil, err
		}
		model = m
	}
	chatCtx, err := a.prepareChatContextOn(ctx, userID, chatMessage, memoryProgress, model)
	if err != nil {
		return nil, err
	}
	if model != nil {
		runChatOnModel(chatCtx.chat, model)
	}
	chatCtx.turnOptions = opts
	// A retry runs without the options, but its message's flag still keeps it out of memory.
	chatCtx.memorySaveOff = !opts.SavesMemory() || chatMessage.ExcludeFromMemory
	return chatCtx, nil
}

// runChatOnModel points one run's copy of chat at model. Only the copy: the save after inference
// writes continuity only (UpdateChatContinuity), so the chat row keeps its own model.
func runChatOnModel(chat *models.Chat, model *models.Model) {
	chat.ModelID = model.ID
	chat.ModelName = model.Name
	chat.ToolsEnabled = model.ToolSupport
}

// turnMaxOutputTokens is the output cap the turn's options set, or 0 for the usual cap.
func (c *chatContext) turnMaxOutputTokens() int {
	if c == nil {
		return 0
	}
	return c.turnOptions.TurnMaxOutputTokens()
}

// The applyTurnOutputCap* functions put the turn's output cap, as-is, in the field each request
// type carries its cap in today, replacing whatever cap the request was built with (the default,
// or the one a reasoning effort raised it to). They run last, after every other change to the
// request, on each request a turn builds (an Advanced run's rebuilds too). No cap set: no-op.

func applyTurnOutputCapResponses(params *responses.ResponseNewParams, chatCtx *chatContext) {
	if n := chatCtx.turnMaxOutputTokens(); n > 0 && params != nil {
		params.MaxOutputTokens = openai.Int(int64(n))
	}
}

func applyTurnOutputCapClaude(params *anthropic.MessageNewParams, chatCtx *chatContext) {
	if n := chatCtx.turnMaxOutputTokens(); n > 0 && params != nil {
		params.MaxTokens = int64(n)
	}
}

func applyTurnOutputCapChat(params *openai.ChatCompletionNewParams, chatCtx *chatContext) {
	if n := chatCtx.turnMaxOutputTokens(); n > 0 && params != nil {
		params.MaxCompletionTokens = openai.Int(int64(n))
	}
}
