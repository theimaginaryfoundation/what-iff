package datastore

import (
	"context"
	"strings"

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

// Queries that object-storage cleanup needs (storage.ReleaseAttachmentObjects and
// storage.PurgeUserObjects). Attachment rows are deleted by ent cascades that never see the
// object store, so callers read the affected rows first with the List*ObjectRefs methods, delete,
// and then release the keys no remaining row references.

// referencedKeysChunk bounds the IN list per query, well under every driver's parameter limit.
const referencedKeysChunk = 500

// ReferencedFileAttachmentKeys returns the subset of keys that at least one FileAttachment row
// (of any owner) stores as its s3_key. Reference copies share an s3_key with their source row, so
// an object may only be deleted once this no longer reports its key. Implements
// storage.AttachmentKeyRefs.
func (d *Datastore) ReferencedFileAttachmentKeys(ctx context.Context, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	uniq := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, k)
	}
	for start := 0; start < len(uniq); start += referencedKeysChunk {
		end := min(start+referencedKeysChunk, len(uniq))
		found, err := d.dbClient.FileAttachment.Query().
			Where(entfileattachment.S3KeyIn(uniq[start:end]...)).
			Unique(true).
			Select(entfileattachment.FieldS3Key).
			Strings(ctx)
		if err != nil {
			return nil, err
		}
		for _, k := range found {
			out[k] = true
		}
	}
	return out, nil
}

// ListChatFileAttachmentObjectRefs returns the attachment rows a DeleteChat cascade will remove
// (those on the chat's messages), with the fields that locate their objects.
func (d *Datastore) ListChatFileAttachmentObjectRefs(ctx context.Context, userID, chatID uuid.UUID) ([]models.FileAttachment, error) {
	return d.listFileAttachmentObjectRefs(ctx, userID,
		entfileattachment.HasChatMessageWith(entchatmessage.HasChatWith(chat.ID(chatID))))
}

// ListPersonalityFileAttachmentObjectRefs returns the attachment rows a DeletePersonality cascade
// will remove (those uploaded to the personality).
func (d *Datastore) ListPersonalityFileAttachmentObjectRefs(ctx context.Context, userID, personalityID uuid.UUID) ([]models.FileAttachment, error) {
	return d.listFileAttachmentObjectRefs(ctx, userID,
		entfileattachment.HasPersonalityWith(personality.ID(personalityID)))
}

// ListUserFileAttachmentObjectRefs returns every attachment row the user owns. Used by account
// deletion.
func (d *Datastore) ListUserFileAttachmentObjectRefs(ctx context.Context, userID uuid.UUID) ([]models.FileAttachment, error) {
	return d.listFileAttachmentObjectRefs(ctx, userID)
}

func (d *Datastore) listFileAttachmentObjectRefs(ctx context.Context, userID uuid.UUID, preds ...predicate.FileAttachment) ([]models.FileAttachment, error) {
	preds = append(preds, entfileattachment.HasOwnerWith(user.ID(userID)))
	rows, err := d.dbClient.FileAttachment.Query().
		Where(preds...).
		WithOwner().
		WithChatMessage(func(q *ent.ChatMessageQuery) { q.WithChat() }).
		WithPersonality().
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.FileAttachment, 0, len(rows))
	for _, r := range rows {
		out = append(out, *toFileAttachmentModel(r))
	}
	return out, nil
}

// ExistingFileAttachmentIDs returns the subset of ids whose rows still exist. Object cleanup
// checks it after a cascade so a row the cascade did not remove never loses its objects.
// Implements storage.AttachmentKeyRefs.
func (d *Datastore) ExistingFileAttachmentIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for start := 0; start < len(ids); start += referencedKeysChunk {
		end := min(start+referencedKeysChunk, len(ids))
		found, err := d.dbClient.FileAttachment.Query().
			Where(entfileattachment.IDIn(ids[start:end]...)).
			IDs(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range found {
			out[id] = true
		}
	}
	return out, nil
}

// FileAttachmentProviderFileShared reports whether a row other than id carries the same provider
// FileID. Reference copies clone the source's FileID, so deleting the provider file for one row
// would break the others.
func (d *Datastore) FileAttachmentProviderFileShared(ctx context.Context, id uuid.UUID, fileID string) (bool, error) {
	if strings.TrimSpace(fileID) == "" {
		return false, nil
	}
	return d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.FileIDEQ(fileID),
			entfileattachment.IDNEQ(id),
		).
		Exist(ctx)
}
