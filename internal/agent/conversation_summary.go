package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

const checkpointSummaryMaxTokens = 1200

// checkpointSummarySource selects how conversation context reaches the checkpoint summarizer.
// OpenAI chats chain PreviousResponseID; Claude chats render explicit input items from inference context.
type checkpointSummarySource struct {
	PreviousResponseID *string
	ModelContext       *provider.ModelContext
	AssistantReply     string
}

// checkpointArchivalContext clones inference modelContext and appends the just-completed
// assistant reply. Inference context is built before the assistant response exists; OpenAI
// checkpoint steps chain PreviousResponseID off that response. Claude archival steps need
// the explicit turn to match.
func checkpointArchivalContext(base *provider.ModelContext, assistantReply string) *provider.ModelContext {
	if base == nil {
		return &provider.ModelContext{}
	}
	ctx := base.Clone()
	if trimmed := strings.TrimSpace(assistantReply); trimmed != "" {
		ctx.Append(provider.SegmentKindHistoryTurn, provider.RoleAssistant, trimmed, false)
	}
	return ctx
}

func buildCheckpointConversationSummaryInput(existingSummary string) string {
	if strings.TrimSpace(existingSummary) == "" {
		return "Create a compact summary of the conversation so far that will allow the assistant to continue seamlessly."
	}
	return fmt.Sprintf("Update the existing summary to incorporate new developments in the conversation, keeping it compact.\n\nEXISTING SUMMARY:\n%s", existingSummary)
}

func buildCheckpointSummaryParams(userID uuid.UUID, existingSummary string, src checkpointSummarySource) (responses.ResponseNewParams, error) {
	updatePrompt := buildCheckpointConversationSummaryInput(existingSummary)
	params := responses.ResponseNewParams{
		Model:            archivalOpenAIModel,
		SafetyIdentifier: openai.String(userID.String()),
		MaxOutputTokens:  openai.Int(checkpointSummaryMaxTokens),
		ServiceTier:      responses.ResponseNewParamsServiceTierFlex,
		Instructions:     openai.String(checkpointConversationSummaryInstructions),
	}

	switch {
	case src.PreviousResponseID != nil && strings.TrimSpace(*src.PreviousResponseID) != "":
		params.PreviousResponseID = openai.String(*src.PreviousResponseID)
		params.Input = responses.ResponseNewParamsInputUnion{
			OfString: openai.String(updatePrompt),
		}
	case src.ModelContext != nil:
		// Strip images OpenAI won't accept (unsupported media types, and file_ids whose
		// stored filename extension OpenAI rejects case-sensitively, e.g. ".JPG"). Without
		// this, a single bad image fails the whole checkpoint summary with a 400.
		items := provider.RenderOpenAIInputItems(provider.SanitizeImagesForOpenAIInput(src.ModelContext))
		if trimmed := strings.TrimSpace(src.AssistantReply); trimmed != "" {
			items = append(items, responses.ResponseInputItemParamOfMessage(trimmed, provider.RoleAssistant))
		}
		items = append(items, responses.ResponseInputItemParamOfMessage(updatePrompt, provider.RoleUser))
		params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: items}
	default:
		return responses.ResponseNewParams{}, fmt.Errorf("checkpoint summary requires PreviousResponseID or ModelContext")
	}
	return params, nil
}

func (a *Agent) summarizeConversationForCheckpoint(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, src checkpointSummarySource) (string, error) {
	if a.OpenAIProvider == nil {
		return "", fmt.Errorf("OpenAIProvider is nil, cannot summarize conversation for checkpoint")
	}

	params, err := buildCheckpointSummaryParams(userID, chatCtx.chat.CheckpointSummary, src)
	if err != nil {
		return "", err
	}

	resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathConversationSummary), params)
	if err != nil {
		return "", fmt.Errorf("failed to summarize conversation for checkpoint: %w", err)
	}

	summary := strings.TrimSpace(resp.OutputText())
	if summary == "" {
		return "", fmt.Errorf("conversation summarization returned empty summary")
	}
	return summary, nil
}
