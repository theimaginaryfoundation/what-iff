package datastore

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/fileeditgrant"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// File edit grants (ADR 0x046): a chat's agent may change one of the user's files once the user
// mentions it in a comment on the file from that chat. A grant is on the file's original, so it
// covers the file's chat copies too, and it is for the personality the chat had when it was made.

// GrantFileEdit lets the agent of the user's chat (the personality the chat has now) change the
// user's file fileID (any row of it). created is false when the grant was already there; granting
// it again records the chat's current personality.
func (d *Datastore) GrantFileEdit(ctx context.Context, userID, chatID, fileID uuid.UUID) (created bool, err error) {
	original, err := d.CommentFileID(ctx, userID, fileID)
	if err != nil {
		return false, err
	}
	chat, err := d.dbClient.Chat.Query().Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).WithPersonality().Only(ctx)
	if ent.IsNotFound(err) {
		return false, ErrChatNotFound
	}
	if err != nil {
		return false, err
	}
	personality := chatPersonality(chat)
	existing, err := d.dbClient.FileEditGrant.Query().
		Where(fileeditgrant.FileAttachmentID(original), fileeditgrant.ChatID(chatID)).
		Only(ctx)
	if err == nil {
		update := d.dbClient.FileEditGrant.UpdateOne(existing).ClearPersonalityID()
		if personality != nil {
			update = d.dbClient.FileEditGrant.UpdateOne(existing).SetPersonalityID(*personality)
		}
		return false, update.Exec(ctx)
	}
	if !ent.IsNotFound(err) {
		return false, err
	}
	err = d.dbClient.FileEditGrant.Create().SetFileAttachmentID(original).SetChatID(chatID).SetNillablePersonalityID(personality).Exec(ctx)
	if ent.IsConstraintError(err) {
		return false, nil
	}
	return err == nil, err
}

// chatPersonality is the chat's personality id, nil for a chat with none.
func chatPersonality(chat *ent.Chat) *uuid.UUID {
	if chat.Edges.Personality == nil {
		return nil
	}
	id := chat.Edges.Personality.ID
	return &id
}

// HasFileEditGrant reports whether the agent of the user's chat may change the user's file fileID
// (any row of it): the chat holds a grant on it, made while the chat had the personality it has now.
func (d *Datastore) HasFileEditGrant(ctx context.Context, userID, chatID, fileID uuid.UUID) (bool, error) {
	original, err := d.CommentFileID(ctx, userID, fileID)
	if err != nil {
		return false, err
	}
	grant, err := d.dbClient.FileEditGrant.Query().
		Where(
			fileeditgrant.FileAttachmentID(original),
			fileeditgrant.ChatID(chatID),
			fileeditgrant.HasChatWith(entchat.HasOwnerWith(user.ID(userID))),
		).
		WithChat(func(q *ent.ChatQuery) { q.WithPersonality() }).
		Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := chatPersonality(grant.Edges.Chat)
	switch {
	case now == nil && grant.PersonalityID == nil:
		return true, nil
	case now != nil && grant.PersonalityID != nil:
		return *now == *grant.PersonalityID, nil
	default:
		return false, nil
	}
}

// ListFileEditGrants is the files the agent of the user's chat may change, newest first.
func (d *Datastore) ListFileEditGrants(ctx context.Context, userID, chatID uuid.UUID) ([]models.FileEditGrant, error) {
	rows, err := d.dbClient.FileEditGrant.Query().
		Where(fileeditgrant.ChatID(chatID), fileeditgrant.HasChatWith(entchat.HasOwnerWith(user.ID(userID)))).
		WithFileAttachment().
		Order(ent.Desc(fileeditgrant.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.FileEditGrant, 0, len(rows))
	for _, r := range rows {
		g := models.FileEditGrant{FileID: r.FileAttachmentID, CreatedAt: r.CreatedAt}
		if r.Edges.FileAttachment != nil {
			g.FileName = r.Edges.FileAttachment.Name
		}
		out = append(out, g)
	}
	return out, nil
}

// RevokeFileEdit takes back the grant of the user's file fileID (any row of it) from the agent of
// the user's chat. Revoking one that is not there changes nothing.
func (d *Datastore) RevokeFileEdit(ctx context.Context, userID, chatID, fileID uuid.UUID) error {
	original, err := d.CommentFileID(ctx, userID, fileID)
	if err != nil {
		return err
	}
	_, err = d.dbClient.FileEditGrant.Delete().
		Where(
			fileeditgrant.FileAttachmentID(original),
			fileeditgrant.ChatID(chatID),
			fileeditgrant.HasChatWith(entchat.HasOwnerWith(user.ID(userID))),
		).
		Exec(ctx)
	return err
}
