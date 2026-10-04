package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Context assembly for restricted chats (memory sensitivity limit below "sensitive").
//
// The limit can change mid-thread, and earlier turns persist what they loaded: memory snippets on
// the user message (additional context) and tool results on the assistant message. Both are
// replayed into later turns, so they are re-filtered against the CURRENT limit here, in the one
// place the replay is assembled. This file also owns the rule that the scratchpad stays out (the
// loader already blanks it; see datastore.toChatModel).

// memoryIDLookup reports which of ids are still readable under a limit. It is the datastore's
// MemoryIDsWithinSensitivity; tests substitute a fake.
type memoryIDLookup func(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, limit models.MemorySensitivity) (map[uuid.UUID]struct{}, error)

// persistedMemoryFilter returns a predicate that keeps a persisted additional-context item only if
// it is still readable under chat's limit, or nil when the chat is unrestricted (keep everything).
// Memory items are matched by id; one without an id cannot be classified and is dropped (fail
// closed), including a legacy MEMORY item that merely looks like the user-name line: only an item
// explicitly typed USER_NAME (profile data, see models.AdditionalContextTypeUserName) is kept
// without an id. The sets are loaded in one query. A failed lookup drops every id-bearing memory
// item.
func (b *messageContextBuilder) persistedMemoryFilter(ctx context.Context, userID uuid.UUID, chat *models.Chat, persisted ...[]models.AdditionalContextItem) func(models.AdditionalContextItem) bool {
	if !chat.MemoryRestricted() {
		return nil
	}
	limit := chat.MemoryLimit()
	var ids []uuid.UUID
	seen := map[uuid.UUID]struct{}{}
	for _, items := range persisted {
		for _, it := range items {
			if it.Type != models.AdditionalContextTypeMemory || it.MemoryID == nil || *it.MemoryID == uuid.Nil {
				continue
			}
			if _, dup := seen[*it.MemoryID]; !dup {
				seen[*it.MemoryID] = struct{}{}
				ids = append(ids, *it.MemoryID)
			}
		}
	}
	allowed := map[uuid.UUID]struct{}{}
	if len(ids) > 0 {
		lookup := b.memoryIDLookup
		if lookup == nil && b.ds != nil {
			lookup = b.ds.MemoryIDsWithinSensitivity
		}
		if lookup != nil {
			found, err := lookup(ctx, userID, ids, limit)
			if err != nil {
				b.telemetry.Logger.Warn("restricted chat: failed to re-check persisted memory context; dropping it",
					zap.String("chat_id", chat.ID.String()), zap.Error(err))
			} else {
				allowed = found
			}
		}
	}
	return func(it models.AdditionalContextItem) bool {
		if it.Type != models.AdditionalContextTypeMemory {
			return true
		}
		if it.MemoryID == nil {
			return false
		}
		_, ok := allowed[*it.MemoryID]
		return ok
	}
}

// persistedAdditionalContext gathers the persisted memory items of the turns about to be replayed.
func persistedAdditionalContext(carryOver [][2]*models.ChatMessage, history []*models.ChatMessage, current *models.ChatMessage) [][]models.AdditionalContextItem {
	var out [][]models.AdditionalContextItem
	add := func(m *models.ChatMessage) {
		if m != nil && len(m.AdditionalContext) > 0 {
			out = append(out, m.AdditionalContext)
		}
	}
	for _, t := range carryOver {
		add(t[0])
		add(t[1])
	}
	for _, m := range history {
		add(m)
	}
	add(current)
	return out
}

// withoutPersistedToolResults returns turns with every tool call removed, which is what a
// restricted chat replays from earlier turns. It is deliberately not a list of tools known to read
// account data: a ToolCall row records nothing about the limit it ran under or which data it
// touched, and result text can carry anything (an mcp__ connector's output, shell_exec or
// web_search results, a create_memory echo, a tool added tomorrow). Fail closed: a restricted chat
// replays none, and the live tools it calls this turn are already gated. The input messages are
// not mutated.
func withoutPersistedToolResults(turns []*models.ChatMessage) []*models.ChatMessage {
	out := make([]*models.ChatMessage, 0, len(turns))
	for _, msg := range turns {
		if msg == nil || len(msg.ToolCalls) == 0 {
			out = append(out, msg)
			continue
		}
		cp := *msg
		cp.ToolCalls = nil
		out = append(out, &cp)
	}
	return out
}

// checkpointSteps decides which archival steps a checkpoint runs for chat. The scratchpad update
// runs only for an unrestricted chat with a personality: the scratchpad is shared across the
// personality's conversations, so a restricted chat never reads or rewrites it. Memory extraction
// runs after a successful scratchpad update, and always in a restricted chat (which has no
// scratchpad delta to wait for), so what it learns is still captured, capped to its limit.
func checkpointSteps(chat *models.Chat, scratchpadWritten bool) (runScratchpad, runExtraction bool) {
	restricted := chat.MemoryRestricted()
	runScratchpad = chat.PersonalityID != uuid.Nil && !restricted
	runExtraction = scratchpadWritten || restricted
	return runScratchpad, runExtraction
}
