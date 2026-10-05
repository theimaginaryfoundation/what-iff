package agent

import (
	"context"
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

// NOTE: Some editor lint pipelines analyze single files without package context.
var (
	_ = (*Agent).updateScratchpad
	_ = buildUpdateScratchpadPrompt
)

const scratchpadMaxTokens = 3072

type ScratchpadUpdate struct {
	Content    string
	ResponseID *string
}

var scratchpadUpdateDeveloperMessage = `The current scratchpad is provided at the start of the conversation, in a developer message. Please use it as a reference while generating an updated scratchpad.
Use the conversation context to decide what to store or how to phrase it. `

func (a *Agent) shouldSummarizeScratchpad(scratchpad string) bool {
	tokenCount, err := a.OpenAIProvider.CountTokens(scratchpad)
	if err != nil {
		// if we can't count the tokens, we don't summarize
		return false
	}
	return tokenCount > provider.ScratchpadMaxContentLength
}

// updateScratchpad allows the personality to evolve by updating its own scratchpad
func (a *Agent) updateScratchpad(ctx context.Context, userID uuid.UUID, responseID *string, chatCtx *chatContext) (ScratchpadUpdate, error) {
	if responseID == nil {
		a.logger.Error("response ID is nil, cannot update scratchpad")
		return ScratchpadUpdate{}, fmt.Errorf("response ID is nil, cannot update scratchpad")
	}
	personalityModel, err := a.ds.GetPersonality(ctx, userID, chatCtx.chat.PersonalityID)
	if err != nil {
		a.logger.Error("failed to get personality by ID", zap.Error(err))
		return ScratchpadUpdate{}, fmt.Errorf("failed to get personality by ID: %w", err)
	}
	prompt := buildUpdateScratchpadPrompt(personalityModel)

	params := responses.ResponseNewParams{
		Model:              archivalOpenAIModel,
		SafetyIdentifier:   openai.String(userID.String()),
		PreviousResponseID: openai.String(*responseID),
		MaxOutputTokens:    openai.Int(scratchpadMaxTokens),
		ServiceTier:        responses.ResponseNewParamsServiceTierFlex,
		Instructions:       openai.String(chatCtx.chat.SystemPrompt),

		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(scratchpadUpdateDeveloperMessage, provider.RoleDeveloper),
				responses.ResponseInputItemParamOfMessage(prompt, provider.RoleUser),
			},
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathScratchpad), params)
	if err != nil {
		a.logger.Error("failed to update scratchpad", zap.Error(err))
		return ScratchpadUpdate{}, fmt.Errorf("failed to update scratchpad: %w", err)
	}

	updatedScratchpad := strings.TrimSpace(resp.OutputText())
	if updatedScratchpad == "" {
		a.logger.Warn("scratchpad update returned empty content")
		return ScratchpadUpdate{}, fmt.Errorf("scratchpad update returned empty content")
	}

	// We let OpenAI return more than our max_tokens, for cases where the scratchpad gets clipped. Then summarize it to get it under our token limit.
	if a.shouldSummarizeScratchpad(updatedScratchpad) {
		summarized, err := a.summarizeScratchpad(ctx, userID, responseID, chatCtx, updatedScratchpad)
		if err != nil {
			a.logger.Error("failed to summarize scratchpad; keeping unsummarized version", zap.Error(err))
		} else {
			updatedScratchpad = summarized
		}
	}

	// Update the personality scratchpad in the database
	personality := models.Personality{
		ID:         chatCtx.chat.PersonalityID,
		Scratchpad: updatedScratchpad,
	}

	_, err = a.ds.UpdatePersonalityScratchpad(ctx, userID, personality)
	if err != nil {
		a.logger.Error("failed to save updated scratchpad", zap.Error(err))
		return ScratchpadUpdate{}, fmt.Errorf("failed to save updated scratchpad: %w", err)
	}

	a.logger.Info("successfully updated personality scratchpad",
		zap.String("personality_id", chatCtx.chat.PersonalityID.String()),
		zap.Int("scratchpad_length", len(updatedScratchpad)))
	return ScratchpadUpdate{
		Content:    updatedScratchpad,
		ResponseID: &resp.ID,
	}, nil
}

func buildScratchpadSummarizationInput(scratchpad string) string {
	return fmt.Sprintf("Please compress this scratchpad to fit the token budget while preserving key identity and durable user context.\n\nSCRATCHPAD:\n%s", scratchpad)
}

// summarizeScratchpad rewrites an overlong scratchpad into a smaller form and returns it.
// The caller is responsible for persisting the result.
func (a *Agent) summarizeScratchpad(ctx context.Context, userID uuid.UUID, responseID *string, chatCtx *chatContext, scratchpad string) (string, error) {
	if responseID == nil {
		return "", fmt.Errorf("response ID is nil, cannot summarize scratchpad")
	}
	if chatCtx.chat.PersonalityID == uuid.Nil {
		return "", fmt.Errorf("scratchpad summarization not available without a personality")
	}

	// we always use the mini model for scratchpad summarization because it is faster and cheaper, should not require deep personality context.
	instructions := fmt.Sprintf(scratchpadSummarizeInstructions+"\n\n TOKEN BUDGET: %d", provider.ScratchpadMaxContentLength)
	params := responses.ResponseNewParams{
		Model:              archivalOpenAIModel,
		SafetyIdentifier:   openai.String(userID.String()),
		PreviousResponseID: openai.String(*responseID),
		MaxOutputTokens:    openai.Int(provider.ScratchpadMaxContentLength),
		ServiceTier:        responses.ResponseNewParamsServiceTierFlex,
		Instructions:       openai.String(instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(buildScratchpadSummarizationInput(scratchpad)),
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathScratchpad), params)
	if err != nil {
		return "", fmt.Errorf("failed to summarize scratchpad: %w", err)
	}

	summarized := strings.TrimSpace(resp.OutputText())
	if summarized == "" {
		return "", fmt.Errorf("scratchpad summarization returned empty content")
	}
	return summarized, nil
}

// updateScratchpadClaude updates the personality scratchpad for a Claude-model chat.
// It rebuilds conversation context from the DB (scoped to the current checkpoint period)
// rather than chaining off a PreviousResponseID. Uses a fixed small Claude model for cost and reliability.
func (a *Agent) updateScratchpadClaude(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, modelContext *provider.ModelContext) (ScratchpadUpdate, error) {

	personalityModel, err := a.ds.GetPersonality(ctx, userID, chatCtx.chat.PersonalityID)
	if err != nil {
		a.logger.Error("failed to get personality by ID", zap.Error(err))
		return ScratchpadUpdate{}, fmt.Errorf("failed to get personality by ID: %w", err)
	}
	prompt := buildUpdateScratchpadPrompt(personalityModel)
	if a.ClaudeProvider == nil {
		return ScratchpadUpdate{}, fmt.Errorf("ClaudeProvider is nil")
	}
	modelContext.Append(provider.SegmentKindUserMessage, provider.RoleUser, prompt, false)

	params := modelContext.BuildClaudeParams(archivalClaudeModel)
	params.MaxTokens = int64(scratchpadMaxTokens)

	msg, err := a.ClaudeProvider.Call(telemetry.WithCallPath(ctx, telemetry.CallPathScratchpad), params)
	if err != nil {
		return ScratchpadUpdate{}, fmt.Errorf("Claude scratchpad update failed: %w", err)
	}
	updatedScratchpad := strings.TrimSpace(provider.ExtractClaudeText(msg))

	if updatedScratchpad == "" {
		return ScratchpadUpdate{}, fmt.Errorf("scratchpad update returned empty content")
	}
	if a.shouldSummarizeScratchpad(updatedScratchpad) {

		summarized, err := a.summarizeScratchpadClaude(ctx, userID, updatedScratchpad)
		if err != nil {
			a.logger.Error("failed to summarize Claude scratchpad; keeping unsummarized version", zap.Error(err))
		} else {
			updatedScratchpad = summarized
		}
	}

	_, err = a.ds.UpdatePersonalityScratchpad(ctx, userID, models.Personality{
		ID:         chatCtx.chat.PersonalityID,
		Scratchpad: updatedScratchpad,
	})
	if err != nil {
		return ScratchpadUpdate{}, fmt.Errorf("failed to save updated scratchpad: %w", err)
	}

	a.logger.Info("successfully updated personality scratchpad (Claude)",
		zap.String("personality_id", chatCtx.chat.PersonalityID.String()),
		zap.Int("scratchpad_length", len(updatedScratchpad)))
	// ResponseID is intentionally nil — the Claude path passes scratchpad content
	// explicitly to memory extraction rather than chaining via a response ID.
	return ScratchpadUpdate{Content: updatedScratchpad}, nil
}

// summarizeScratchpadClaude compresses an overlong scratchpad for the Claude archival path.
func (a *Agent) summarizeScratchpadClaude(ctx context.Context, userID uuid.UUID, scratchpad string) (string, error) {
	instructions := fmt.Sprintf(scratchpadSummarizeInstructions+"\n\n TOKEN BUDGET: %d", provider.ScratchpadMaxContentLength)
	input := responses.ResponseInputItemParamOfMessage(buildScratchpadSummarizationInput(scratchpad), provider.RoleUser)

	params := responses.ResponseNewParams{
		Model:            archivalOpenAIModel,
		SafetyIdentifier: openai.String(userID.String()),
		MaxOutputTokens:  openai.Int(provider.ScratchpadMaxContentLength),
		ServiceTier:      responses.ResponseNewParamsServiceTierFlex,
		Instructions:     openai.String(instructions),
		Input:            responses.ResponseNewParamsInputUnion{OfInputItemList: []responses.ResponseInputItemUnionParam{input}},
	}
	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathScratchpad), params)
	if err != nil {
		return "", fmt.Errorf("failed to summarize Claude scratchpad: %w", err)
	}
	summarized := strings.TrimSpace(resp.OutputText())
	if summarized == "" {
		return "", fmt.Errorf("scratchpad summarization returned empty content")
	}
	return summarized, nil
}

func buildUpdateScratchpadPrompt(personality *models.Personality) string {
	var userMessage string
	if personality.Scratchpad != "" {
		userMessage = scratchpadPromptOrDefault(personality)
	} else {
		userMessage = scratchpadInitialPrompt
	}
	return fmt.Sprintf("%s\n\n%s", userMessage, scratchpadUpdatePostamble)
}

func scratchpadPromptOrDefault(personality *models.Personality) string {
	if personality == nil {
		return scratchpadUpdatePrompt
	}
	if personality.ScratchpadUpdatePrompt != "" {
		return personality.ScratchpadUpdatePrompt
	}
	return scratchpadUpdatePrompt
}
