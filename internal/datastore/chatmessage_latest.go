package datastore

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
)

// GetLatestMessagesForChats returns a map from chat ID to the most recent chat message body
// for each of the supplied chat IDs that the user owns. Chats with no messages or chats not
// owned by the user are simply omitted from the map (callers should treat a missing entry as
// "no snippet available"). The query is owner-scoped as a defense-in-depth measure even when
// the chat IDs originated from an already user-scoped list.
//
// This helper is intentionally light: it runs one indexed lookup per chat ID against the
// (chat, sent_at) index. Callers should keep len(chatIDs) bounded — the cross-resource search
// endpoint caps it at limit_per_type (≤ 25), well within a single request budget.
func (d *Datastore) GetLatestMessagesForChats(ctx context.Context, userID uuid.UUID, chatIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(chatIDs))
	if len(chatIDs) == 0 {
		return out, nil
	}

	for _, chatID := range chatIDs {
		msg, err := d.dbClient.ChatMessage.Query().
			Where(
				chatmessage.HasChatWith(
					entchat.ID(chatID),
					entchat.HasOwnerWith(user.ID(userID)),
				),
			).
			Order(ent.Desc(chatmessage.FieldSentAt)).
			Limit(1).
			First(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			d.logger.Error("failed to fetch latest chat message for snippet",
				zap.String("user_id", userID.String()),
				zap.String("chat_id", chatID.String()),
				zap.Error(err))
			return nil, err
		}
		out[chatID] = msg.Message
	}

	return out, nil
}
