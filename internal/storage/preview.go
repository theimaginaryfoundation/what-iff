package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"

	"go.uber.org/zap"
)

// A file's preview (ADR 0x049 §8) is a picture of its content that the server cannot draw itself,
// such as a draw.io diagram, sent by the browser that drew it. It belongs to one stored object:
// keyed by that object's key, it matches the content it was drawn from, every row sharing the
// object shares it, and a save, which always writes a new object, leaves the file with none until
// it is drawn again. It is a PNG beside a JPEG thumbnail for the gallery, and goes when its object
// does (DeletePreviews).

// FileKeyForPreview is the PNG preview of the stored object at objectKey:
// users/{userID}/previews/{hash of objectKey}.png, under the object's own user prefix.
func FileKeyForPreview(objectKey string) string {
	return previewBase(objectKey) + ".png"
}

// FileKeyForPreviewThumbnail is the preview's JPEG thumbnail, beside it.
func FileKeyForPreviewThumbnail(objectKey string) string {
	return previewBase(objectKey) + ".thumb.jpg"
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

// DeletePreviews removes the preview and its thumbnail of the object at objectKey, after the object
// itself was deleted. Best effort: a failure is logged and leaves an orphan, never an error.
func DeletePreviews(ctx context.Context, logger *zap.Logger, store FileStore, objectKey string) {
	if store == nil || !mayHavePreview(objectKey) {
		return
	}
	for _, key := range []string{FileKeyForPreview(objectKey), FileKeyForPreviewThumbnail(objectKey)} {
		if err := store.DeleteFile(ctx, key); err != nil && logger != nil {
			logger.Warn("preview cleanup: delete failed", zap.String("key", key), zap.Error(err))
		}
	}
}

// mayHavePreview reports whether the object at key can have a preview: only diagrams get one, and
// a stored object's key ends with its file's name (.drawio or .drawio.xml).
func mayHavePreview(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.HasSuffix(key, ".drawio") || strings.HasSuffix(key, ".drawio.xml")
}
