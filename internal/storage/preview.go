package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/drawio"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// A file's preview (ADR 0x049 §8) is a picture of its content that the server cannot draw itself,
// such as a draw.io diagram, sent by the browser that drew it. It belongs to one stored object:
// keyed by that object's key, it matches the content it was drawn from, every row sharing the
// object shares it, and a save, which always writes a new object, leaves the file with none until
// it is drawn again. It is a PNG beside a JPEG thumbnail for the gallery and a tag naming that PNG
// (PreviewTag), and goes when its object does (DeletePreviews).

// FileKeyForPreview is the PNG preview of the stored object at objectKey:
// users/{userID}/previews/{hash of objectKey}.png, under the object's own user prefix.
func FileKeyForPreview(objectKey string) string {
	return previewBase(objectKey) + ".png"
}

// FileKeyForPreviewThumbnail is the preview's JPEG thumbnail, beside it.
func FileKeyForPreviewThumbnail(objectKey string) string {
	return previewBase(objectKey) + ".thumb.jpg"
}

// fileKeyForPreviewTag is the preview's tag, beside it: a short hash of the PNG, so a reader can
// tell whether its copy is current without downloading the picture.
func fileKeyForPreviewTag(objectKey string) string {
	return previewBase(objectKey) + ".tag"
}

// PutPreview stores png, with its thumbnail thumb (nil for none), as the preview of the object at
// objectKey, replacing any it had. The tag goes first and is written last, so a reader never sees
// the old tag with the new picture; should writing it fail, PreviewTag has none and the reader
// hashes the picture itself. A thumbnail that cannot be stored is logged, not an error.
func PutPreview(ctx context.Context, logger *zap.Logger, store FileStore, objectKey string, png, thumb []byte) error {
	tagKey := fileKeyForPreviewTag(objectKey)
	if err := store.DeleteFile(ctx, tagKey); err != nil {
		return err
	}
	if err := store.UploadFile(ctx, FileKeyForPreview(objectKey), png, "image/png"); err != nil {
		return err
	}
	if len(thumb) > 0 {
		if err := store.UploadFile(ctx, FileKeyForPreviewThumbnail(objectKey), thumb, "image/jpeg"); err != nil && logger != nil {
			logger.Warn("preview: thumbnail upload failed", zap.Error(err))
		}
	}
	if err := store.UploadFile(ctx, tagKey, []byte(PreviewTagOf(png)), "text/plain"); err != nil && logger != nil {
		logger.Warn("preview: tag upload failed", zap.Error(err))
	}
	return nil
}

// PreviewTagOf is the tag of a preview's bytes.
func PreviewTagOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

// PreviewTag is the tag PutPreview stored for the object's preview, or "" when it has none (no
// preview, or one stored before tags were). Reading it is one small download.
func PreviewTag(ctx context.Context, store FileStore, objectKey string) (string, error) {
	data, err := store.DownloadFile(ctx, fileKeyForPreviewTag(objectKey))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func previewBase(objectKey string) string {
	sum := sha256.Sum256([]byte(objectKey))
	name := hex.EncodeToString(sum[:16])
	// Under the object's users/{id}/ prefix, so deleting an account (PurgeUserObjects) takes it.
	if parts := strings.SplitN(objectKey, "/", 3); len(parts) == 3 && parts[0] == "users" && parts[1] != "" {
		return path.Join("users", parts[1], "previews", name)
	}
	return path.Join("previews", name)
}

// DeletePreviews removes the preview, its thumbnail and its tag of the object at objectKey, after the object
// itself was deleted. Best effort: a failure is logged and leaves an orphan, never an error.
// Callers releasing objects of any kind ask mayHavePreview first.
func DeletePreviews(ctx context.Context, logger *zap.Logger, store FileStore, objectKey string) {
	if store == nil {
		return
	}
	for _, key := range []string{fileKeyForPreviewTag(objectKey), FileKeyForPreview(objectKey), FileKeyForPreviewThumbnail(objectKey)} {
		if err := store.DeleteFile(ctx, key); err != nil && logger != nil {
			logger.Warn("preview cleanup: delete failed", zap.String("key", key), zap.Error(err))
		}
	}
}

// mayHavePreview reports whether the object at key, which rows point at, can have a preview. Only a
// diagram (drawio.IsDiagramFile) is given one, and the rule is asked of the object's key, which ends
// with the name the file had when the object was written, and of each row as it is now. Either
// alone can miss: a file renamed to .drawio, or stored with draw.io's media type under another
// name, keeps an object whose key is not a diagram's; one renamed away from .drawio keeps a key
// that is. Asking both costs nothing, where deleting previews for every object released would cost
// two storage calls each.
func mayHavePreview(key string, rows ...*models.FileAttachment) bool {
	if drawio.IsDiagramFile(strings.TrimSpace(key), "") {
		return true
	}
	for _, row := range rows {
		if row != nil && drawio.IsDiagramFile(row.Name, row.FileType) {
			return true
		}
	}
	return false
}
