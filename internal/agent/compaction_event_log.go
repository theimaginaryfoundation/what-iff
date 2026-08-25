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

	input := models.CompactionEventInput{
		ChatID:         chatID,
		Provider:       providerName,
		Reason:         reason,
		OldSummary:     chatCtx.chat.CheckpointSummary,
		OldScratchpad:  chatCtx.chat.Scratchpad,
		NewScratchpad:  newScratchpad,
		HasScratchpad:  hasScratchpad,
		LoadedMemories: loadedMemoriesSnapshot(inferenceModelContext, chatCtx.liveMemories),
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

// finishCompactionEvent attaches the post-checkpoint summary to an open compaction event. No-op when
// eventID is nil (compaction was not being logged).
func (a *Agent) finishCompactionEvent(ctx context.Context, userID uuid.UUID, eventID *uuid.UUID, newSummary string) {
	if eventID == nil {
		return
	}
	if err := a.ds.SetCompactionEventNewSummary(ctx, userID, *eventID, newSummary); err != nil {
		a.logger.Warn("failed to attach new summary to compaction event",
			zap.String("compaction_event_id", eventID.String()),
			zap.Error(err))
	}
}

// loadedMemoriesSnapshot flattens the same segment-wide loaded set used by the memory merger into
// the audit shape. ModelContext.MemoryRefs carries prior turns' persisted additional_context;
// liveMemories adds current-turn prefetch and tool-loaded rows that are not in that frozen snapshot.
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
