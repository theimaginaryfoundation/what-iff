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
// Save off: no memories are extracted while the turn is in a checkpoint's window, and create_memory
// is not offered on the turn. Both messages of the turn are flagged ChatMessage.ExcludeFromMemory,
// so a later checkpoint (possibly in another process) knows. The turn can still reach the
// personality's scratchpad (shared across its chats) and the chat's summary (stored as a Summary
// memory that find_context searches), and a later checkpoint's extraction reads both: they are
// load-bearing for a long chat, so they keep running.
//
// Memory extraction runs at a checkpoint, over the conversation since the last one (and the turns
// carried over from before it), not per turn, so it cannot leave one turn out: a checkpoint whose
// window has a flagged turn in it skips extraction altogether. A flagged turn therefore suppresses
// extraction for as long as it is carried over, and the save-on turns in a skipped window are never
// extracted.
//
// Neither switch applies to a message folded into a running turn as a steer: a message with options
// always runs as a turn of its own (turnSendRequest). A retry runs without options, so it recalls as
// usual; save off survives it through the stored flag.

// loadTurnMemoriesFor loads the memory context for the turn chatMessage starts: the usual recall
// (loadTurnMemories), or, with recall off, only the user-name line. userNameLine is the user-name
// line it put first in memories ("" when none), recorded as chatContext.userNameLine.
func (a *Agent) loadTurnMemoriesFor(ctx context.Context, progress *memoryLoadProgress, userID uuid.UUID, chatMessage *models.ChatMessage, chat *models.Chat) (memories []string, liveMemories []*models.Memory, userNameLine string, failed bool) {
	if chatMessage.TurnOptions.RecallsMemory() {
		return a.loadTurnMemories(ctx, progress, userID, chatMessage.ChatID, chat.PersonalityID, chatMessage.Message, chat.IsSandboxed())
	}
	memories = a.userNameLinesWithoutRecall(ctx, userID, chatMessage.ChatID, chat.IsSandboxed())
	return memories, nil, firstUserNameLine(memories), false
}

// userNameLinesWithoutRecall is a recall-off turn's memory context: the user-name line on a chat's
// first message, which is identity rather than a recalled memory, and nothing else. It makes no
// model call. Like recall it is best effort, and adds nothing in mock/local mode (where recall adds
// nothing either), so the two differ by the memories alone. Nothing is retrieved, so the turn shows
// no live "Load Memory" row and its saved reply gets no "Load Memory" tool call either
// (recallOffWithoutMemories).
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

// recallOffWithoutMemories reports whether the turn has recall off and its memory context holds
// nothing but the user-name line (identity, not a recalled memory): then its saved reply records no
// "Load Memory" tool call (memoryToolCallsForChatContext), as nothing was retrieved.
func (c *chatContext) recallOffWithoutMemories() bool {
	if c == nil || c.turnOptions.RecallsMemory() {
		return false
	}
	for _, line := range c.memories {
		if line != c.userNameLine {
			return false
		}
	}
	return true
}

// turnSendRequest settles what a new message's turn options mean for how it is sent. Save off
// flags the message ExcludeFromMemory, so a later checkpoint knows. A message with options is never
// queued as a steer: a running turn would fold its text into that turn (takeSteering), whose
// context was built without the options, so they would be ignored (create_memory still offered,
// the reply unflagged, a checkpoint extracting with the text in context). It runs as a turn of its
// own instead. A message without options is left as it is.
func turnSendRequest(request *models.ChatMessage) {
	if request.TurnOptions == nil {
		return
	}
	request.Steer = false
	if !request.TurnOptions.SavesMemory() {
		request.ExcludeFromMemory = true
	}
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

// checkpointMayExtractMemories is checkpointExtractsMemories, logging a skip. Nil-safe like it: a
// nil chatCtx extracts.
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
