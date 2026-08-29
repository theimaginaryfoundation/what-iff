package agent

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// beginCompactionEvent opens the audit record for a checkpoint, capturing the before-state (old
// summary, old scratchpad), the just-generated new scratchpad, and every loaded memory. It returns
// the new event's ID so merge/link/create events produced during memory compaction can be grouped
// under it. Returns nil (and logs nothing at error level) when chatCtx is nil or the
// record cannot be written — callers treat a nil ID as "not logging this compaction".
//
// hasScratchpad is false when the checkpoint had no personality (so no scratchpad step ran); it keeps
// "scratchpad was genuinely empty" distinct from "no scratchpad captured".
func (a *Agent) beginCompactionEvent(
	ctx context.Context,
	userID uuid.UUID,
	chatCtx *chatContext,
	inferenceModelContext *provider.ModelContext,
	chatID uuid.UUID,
	providerName string,
	reason string,
	assistantMessageID uuid.UUID,
	newScratchpad string,
	hasScratchpad bool,
) *uuid.UUID {
	if chatCtx == nil {
		return nil
	}

	scratchpadExplanation := ""
	if hasScratchpad {
		if explanation, err := a.explainCheckpointChange(ctx, userID, "personality scratchpad", chatCtx.chat.Scratchpad, newScratchpad); err != nil {
			a.logger.Warn("failed to explain scratchpad checkpoint change; continuing without explanation",
				zap.String("chat_id", chatID.String()),
				zap.Error(err))
		} else {
			scratchpadExplanation = explanation
		}
	}

	input := models.CompactionEventInput{
		ChatID:                chatID,
		Provider:              providerName,
		Reason:                reason,
		OldSummary:            chatCtx.chat.CheckpointSummary,
		OldScratchpad:         chatCtx.chat.Scratchpad,
		NewScratchpad:         newScratchpad,
		ScratchpadExplanation: scratchpadExplanation,
		HasScratchpad:         hasScratchpad,
		LoadedMemories:        loadedMemoriesSnapshot(inferenceModelContext, chatCtx.liveMemories),
	}
	if chatCtx.chat.PersonalityID != uuid.Nil {
		pid := chatCtx.chat.PersonalityID
		input.PersonalityID = &pid
	}
	if assistantMessageID != uuid.Nil {
		amid := assistantMessageID
		input.AssistantMessageID = &amid
	}

	event, err := a.ds.CreateCompactionEvent(ctx, userID, input)
	if err != nil {
		a.logger.Warn("failed to record compaction event; continuing checkpoint",
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		return nil
	}
	id := event.ID
	return &id
}

// finishCompactionEvent attaches the post-checkpoint summary and a best-effort transition
// explanation to an open compaction event. No-op when eventID is nil.
func (a *Agent) finishCompactionEvent(ctx context.Context, userID uuid.UUID, eventID *uuid.UUID, newSummary string) {
	if eventID == nil {
		return
	}

	summaryExplanation := ""
	if event, err := a.ds.GetCompactionEvent(ctx, userID, *eventID); err != nil {
		a.logger.Warn("failed to load compaction event for summary explanation; continuing without explanation",
			zap.String("compaction_event_id", eventID.String()),
			zap.Error(err))
	} else if event.OldSummary != nil {
		if explanation, err := a.explainCheckpointChange(ctx, userID, "conversation summary", event.OldSummary.Content, newSummary); err != nil {
			a.logger.Warn("failed to explain summary checkpoint change; continuing without explanation",
				zap.String("compaction_event_id", eventID.String()),
				zap.Error(err))
		} else {
			summaryExplanation = explanation
		}
	}

	if err := a.ds.SetCompactionEventNewSummary(ctx, userID, *eventID, newSummary, summaryExplanation); err != nil {
		a.logger.Warn("failed to attach new summary to compaction event",
			zap.String("compaction_event_id", eventID.String()),
			zap.Error(err))
	}
}

func loadedMemoriesSnapshot(modelContext *provider.ModelContext, liveMemories []*models.Memory) []models.CompactionLoadedMemory {
	candidates := buildMemoryMergeCandidates(modelContext, liveMemories, nil)
	if len(candidates) == 0 {
		return nil
	}
	out := make([]models.CompactionLoadedMemory, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Content) == "" {
			continue
		}
		item := models.CompactionLoadedMemory{
			Content:    candidate.Content,
			Scope:      candidate.Scope,
			Confidence: candidate.Confidence.Float(),
		}
		if candidate.MemoryID != nil && *candidate.MemoryID != uuid.Nil {
			item.MemoryID = candidate.MemoryID
		}
		out = append(out, item)
	}
	return out
}
