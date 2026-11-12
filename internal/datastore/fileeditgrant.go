package datastore

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/fileeditgrant"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
)

// File edit grants (ADR 0x046): a chat's agent may change one of the user's files once the user
// mentions it in a comment on the file from that chat. A grant is on the file's original, so it
// covers the file's chat copies too.

// GrantFileEdit lets the agent of the user's chat change the user's file fileID (any row of it).
// Granting it again changes nothing.
func (d *Datastore) GrantFileEdit(ctx context.Context, userID, chatID, fileID uuid.UUID) error {
	original, err := d.CommentFileID(ctx, userID, fileID)
	if err != nil {
		return err
	}
	owned, err := d.dbClient.Chat.Query().Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).Exist(ctx)
	if err != nil {
		return err
	}
	if !owned {
		return ErrChatNotFound
	}
	err = d.dbClient.FileEditGrant.Create().SetFileAttachmentID(original).SetChatID(chatID).Exec(ctx)
	if ent.IsConstraintError(err) {
		return nil
	}
	return err
}

// HasFileEditGrant reports whether the agent of the user's chat may change the user's file fileID
// (any row of it).
func (d *Datastore) HasFileEditGrant(ctx context.Context, userID, chatID, fileID uuid.UUID) (bool, error) {
	original, err := d.CommentFileID(ctx, userID, fileID)
	if err != nil {
		return false, err
	}
	return d.dbClient.FileEditGrant.Query().
		Where(
			fileeditgrant.FileAttachmentID(original),
			fileeditgrant.ChatID(chatID),
			fileeditgrant.HasChatWith(entchat.HasOwnerWith(user.ID(userID))),
		).
		Exist(ctx)
}
