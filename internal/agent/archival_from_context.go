package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// Archival for chats that cannot chain a PreviousResponseID.
//
// The checkpoint sequence has two shapes. OpenAI chats chain each archival step
// off the response id of the turn that just finished. Every other provider
// rebuilds conversation context from the database and sends it explicitly.
//
// That second shape used to be hardcoded to the Anthropic provider, which was
// correct only by accident: it is also the path Gemini and z.ai chats take, and
// those accounts have no reason to hold an Anthropic key. The result was a chat
// that worked while its memory silently never updated. These functions are the
// OpenAI rendering of the same steps, for chats whose own provider is not
// Anthropic — OpenAI being the one credential every account already needs.
//
// archivalContextItems renders the rebuilt context, then appends the step's own
// instruction turns. Images the Responses API will not accept are stripped
// first: one unsupported attachment otherwise fails the whole call with a 400,
// which is the same guard the checkpoint summariser already applies.
func archivalContextItems(modelContext *provider.ModelContext, extra ...responses.ResponseInputItemUnionParam) []responses.ResponseInputItemUnionParam {
	items := provider.RenderOpenAIInputItems(provider.SanitizeImagesForOpenAIInput(modelContext))
	return append(items, extra...)
}

// updateScratchpadFromContext is updateScratchpad for a chat with no response
// id to chain from. Same model, same prompt, same token budget; the difference
// is that conversation context arrives as explicit input items.
func (a *Agent) updateScratchpadFromContext(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, modelContext *provider.ModelContext) (ScratchpadUpdate, error) {
	if a.OpenAIProvider == nil {
		return ScratchpadUpdate{}, fmt.Errorf("OpenAIProvider is nil, cannot update scratchpad")
	}
	personalityModel, err := a.ds.GetPersonality(ctx, userID, chatCtx.chat.PersonalityID)
	if err != nil {
		a.logger.Error("failed to get personality by ID", zap.Error(err))
		return ScratchpadUpdate{}, fmt.Errorf("failed to get personality by ID: %w", err)
	}
	prompt := buildUpdateScratchpadPrompt(personalityModel)

	params := responses.ResponseNewParams{
		Model:            archivalOpenAIModel,
		SafetyIdentifier: openai.String(userID.String()),
		MaxOutputTokens:  openai.Int(scratchpadMaxTokens),
		ServiceTier:      openAIServiceTier(responses.ResponseNewParamsServiceTierFlex),
		Instructions:     openai.String(chatCtx.chat.SystemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: archivalContextItems(modelContext,
				responses.ResponseInputItemParamOfMessage(scratchpadUpdateDeveloperMessage, provider.RoleDeveloper),
				responses.ResponseInputItemParamOfMessage(prompt, provider.RoleUser),
			),
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathScratchpad), params)
	if err != nil {
		return ScratchpadUpdate{}, fmt.Errorf("failed to update scratchpad: %w", err)
	}

	updatedScratchpad := strings.TrimSpace(resp.OutputText())
	if updatedScratchpad == "" {
		return ScratchpadUpdate{}, fmt.Errorf("scratchpad update returned empty content")
	}
	if a.shouldSummarizeScratchpad(updatedScratchpad) {
		// Reuses the Claude path's summariser, which was already an OpenAI call
		// taking the scratchpad as a plain string rather than as chained context.
		summarized, err := a.summarizeScratchpadClaude(ctx, userID, updatedScratchpad)
		if err != nil {
			a.logger.Error("failed to summarize scratchpad; keeping unsummarized version", zap.Error(err))
		} else {
			updatedScratchpad = summarized
		}
	}

	if _, err := a.ds.UpdatePersonalityScratchpad(ctx, userID, models.Personality{
		ID:         chatCtx.chat.PersonalityID,
		Scratchpad: updatedScratchpad,
	}); err != nil {
		return ScratchpadUpdate{}, fmt.Errorf("failed to save updated scratchpad: %w", err)
	}

	a.logger.Info("successfully updated personality scratchpad (rebuilt context)",
		zap.String("personality_id", chatCtx.chat.PersonalityID.String()),
		zap.Int("scratchpad_length", len(updatedScratchpad)))
	// No ResponseID: like the Claude path, memory extraction receives the
	// scratchpad content explicitly rather than by chaining.
	return ScratchpadUpdate{Content: updatedScratchpad}, nil
}

// extractMemoriesWithScratchpadDeltaFromContext is the memory-extraction step
// for a chat with no response id to chain from. It mirrors the OpenAI path's
// prompt, dedup note and JSON schema; only the delivery of conversation context
// differs.
func (a *Agent) extractMemoriesWithScratchpadDeltaFromContext(ctx context.Context, userID, chatID uuid.UUID, modelContext, inferenceModelContext *provider.ModelContext, chatCtx *chatContext, compactionEventID *uuid.UUID) error {
	if a.OpenAIProvider == nil {
		return fmt.Errorf("OpenAIProvider is nil, cannot extract memories")
	}
	dedupNote := fmt.Sprintf("%s\n\nDO NOT extract memories that would be duplicative of the following memories:\n\n %s",
		memoryExtractionDeveloperMessage, strings.Join(chatCtx.memories, "\n\n"))

	params := responses.ResponseNewParams{
		Model:            archivalOpenAIModel,
		SafetyIdentifier: openai.String(userID.String()),
		MaxOutputTokens:  openai.Int(provider.DefaultMaxContentLength),
		ServiceTier:      openAIServiceTier(responses.ResponseNewParamsServiceTierFlex),
		Instructions:     openai.String(chatCtx.chat.SystemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: archivalContextItems(modelContext,
				responses.ResponseInputItemParamOfMessage(dedupNote, provider.RoleDeveloper),
				responses.ResponseInputItemParamOfMessage(memoryWritePromptText(), provider.RoleUser),
			),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "MemoryExtraction",
					Schema:      memoryExtractionSchema,
					Strict:      openai.Bool(true),
					Description: openai.String("Memory Extraction JSON"),
					Type:        "json_schema",
				},
			},
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathMemory), params)
	if err != nil {
		a.logger.Error("memory extraction failed", zap.Error(err))
		return fmt.Errorf("memory extraction failed: %w", err)
	}

	extractedMemories := models.ExtractedMemoryResponse{}
	if err := json.Unmarshal([]byte(resp.OutputText()), &extractedMemories); err != nil {
		a.logger.Error("failed to unmarshal extracted memories", zap.Error(err))
		return fmt.Errorf("failed to unmarshal extracted memories: %w", err)
	}

	a.logger.Info("extracted memories (rebuilt context scratchpad delta)", zap.Int("count", len(extractedMemories.Memories)))
	a.compactMemoriesFromCheckpoint(ctx, userID, chatID, chatCtx.chat.PersonalityID, inferenceModelContext, chatCtx, extractedMemories.Memories, compactionEventID)
	return nil
}

// archivalUsesClaude reports whether the rebuilt-context checkpoint should run
// its scratchpad and memory steps on the Anthropic provider.
//
// Only a genuinely Anthropic chat may: holding one proves the account has an
// Anthropic key. z.ai and Gemini reach the same checkpoint path — z.ai because
// it speaks the Anthropic Messages API, Gemini because it cannot chain a
// response id — but neither implies an Anthropic key, so both archive on
// OpenAI, which every account needs regardless.
func archivalUsesClaude(modelProvider, modelName string) bool {
	return models.IsAnthropicModel(modelProvider, modelName)
}
