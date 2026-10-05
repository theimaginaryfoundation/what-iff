package storage

import (
	"context"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// AttachmentKeyRefs answers what object cleanup needs to know about the attachment rows that
// remain. The datastore implements it; it is an interface so object cleanup can live here without
// importing the datastore (which imports this package).
type AttachmentKeyRefs interface {
	// ReferencedFileAttachmentKeys returns the subset of keys that at least one remaining
	// FileAttachment row (of any owner) stores as its s3_key.
	ReferencedFileAttachmentKeys(ctx context.Context, keys []string) (map[string]bool, error)
	// ExistingFileAttachmentIDs returns the subset of ids whose rows still exist.
	ExistingFileAttachmentIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)
}

// ReleaseResult counts what ReleaseAttachmentObjects did.
type ReleaseResult struct {
	Deleted int // object deletes that succeeded (missing objects count as deleted)
	Kept    int // keys still referenced, plus rows (or, on a failed check, keys) left alone
	Failed  int // object deletes that returned an error
}

// ReleaseAttachmentObjects deletes the stored objects behind attachment rows that have ALREADY
// been deleted from the datastore (directly, or by a chat/personality/user cascade). Call it with
// the rows as they were read before the delete.
//
// It first confirms the rows are gone: a row that still exists (say, an FK cascade that did not
// fire) keeps all its objects. Reference copies (datastore.CreateFileAttachmentReference) share
// an s3_key with the row they were copied from, so a key is only deleted when no remaining row
// references it. Thumbnails are keyed by the attachment's own id and are never shared, so a
// deleted image row's thumbnail always goes.
//
// Best effort: failures are logged and counted, never returned, because the user-facing delete
// has already happened and must not fail on storage; anything left behind stays an orphan. A nil
// store or refs makes it a no-op.
func ReleaseAttachmentObjects(ctx context.Context, logger *zap.Logger, store FileStore, refs AttachmentKeyRefs, userID uuid.UUID, atts []models.FileAttachment) ReleaseResult {
	var res ReleaseResult
	if store == nil || refs == nil || len(atts) == 0 {
		return res
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	ids := make([]uuid.UUID, len(atts))
	for i := range atts {
		ids[i] = atts[i].ID
	}
	surviving, err := refs.ExistingFileAttachmentIDs(ctx, ids)
	if err != nil {
		logger.Warn("attachment cleanup: could not confirm rows were deleted, keeping objects",
			zap.String("user_id", userID.String()), zap.Int("rows", len(atts)), zap.Error(err))
		res.Kept += len(atts)
		return res
	}

	var shared, thumbs []string
	seen := map[string]bool{}
	for i := range atts {
		if surviving[atts[i].ID] {
			logger.Warn("attachment cleanup: row still exists after delete, keeping its objects",
				zap.String("user_id", userID.String()), zap.String("file_attachment_id", atts[i].ID.String()))
			res.Kept++
			continue
		}
		for _, k := range attachmentReleaseKeys(userID, &atts[i]) {
			if !seen[k] {
				seen[k] = true
				shared = append(shared, k)
			}
		}
		if isImageType(atts[i].FileType) {
			thumbs = append(thumbs, FileKeyForImageThumbnail(userID, atts[i].ID))
		}
	}

	referenced, err := refs.ReferencedFileAttachmentKeys(ctx, shared)
	if err != nil {
		// Without the reference check a delete could break a reference copy, so keep the shared
		// objects (they may stay orphaned). Thumbnails are not shared and can still go.
		logger.Warn("attachment cleanup: reference check failed, keeping objects",
			zap.String("user_id", userID.String()), zap.Int("keys", len(shared)), zap.Error(err))
		res.Kept += len(shared)
		shared = nil
	}

	del := func(key string) {
		if err := store.DeleteFile(ctx, key); err != nil {
			res.Failed++
			logger.Warn("attachment cleanup: object delete failed",
				zap.String("user_id", userID.String()), zap.String("key", key), zap.Error(err))
			return
		}
		res.Deleted++
	}
	for _, k := range shared {
		if referenced[k] {
			res.Kept++
			continue
		}
		del(k)
	}
	for _, k := range thumbs {
		del(k)
	}
	if res.Failed > 0 || res.Deleted > 0 {
		logger.Info("attachment cleanup: released objects",
			zap.String("user_id", userID.String()),
			zap.Int("deleted", res.Deleted), zap.Int("kept", res.Kept), zap.Int("failed", res.Failed))
	}
	return res
}

// PurgeUserObjects deletes everything stored for an account after it has been deleted: the
// whole users/{userID}/ prefix (attachments, thumbnails, and anything else a feature keeps there)
// and the account's export bundles under exports/{userID}/. Without an ObjectLister, or when
// listing users/{userID}/ fails, it falls back to releasing fallback, the user's attachment rows as
// read before the delete; export bundles then stay until the bucket lifecycle rule expires them.
// Best effort, like ReleaseAttachmentObjects.
//
// ListObjects stops at maxAccountExportFileObjects per prefix, so a larger account errors and
// takes the fallback, which removes only the known rows' objects.
func PurgeUserObjects(ctx context.Context, logger *zap.Logger, store FileStore, refs AttachmentKeyRefs, userID uuid.UUID, fallback []models.FileAttachment) ReleaseResult {
	var res ReleaseResult
	if store == nil {
		return res
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	lister, ok := store.(ObjectLister)
	if !ok {
		return ReleaseAttachmentObjects(ctx, logger, store, refs, userID, fallback)
	}

	if err := purgePrefix(ctx, logger, store, lister, UserObjectPrefix(userID), &res); err != nil {
		logger.Warn("account cleanup: listing user objects failed, releasing known attachments only",
			zap.String("user_id", userID.String()), zap.Error(err))
		r := ReleaseAttachmentObjects(ctx, logger, store, refs, userID, fallback)
		res.Deleted += r.Deleted
		res.Kept += r.Kept
		res.Failed += r.Failed
	}
	if err := purgePrefix(ctx, logger, store, lister, UserExportPrefix(userID), &res); err != nil {
		logger.Warn("account cleanup: listing export bundles failed, leaving them to the bucket lifecycle rule",
			zap.String("user_id", userID.String()), zap.Error(err))
	}
	logger.Info("account cleanup: purged user objects",
		zap.String("user_id", userID.String()), zap.Int("deleted", res.Deleted), zap.Int("failed", res.Failed))
	return res
}

// purgePrefix deletes every object under prefix, counting into res. It returns an error only when
// the prefix cannot be listed.
func purgePrefix(ctx context.Context, logger *zap.Logger, store FileStore, lister ObjectLister, prefix string, res *ReleaseResult) error {
	objs, err := lister.ListObjects(ctx, prefix)
	if err != nil {
		return err
	}
	for _, o := range objs {
		if !strings.HasPrefix(o.Key, prefix) {
			continue
		}
		if err := store.DeleteFile(ctx, o.Key); err != nil {
			res.Failed++
			logger.Warn("account cleanup: object delete failed", zap.String("key", o.Key), zap.Error(err))
			continue
		}
		res.Deleted++
	}
	return nil
}

// UserObjectPrefix is the key prefix the user's uploaded and generated files live under.
func UserObjectPrefix(userID uuid.UUID) string {
	return path.Join("users", userID.String()) + "/"
}

// ExportBundleRoot is the top-level prefix account export bundles are written under, as
// exports/{userID}/account-export-*.zip. It sits outside users/{userID}/ and is expired by a bucket
// lifecycle rule.
const ExportBundleRoot = "exports"

// UserExportPrefix is the key prefix of userID's account export bundles.
func UserExportPrefix(userID uuid.UUID) string {
	return path.Join(ExportBundleRoot, userID.String()) + "/"
}

// attachmentReleaseKeys is where an attachment's bytes live: its s3_key, or for legacy rows
// without one, every key the readers fall back to (all embed the attachment id).
func attachmentReleaseKeys(userID uuid.UUID, att *models.FileAttachment) []string {
	if k := strings.TrimSpace(att.S3Key); k != "" {
		return []string{k}
	}
	return legacyAttachmentKeys(userID, att)
}

// legacyAttachmentKeys are the derived keys ResolveAttachmentTextContent,
// ResolveAttachmentImageBytes and the download handler try when s3_key is empty.
func legacyAttachmentKeys(userID uuid.UUID, att *models.FileAttachment) []string {
	keys := make([]string, 0, 4)
	if isImageType(att.FileType) {
		keys = append(keys, FileKeyForImage(userID, att.ID, att.Name))
	}
	if att.ChatID != nil && *att.ChatID != uuid.Nil {
		keys = append(keys, FileKeyForChat(userID, *att.ChatID, att.ID, att.Name))
	}
	if att.PersonalityID != nil && *att.PersonalityID != uuid.Nil {
		keys = append(keys, FileKeyForPersonality(userID, *att.PersonalityID, att.ID, att.Name))
	}
	keys = append(keys, FileKeyFallback(userID, att.ID, att.Name))
	return keys
}

func isImageType(fileType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(fileType)), models.ImageMIMEPrefix)
}
