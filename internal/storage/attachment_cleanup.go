package storage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// AttachmentKeyRefs reports which object keys file attachment rows still point at. The datastore
// implements it; it is an interface so object cleanup can live here without importing the
// datastore (which imports this package).
type AttachmentKeyRefs interface {
	// ReferencedFileAttachmentKeys returns the subset of keys that at least one remaining
	// FileAttachment row (of any owner) stores as its s3_key.
	ReferencedFileAttachmentKeys(ctx context.Context, keys []string) (map[string]bool, error)
}

// ReleaseResult counts what ReleaseAttachmentObjects did.
type ReleaseResult struct {
	Deleted int // object deletes that succeeded (missing objects count as deleted)
	Kept    int // keys skipped because another row still references them
	Failed  int // object deletes that returned an error
}

// ReleaseAttachmentObjects deletes the stored objects behind attachment rows that have ALREADY
// been deleted from the datastore (directly, or by a chat/personality/user cascade). Call it with
// the rows as they were read before the delete.
//
// Reference copies (datastore.CreateFileAttachmentReference) share an s3_key with the row they
// were copied from, so a key is only deleted when no remaining row references it. Thumbnails
// are keyed by the attachment's own id and are never shared, so an image row's thumbnail is
// always deleted.
//
// Best effort: failures are logged and counted, never returned, because the user-facing delete
// has already happened and must not fail on storage. The orphan sweep (SweepUserOrphans) is the
// backstop for anything left behind. A nil store or refs makes it a no-op.
func ReleaseAttachmentObjects(ctx context.Context, logger *zap.Logger, store FileStore, refs AttachmentKeyRefs, userID uuid.UUID, atts []models.FileAttachment) ReleaseResult {
	var res ReleaseResult
	if store == nil || refs == nil || len(atts) == 0 {
		return res
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	var shared, thumbs []string
	seen := map[string]bool{}
	for i := range atts {
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
		// objects and leave them to the sweep. Thumbnails are not shared and can still go.
		logger.Warn("attachment cleanup: reference check failed, keeping objects for the orphan sweep",
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
			zap.Int("deleted", res.Deleted), zap.Int("kept_referenced", res.Kept), zap.Int("failed", res.Failed))
	}
	return res
}

// PurgeUserObjects deletes everything stored under users/{userID}/ after the account has been
// deleted. With an ObjectLister it removes the whole prefix, which also catches objects no row
// pointed at any more. Without one it falls back to releasing fallback, the user's attachment rows
// as read before the delete. Best effort, like ReleaseAttachmentObjects.
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
	prefix := UserObjectPrefix(userID)
	objs, err := lister.ListObjects(ctx, prefix)
	if err != nil {
		logger.Warn("account cleanup: listing user objects failed, releasing known attachments only",
			zap.String("user_id", userID.String()), zap.Error(err))
		return ReleaseAttachmentObjects(ctx, logger, store, refs, userID, fallback)
	}
	for _, o := range objs {
		if !strings.HasPrefix(o.Key, prefix) {
			continue
		}
		if err := store.DeleteFile(ctx, o.Key); err != nil {
			res.Failed++
			logger.Warn("account cleanup: object delete failed",
				zap.String("user_id", userID.String()), zap.String("key", o.Key), zap.Error(err))
			continue
		}
		res.Deleted++
	}
	logger.Info("account cleanup: purged user objects",
		zap.String("user_id", userID.String()), zap.Int("deleted", res.Deleted), zap.Int("failed", res.Failed))
	return res
}

// UserObjectPrefix is the key prefix every object owned by userID lives under.
func UserObjectPrefix(userID uuid.UUID) string {
	return path.Join("users", userID.String()) + "/"
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

// ── Orphan sweep ──────────────────────────────────────────────────────────────

// SweepRefs is the datastore surface the orphan sweep needs.
type SweepRefs interface {
	AttachmentKeyRefs
	// ListUserFileAttachmentObjectRefs returns every attachment row the user owns, with the
	// fields that locate its object (ID, Name, FileType, S3Key, ChatID, PersonalityID).
	ListUserFileAttachmentObjectRefs(ctx context.Context, userID uuid.UUID) ([]models.FileAttachment, error)
}

// ErrObjectListingUnsupported is returned by SweepUserOrphans when the store cannot list objects.
var ErrObjectListingUnsupported = errors.New("file store does not support listing objects")

// SweepOptions controls SweepUserOrphans.
type SweepOptions struct {
	// DryRun reports what would be deleted without deleting anything.
	DryRun bool
	// GracePeriod keeps objects modified more recently than this, so an upload whose row is not
	// written yet (or whose s3_key is set just after the object) is never mistaken for an orphan.
	GracePeriod time.Duration
	// Now is the reference time for the grace period; zero means time.Now().
	Now time.Time
}

// SweepResult is what one SweepUserOrphans run found and did.
type SweepResult struct {
	UserID     uuid.UUID    `json:"user_id"`
	DryRun     bool         `json:"dry_run"`
	Scanned    int          `json:"scanned"`
	Referenced int          `json:"referenced"`
	Excluded   int          `json:"excluded"`   // other lifecycles (SweepExcludedSubprefixes)
	TooRecent  int          `json:"too_recent"` // unreferenced but inside the grace period
	Orphans    []ObjectInfo `json:"orphans"`    // unreferenced and past the grace period
	Deleted    int          `json:"deleted"`
	Failed     []string     `json:"failed,omitempty"`
}

// SweepUserOrphans deletes objects under users/{userID}/ that no FileAttachment row references
// and that are older than the grace period. It is idempotent: a second run finds nothing new.
// userID need not exist any more, so it also clears what a deleted account left behind.
//
// An object counts as referenced when it is a row's s3_key, any legacy key a reader falls back
// to for that row, or a row's thumbnail, or when any row (of any owner) stores it as s3_key.
// Keys under SweepExcludedSubprefixes (exports/, workspace/) are skipped: they are not
// attachments and have their own lifecycle.
func SweepUserOrphans(ctx context.Context, logger *zap.Logger, store FileStore, refs SweepRefs, userID uuid.UUID, opts SweepOptions) (*SweepResult, error) {
	if store == nil || refs == nil {
		return nil, fmt.Errorf("orphan sweep: file store and datastore are required")
	}
	if opts.GracePeriod < 0 {
		return nil, fmt.Errorf("orphan sweep: grace period must not be negative")
	}
	lister, ok := store.(ObjectLister)
	if !ok {
		return nil, ErrObjectListingUnsupported
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-opts.GracePeriod)
	res := &SweepResult{UserID: userID, DryRun: opts.DryRun, Orphans: []ObjectInfo{}}

	// List objects before rows: an upload that lands in between then has its row in the listing,
	// and one that lands after the row listing is inside the grace period.
	prefix := UserObjectPrefix(userID)
	objs, err := lister.ListObjects(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("orphan sweep: list %s: %w", prefix, err)
	}
	rows, err := refs.ListUserFileAttachmentObjectRefs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("orphan sweep: list attachment rows: %w", err)
	}
	known := map[string]bool{}
	for i := range rows {
		att := &rows[i]
		if k := strings.TrimSpace(att.S3Key); k != "" {
			known[k] = true
		}
		for _, k := range legacyAttachmentKeys(userID, att) {
			known[k] = true
		}
		known[FileKeyForImageThumbnail(userID, att.ID)] = true
	}

	var candidates []ObjectInfo
	for _, o := range objs {
		res.Scanned++
		switch {
		case !strings.HasPrefix(o.Key, prefix) || isSweepExcluded(o.Key, prefix):
			res.Excluded++
		case known[o.Key]:
			res.Referenced++
		case o.LastModified.After(cutoff):
			res.TooRecent++
		default:
			candidates = append(candidates, o)
		}
	}

	if len(candidates) > 0 {
		keys := make([]string, len(candidates))
		for i, o := range candidates {
			keys[i] = o.Key
		}
		// Catch rows outside the user's own listing (another owner, or one written since).
		referenced, err := refs.ReferencedFileAttachmentKeys(ctx, keys)
		if err != nil {
			return nil, fmt.Errorf("orphan sweep: reference check: %w", err)
		}
		for _, o := range candidates {
			if referenced[o.Key] {
				res.Referenced++
				continue
			}
			res.Orphans = append(res.Orphans, o)
		}
	}
	sort.Slice(res.Orphans, func(i, j int) bool { return res.Orphans[i].Key < res.Orphans[j].Key })

	if !opts.DryRun {
		for _, o := range res.Orphans {
			if err := store.DeleteFile(ctx, o.Key); err != nil {
				res.Failed = append(res.Failed, o.Key)
				logger.Warn("orphan sweep: delete failed", zap.String("key", o.Key), zap.Error(err))
				continue
			}
			res.Deleted++
		}
	}
	logger.Info("orphan sweep finished",
		zap.String("user_id", userID.String()),
		zap.Bool("dry_run", opts.DryRun),
		zap.Int("scanned", res.Scanned),
		zap.Int("referenced", res.Referenced),
		zap.Int("excluded", res.Excluded),
		zap.Int("too_recent", res.TooRecent),
		zap.Int("orphans", len(res.Orphans)),
		zap.Int("deleted", res.Deleted),
		zap.Int("failed", len(res.Failed)))
	return res, nil
}

// SweepExcludedSubprefixes are the parts of users/{uid}/ that do not hold attachment objects.
// They are referenced from elsewhere and cleaned up by their own lifecycle, so the attachment
// orphan sweep never touches them. Account deletion (PurgeUserObjects) still removes them.
var SweepExcludedSubprefixes = []string{
	// Account export bundles; expired by the bucket lifecycle rule.
	"exports/",
	// Agent workspace file revisions; referenced from workspace_file_revisions.storage_key, not
	// from FileAttachment, and swept by the workspace's own lifecycle.
	"workspace/",
}

// isSweepExcluded reports whether key, under userPrefix, belongs to another lifecycle.
func isSweepExcluded(key, userPrefix string) bool {
	rel := strings.TrimPrefix(key, userPrefix)
	for _, p := range SweepExcludedSubprefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}
