package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
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

func buildCheckpointSummaryRequest(userID uuid.UUID, existingSummary string, src checkpointSummarySource) (provider.UtilityRequest, error) {
	updatePrompt := buildCheckpointConversationSummaryInput(existingSummary)
	req := provider.UtilityRequest{
		Role:            provider.UtilityRoleMemory,
		CallPath:        telemetry.CallPathConversationSummary,
		Instructions:    checkpointConversationSummaryInstructions,
		MaxOutputTokens: checkpointSummaryMaxTokens,
		SafetyUserID:    userID.String(),
	}

	switch {
	case src.PreviousResponseID != nil && strings.TrimSpace(*src.PreviousResponseID) != "":
		req.PreviousResponseID = *src.PreviousResponseID
		req.Input = updatePrompt
	case src.ModelContext != nil:
		req.Context = checkpointArchivalContext(src.ModelContext, src.AssistantReply)
		req.Messages = []provider.UtilityMessage{{Role: provider.RoleUser, Text: updatePrompt}}
	default:
		return provider.UtilityRequest{}, fmt.Errorf("checkpoint summary requires PreviousResponseID or ModelContext")
	}
	return req, nil
}

// summarizeConversationForCheckpoint writes the chat's checkpoint summary, updating the
// existing one, from whichever source the chat's checkpoint path has.
func (a *Agent) summarizeConversationForCheckpoint(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, src checkpointSummarySource) (string, error) {
	var existing string
	if chatCtx != nil && chatCtx.chat != nil {
		existing = chatCtx.chat.CheckpointSummary
	}
	req, err := buildCheckpointSummaryRequest(userID, existing, src)
	if err != nil {
		return "", err
	}

	summary, err := a.CallUtility(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to summarize conversation for checkpoint: %w", err)
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", fmt.Errorf("conversation summarization returned empty summary")
	}
	return summary, nil
}
