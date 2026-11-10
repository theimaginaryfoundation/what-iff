package datastore

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// FindFileAttachmentsAtPath returns the user's files named name (case-insensitively) directly in
// folder (a normalized path; "" is the top level), as the gallery lists them: reference copies and
// context-only rows are left out. The agent file tools address files by this folder/name path.
func (d *Datastore) FindFileAttachmentsAtPath(ctx context.Context, userID uuid.UUID, folder, name string) ([]*models.FileAttachment, error) {
	rows, err := d.dbClient.FileAttachment.Query().
		Where(galleryFiles(userID, models.GalleryKindAll)...).
		Where(
			entfileattachment.FolderEQ(folder),
			entfileattachment.NameEqualFold(strings.TrimSpace(name)),
		).
		WithOwner().
		WithChatMessage(func(q *ent.ChatMessageQuery) { q.WithChat() }).
		WithPersonality().
		Order(ent.Asc(entfileattachment.FieldCreatedAt), ent.Asc(entfileattachment.FieldID)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to find files at a path", zap.Error(err))
		return nil, err
	}
	out := make([]*models.FileAttachment, 0, len(rows))
	for _, row := range rows {
		out = append(out, toFileAttachmentModel(row))
	}
	return out, nil
}

// SwapAgentFileObject points the user's agent-authored file id at newKey, a newly uploaded version,
// provided it still points at oldKey, and records the change time. It reports whether the swap
// happened: false means another writer changed the file first (or it is gone, or not
// agent-authored), and the caller should drop newKey and re-read. It is SwapFileObject with
// AgentOnly set; an agent write never repoints the chat copies that share the object.
func (d *Datastore) SwapAgentFileObject(ctx context.Context, userID, id uuid.UUID, oldKey, newKey string, sizeBytes int64, sha256Hex string) (bool, error) {
	return d.SwapFileObject(ctx, userID, id, oldKey, newKey, SwapFileObjectOptions{AgentOnly: true, SizeBytes: sizeBytes, SHA256: sha256Hex})
}

// CountAgentFiles returns how many agent-authored files the user holds.
func (d *Datastore) CountAgentFiles(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := d.dbClient.FileAttachment.Query().
		Where(entfileattachment.HasOwnerWith(user.ID(userID)), entfileattachment.OriginEQ(entfileattachment.OriginAgent)).
		Count(ctx)
	if err != nil {
		d.logger.Error("failed to count agent files", zap.Error(err))
	}
	return n, err
}

// ListAgentFilesCreatedInChat returns the agent-authored files written in chatID (by its
// personality or, with none, by the conversation itself), oldest first. They are part of the
// conversation's files (ADR 0x043 §4) even though no message carries them.
//
// Agent files written before created_by_chat_id existed have it NULL and are not returned: which
// conversation wrote them is not recorded anywhere to backfill from. They still reach a thread
// when a message attaches them or find_context recalls them.
func (d *Datastore) ListAgentFilesCreatedInChat(ctx context.Context, userID, chatID uuid.UUID) ([]*models.FileAttachment, error) {
	rows, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.OriginEQ(entfileattachment.OriginAgent),
			entfileattachment.CreatedByChatID(chatID),
		).
		WithOwner().
		Order(ent.Asc(entfileattachment.FieldCreatedAt), ent.Asc(entfileattachment.FieldID)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list a chat's agent files", zap.Error(err))
		return nil, err
	}
	out := make([]*models.FileAttachment, 0, len(rows))
	for _, row := range rows {
		out = append(out, toFileAttachmentModel(row))
	}
	return out, nil
}
