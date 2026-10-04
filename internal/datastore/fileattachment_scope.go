package datastore

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	entchatmessage "github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// maxFilesInChatScope bounds ListFileAttachmentsInChatScope so a chat with a very large upload
// history cannot turn one tool call into an unbounded scan.
const maxFilesInChatScope = 200

// ListFileAttachmentsInChatScope returns the files a conversation can see by default: those
// attached to messages in chatID, plus the documents attached directly to personalityID when one
// is given. This is the same scope find_context's file search uses (GetRelatedFileChunks), so the
// agent's exact-text tools and its semantic search agree on what "this conversation's files" means.
//
// Newest first, capped at limit (and at maxFilesInChatScope). Ownership is part of the query, so
// another user's chat or personality yields an empty list rather than an error.
func (d *Datastore) ListFileAttachmentsInChatScope(ctx context.Context, userID, chatID uuid.UUID, personalityID *uuid.UUID, limit int) ([]*models.FileAttachment, error) {
	if limit <= 0 || limit > maxFilesInChatScope {
		limit = maxFilesInChatScope
	}

	scope := []predicate.FileAttachment{
		entfileattachment.HasChatMessageWith(entchatmessage.HasChatWith(chat.ID(chatID))),
	}
	if personalityID != nil && *personalityID != uuid.Nil {
		scope = append(scope, entfileattachment.HasPersonalityWith(personality.ID(*personalityID)))
	}

	rows, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.Or(scope...),
		).
		WithOwner().
		WithChatMessage(func(q *ent.ChatMessageQuery) {
			q.WithChat()
		}).
		WithPersonality().
		Order(ent.Desc(entfileattachment.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*models.FileAttachment, 0, len(rows))
	for _, row := range rows {
		if m := toFileAttachmentModel(row); m != nil {
			out = append(out, m)
		}
	}
	return out, nil
}
