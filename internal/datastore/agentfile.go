package datastore

import (
	"context"
	"strings"
	"time"

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
// agent-authored), and the caller should drop newKey and re-read.
func (d *Datastore) SwapAgentFileObject(ctx context.Context, userID, id uuid.UUID, oldKey, newKey string) (bool, error) {
	n, err := d.dbClient.FileAttachment.Update().
		Where(
			entfileattachment.ID(id),
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.OriginEQ(entfileattachment.OriginAgent),
			entfileattachment.S3Key(oldKey),
		).
		SetS3Key(newKey).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to swap an agent file's object", zap.Error(err))
		return false, err
	}
	return n == 1, nil
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
