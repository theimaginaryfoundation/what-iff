package datastore

import (
	"context"

	"github.com/google/uuid"

	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
)

// MemoryIDsCreatedInChat returns which of ids are owner-scoped, still exist and were created in
// chatID. It backs re-filtering of memory context persisted on earlier turns of a chat that has
// since become a sandbox: only memories of the chat itself may be replayed there.
func (d *Datastore) MemoryIDsCreatedInChat(ctx context.Context, userID, chatID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	out := map[uuid.UUID]struct{}{}
	if len(ids) == 0 {
		return out, nil
	}
	found, err := d.dbClient.Memory.Query().
		Where(
			memory.IDIn(ids...),
			memory.HasOwnerWith(user.ID(userID)),
			memory.HasChatWith(entchat.ID(chatID)),
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
