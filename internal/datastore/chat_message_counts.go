package datastore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"go.uber.org/zap"
)

// CountAllChatMessages returns the number of chat messages (any origin) in the user's chats,
// capped at the provided limit to avoid expensive full scans for heavy users.
// If the actual count exceeds cap, cap is returned.
func (d *Datastore) CountAllChatMessages(ctx context.Context, userID uuid.UUID, cap int) (int, error) {
	if cap <= 0 {
		return 0, nil
	}
	ids, err := d.dbClient.ChatMessage.Query().
		Where(
			chatmessage.HasChatWith(
				entchat.HasOwnerWith(
					user.ID(userID),
				),
			),
		).
		Limit(cap).
		IDs(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("free_tier.count_failed", "UserID", userID.String()), zap.Error(err))
		return 0, fmt.Errorf("failed to count chat messages: %w", err)
	}
	return len(ids), nil
}

// IsFirstChat reports whether chatID is the first chat owned by userID by creation order.
func (d *Datastore) IsFirstChat(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
	firstChat, err := d.dbClient.Chat.Query().
		Where(entchat.HasOwnerWith(user.ID(userID))).
		Order(ent.Asc(entchat.FieldCreatedAt), ent.Asc(entchat.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return firstChat.ID == chatID, nil
}
