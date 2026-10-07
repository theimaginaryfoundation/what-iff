package datastore

import (
	"context"

	"github.com/google/uuid"

	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
)

// memoryOfChat matches the memories that belong to chatID in the sandbox sense: created in it AND
// scoped to it (Chat, or its checkpoint Summary). The chat edge alone is provenance: a User-scoped
// memory created in the chat is the account's, and a sandbox may neither read nor rewrite it.
func memoryOfChat(chatID uuid.UUID) predicate.Memory {
	return memory.And(
		memory.HasChatWith(entchat.ID(chatID)),
		memory.ScopeIn(memory.ScopeChat, memory.ScopeSummary),
	)
}

// MemoryIDsCreatedInChat returns which of ids are owner-scoped, still exist and belong to chatID:
// its Chat-scoped memories and its own checkpoint summary. A User-scoped memory keeps the chat it
// was created in as provenance, but it is the account's, so it does not count. It backs
// re-filtering of memory context persisted on earlier turns of a sandboxed chat: only memories of
// the chat itself may be replayed there.
func (d *Datastore) MemoryIDsCreatedInChat(ctx context.Context, userID, chatID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	out := map[uuid.UUID]struct{}{}
	if len(ids) == 0 {
		return out, nil
	}
	found, err := d.dbClient.Memory.Query().
		Where(
			memory.IDIn(ids...),
			memory.HasOwnerWith(user.ID(userID)),
			memoryOfChat(chatID),
		).
		IDs(ctx)
	if err != nil {
		return out, err
	}
	for _, id := range found {
		out[id] = struct{}{}
	}
	return out, nil
}
