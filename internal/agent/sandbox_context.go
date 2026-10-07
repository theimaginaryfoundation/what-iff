package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Context assembly for sandboxed chats (models.Chat.Sandboxed).
//
// The flag can change mid-thread, and earlier turns persist what they loaded: memory snippets on
// the user message (additional context) and tool results on the assistant message. Both are
// replayed into later turns, so they are re-filtered against the chat's CURRENT flag here, in the
// one place the replay is assembled. This file also owns the rule that the scratchpad stays out
// (the loader already blanks it; see datastore.toChatModel).

// userNameLineAllowed reports whether a chat is given the owner's name line ("The user's name
// is ...", the USER_NAME additional-context item). A sandboxed chat is one strangers may talk in,
// so it is never given the name, neither fresh nor replayed from an earlier turn.
func userNameLineAllowed(sandboxed bool) bool {
	return !sandboxed
}

// memoryIDLookup reports which of ids were created in chatID (and so are still readable there). It
// is the datastore's MemoryIDsCreatedInChat; tests substitute a fake.
type memoryIDLookup func(ctx context.Context, userID, chatID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error)

// persistedMemoryFilter returns a predicate that keeps a persisted additional-context item only if
// it is still readable in chat, or nil when the chat is not sandboxed (keep everything).
// Memory items are matched by id; one without an id cannot be classified and is dropped (fail
// closed), including a legacy MEMORY item that merely looks like the user-name line: only an item
// explicitly typed USER_NAME (profile data, see models.AdditionalContextTypeUserName) is kept
// without an id, and only when the chat may have the name line at all (not in a sandbox, see
// userNameLineAllowed). The sets are loaded in one query. A failed lookup drops every id-bearing
// memory item.
func (b *messageContextBuilder) persistedMemoryFilter(ctx context.Context, userID uuid.UUID, chat *models.Chat, persisted ...[]models.AdditionalContextItem) func(models.AdditionalContextItem) bool {
	if !chat.IsSandboxed() {
		return nil
	}
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
			lookup = b.ds.MemoryIDsCreatedInChat
		}
		if lookup != nil {
			found, err := lookup(ctx, userID, chat.ID, ids)
			if err != nil {
				b.telemetry.Logger.Warn("sandboxed chat: failed to re-check persisted memory context; dropping it",
					zap.String("chat_id", chat.ID.String()), zap.Error(err))
			} else {
				allowed = found
			}
		}
	}
	return func(it models.AdditionalContextItem) bool {
		if it.Type == models.AdditionalContextTypeUserName {
			return userNameLineAllowed(chat.IsSandboxed())
		}
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

// checkpointSteps decides which archival steps a checkpoint runs for chat. The scratchpad update
// runs only for a chat that is not sandboxed and has a personality: the scratchpad is shared across
// the personality's conversations, so a sandboxed chat never reads or rewrites it. Memory extraction
// runs after a successful scratchpad update, and always in a sandboxed chat (which has no
// scratchpad delta to wait for), so what it learns is still captured, as Chat-scoped memories.
func checkpointSteps(chat *models.Chat, scratchpadWritten bool) (runScratchpad, runExtraction bool) {
	sandboxed := chat.IsSandboxed()
	runScratchpad = chat.PersonalityID != uuid.Nil && !sandboxed
	runExtraction = scratchpadWritten || sandboxed
	return runScratchpad, runExtraction
}

// sandboxScope is the context scope a conversation carved out of a chat (a sub-agent's tool loop,
// say) is given: the parent's sandbox, or the account.
func sandboxScope(sandboxed bool) models.ContextScope {
	if sandboxed {
		return models.ContextScopeSandbox
	}
	return models.ContextScopeAccount
}
