package agent

import (
	"context"
	"errors"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Per-turn options (models.TurnOptions) for a user turn: an extension that starts a turn with
// ChatMessage.TurnOptions set gets its pre/post context placed in this turn's model context
// (messageContextBuilder.build), its model used for this turn only, and its output cap on every
// request its own model makes. Every ordinary turn has nil options, and then each function here
// leaves the turn exactly as it was.

// ErrTurnModelUnavailable is returned when a turn's options name a model the sender cannot use:
// unknown, retired or deleted, another account's connection model, or an experimental model the
// sender has not turned on. It wraps the datastore's reason (datastore.ErrModelNotFound or
// datastore.ErrExperimentalModelNotAllowed). The turn never falls back to the chat's model.
var ErrTurnModelUnavailable = errors.New("the model this turn asks for is not available")

// turnModel resolves the model a turn's options name, with the check that choosing a chat's model
// runs (datastore.CheckUserCanUseModel). Unlike a job's override (jobModel), a retired model is an
// error, not a fall back to the chat's model: the caller asked for this model.
func (a *Agent) turnModel(ctx context.Context, userID, id uuid.UUID) (*models.Model, error) {
	if err := a.ds.CheckUserCanUseModel(ctx, userID, id); err != nil {
		if errors.Is(err, datastore.ErrModelNotFound) || errors.Is(err, datastore.ErrExperimentalModelNotAllowed) {
			return nil, fmt.Errorf("%w: %w", ErrTurnModelUnavailable, err)
		}
		return nil, fmt.Errorf("failed to check the turn's model: %w", err)
	}
	model, err := a.ds.GetModel(ctx, id)
	if errors.Is(err, datastore.ErrModelNotFound) {
		return nil, fmt.Errorf("%w: %w", ErrTurnModelUnavailable, err)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the turn's model: %w", err)
	}
	return model, nil
}

// prepareUserTurnContext is prepareChatContext for a user's turn: on the chat's own model, or on
// the model the turn's options name, with the options kept on the turn's context.
func (a *Agent) prepareUserTurnContext(ctx context.Context, userID uuid.UUID, chatMessage *models.ChatMessage, memoryProgress *memoryLoadProgress) (*chatContext, error) {
	opts := chatMessage.TurnOptions
	var model *models.Model
	if id := opts.TurnModelID(); id != nil {
		// Checked again at the run: the model may have been retired since the send.
		m, err := a.turnModel(ctx, userID, *id)
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
	return chatCtx, nil
}

// runChatOnModel points one run's copy of chat at model. Only the copy: the save after inference
// writes continuity only (UpdateChatContinuity), so the chat row keeps its own model.
func runChatOnModel(chat *models.Chat, model *models.Model) {
	chat.ModelID = model.ID
	chat.ModelName = model.Name
	chat.ToolsEnabled = model.ToolSupport
}

// turnMaxOutputTokens is the output cap the turn's options set, raised to
// models.MinTurnMaxOutputTokens, or 0 for the usual cap.
func (c *chatContext) turnMaxOutputTokens() int {
	if c == nil {
		return 0
	}
	n := c.turnOptions.TurnMaxOutputTokens()
	if n > 0 && n < models.MinTurnMaxOutputTokens {
		return models.MinTurnMaxOutputTokens
	}
	return n
}

// The applyTurnOutputCap* functions put the turn's output cap in the field each request type
// carries its cap in today, replacing whatever cap the request was built with (the default, or the
// one a reasoning effort raised it to). They run last, after every other change to the request,
// on each request a turn builds with its own model (an Advanced run's rebuilds and z.ai's
// truncation retry too). No cap set: no-op.

func applyTurnOutputCapResponses(params *responses.ResponseNewParams, chatCtx *chatContext) {
	if n := chatCtx.turnMaxOutputTokens(); n > 0 && params != nil {
		params.MaxOutputTokens = openai.Int(int64(n))
	}
}

// claudeMinThinkingBudget is the smallest budget_tokens Anthropic accepts.
const claudeMinThinkingBudget = 1024

func applyTurnOutputCapClaude(params *anthropic.MessageNewParams, chatCtx *chatContext) {
	n := int64(chatCtx.turnMaxOutputTokens())
	if n <= 0 || params == nil {
		return
	}
	params.MaxTokens = n
	// A model that takes a thinking budget needs max_tokens above it (else a 400): lower the
	// budget under the cap, or turn thinking off when the cap leaves no room for the minimum.
	if enabled := params.Thinking.OfEnabled; enabled != nil && n <= enabled.BudgetTokens {
		if n <= claudeMinThinkingBudget {
			params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
		} else {
			enabled.BudgetTokens = n - 1
		}
	}
}

func applyTurnOutputCapChat(params *openai.ChatCompletionNewParams, chatCtx *chatContext) {
	if n := chatCtx.turnMaxOutputTokens(); n > 0 && params != nil {
		params.MaxCompletionTokens = openai.Int(int64(n))
	}
}
