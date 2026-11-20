package agent

import (
	"context"

	"github.com/google/uuid"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Per-turn memory switches (models.TurnOptions.MemoryRecall and MemorySave). Every ordinary turn
// has nil options, and then each function here leaves the turn exactly as it was.
//
// Recall off: the turn retrieves no memories (no memory-query model call, no embedding, no
// lookup), and the memories earlier turns recalled and stored on their messages are not replayed
// into its context. The user-name line on a chat's first message stays: it is identity, not memory.
//
// Save off: both messages of the turn are flagged ChatMessage.ExcludeFromMemory, the turn is not
// offered create_memory, and no checkpoint extracts memories from a conversation that includes a
// flagged message. Memory extraction runs at a checkpoint, over the conversation since the last
// one (and the turns carried over from before it), not per turn, so it cannot leave one turn out:
// a checkpoint whose conversation has a flagged turn in it skips extraction altogether. The
// scratchpad and the checkpoint summary still run, as find_context does: they are load-bearing for
// a long chat and are not long-term memory.

// loadTurnMemoriesFor loads the memory context for the turn chatMessage starts: the usual recall
// (loadTurnMemories), or, with recall off, only the user-name line.
func (a *Agent) loadTurnMemoriesFor(ctx context.Context, progress *memoryLoadProgress, userID uuid.UUID, chatMessage *models.ChatMessage, chat *models.Chat) ([]string, []*models.Memory, bool) {
	if chatMessage.TurnOptions.RecallsMemory() {
		return a.loadTurnMemories(ctx, progress, userID, chatMessage.ChatID, chat.PersonalityID, chatMessage.Message, chat.IsSandboxed())
	}
	return a.userNameLinesWithoutRecall(ctx, userID, chatMessage.ChatID, chat.IsSandboxed()), nil, false
}

// userNameLinesWithoutRecall is a recall-off turn's memory context: the user-name line on a chat's
// first message, which is identity rather than a recalled memory, and nothing else. It makes no
// model call. Like recall it is best effort, and adds nothing in mock/local mode (where recall adds
// nothing either), so the two differ by the memories alone. No "Load Memory" row is shown: nothing
// is retrieved.
func (a *Agent) userNameLinesWithoutRecall(ctx context.Context, userID, chatID uuid.UUID, sandboxed bool) []string {
	if a.ds == nil || !a.memoryEnrichmentRuns() {
		return nil
	}
	lines, err := a.turnUserNameLines(ctx, userID, chatID, sandboxed)
	if err != nil {
		a.logger.Info("could not load the user-name line; proceeding without it",
			zap.String("chat_id", chatID.String()), zap.Error(err))
		return nil
	}
	return lines
}

// withoutRecalledMemories wraps a persisted-context filter (nil keeps everything) so it also drops
// the memories earlier turns recalled (AdditionalContextTypeMemory). The user-name line has its own
// type and stays, as does every other kind of item.
func withoutRecalledMemories(keep func(models.AdditionalContextItem) bool) func(models.AdditionalContextItem) bool {
	return func(it models.AdditionalContextItem) bool {
		if it.Type == models.AdditionalContextTypeMemory {
			return false
		}
		return keep == nil || keep(it)
	}
}

// replaysExcludedTurn reports whether the conversation a turn replays (its checkpoint carry-over
// and history) includes a message flagged ExcludeFromMemory.
func replaysExcludedTurn(carryOver [][2]*models.ChatMessage, history []*models.ChatMessage) bool {
	for _, t := range carryOver {
		if excludedFromMemory(t[0]) || excludedFromMemory(t[1]) {
			return true
		}
	}
	for _, msg := range history {
		if excludedFromMemory(msg) {
			return true
		}
	}
	return false
}

func excludedFromMemory(msg *models.ChatMessage) bool {
	return msg != nil && msg.ExcludeFromMemory
}

// checkpointExtractsMemories reports whether a checkpoint at the end of this turn may extract
// memories: not when the turn itself is kept out of memory, nor when the conversation it replayed
// includes a turn that was (see the note at the top of this file).
func (c *chatContext) checkpointExtractsMemories() bool {
	return c == nil || (!c.memorySaveOff && !c.historyExcludedFromMemory)
}

// checkpointMayExtractMemories is checkpointExtractsMemories, logging a skip.
func (a *Agent) checkpointMayExtractMemories(chatCtx *chatContext, chatID uuid.UUID) bool {
	if chatCtx.checkpointExtractsMemories() {
		return true
	}
	a.logger.Info("checkpoint: skipping memory extraction; the conversation includes a turn sent with memory saving off",
		zap.String("chat_id", chatID.String()))
	return false
}

// disableMemoryWriteTools takes create_memory out of a turn kept out of memory
// (models.TurnOptions.MemorySave false). Other memory tools stay: find_context only reads.
func disableMemoryWriteTools(chatCtx *chatContext, disabledTools map[string]bool) {
	if chatCtx != nil && chatCtx.memorySaveOff {
		disabledTools[agenttools.CreateMemoryToolSpec.Name] = true
	}
}
