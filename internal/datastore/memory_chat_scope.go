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
	return memoryOfChats(chatID)
}

// memoryOfChats is memoryOfChat for any of chatIDs.
func memoryOfChats(chatIDs ...uuid.UUID) predicate.Memory {
	return memory.And(
		memory.HasChatWith(entchat.IDIn(chatIDs...)),
		memory.ScopeIn(memory.ScopeChat, memory.ScopeSummary),
	)
}

// MemoryIDsReadableInChat returns which of ids a sandboxed chatID may still read: the memories of
// the chat itself (MemoryIDsCreatedInChat) and of its weave peers
// (docs/adr/0x030-thread-weaves.md). It backs the re-filtering of memory context persisted on
// earlier turns, so a chat that leaves a weave stops replaying what it read from its peers.
func (d *Datastore) MemoryIDsReadableInChat(ctx context.Context, userID, chatID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	out := map[uuid.UUID]struct{}{}
	if len(ids) == 0 {
		return out, nil
	}
	chats, err := readableChatIDs(ctx, d.dbClient, userID, chatID)
	if err != nil {
		return out, err
	}
	found, err := d.dbClient.Memory.Query().
		Where(memory.IDIn(ids...), memory.HasOwnerWith(user.ID(userID)), memoryOfChats(chats...)).
		IDs(ctx)
	if err != nil {
		return out, err
	}
	for _, id := range found {
		out[id] = struct{}{}
	}
	return out, nil
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
