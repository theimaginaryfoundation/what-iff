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

	ids := make([]uuid.UUID, len(atts))
	for i := range atts {
		ids[i] = atts[i].ID
	}
	surviving, err := refs.ExistingFileAttachmentIDs(ctx, ids)
	if err != nil {
		logger.Warn("attachment cleanup: could not confirm rows were deleted, keeping objects for the orphan sweep",
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
// takes the fallback; the orphan sweep can finish the rest.
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
	UserID         uuid.UUID    `json:"user_id"`
	DryRun         bool         `json:"dry_run"`
	Scanned        int          `json:"scanned"`
	Referenced     int          `json:"referenced"`
	SkippedUnknown int          `json:"skipped_unknown_prefix"` // not an attachment layout; never touched
	TooRecent      int          `json:"too_recent"`             // unreferenced but inside the grace period
	Orphans        []ObjectInfo `json:"orphans"`                // unreferenced and past the grace period
	Deleted        int          `json:"deleted"`
	Failed         []string     `json:"failed,omitempty"`
}

// SweepUserOrphans deletes attachment objects under users/{userID}/ that no FileAttachment row
// references and that are older than the grace period. It is idempotent: a second run finds
// nothing new. userID need not exist any more, so it also clears what a deleted account left
// behind.
//
// It is an allow-list: only keys in a known attachment layout (see isAttachmentObjectKey) are
// candidates. Anything else under users/{userID}/ (agent workspace files, or whatever a future
// feature stores there) is counted as SkippedUnknown and left alone, because it is referenced
// from tables this sweep does not read.
//
// A candidate counts as referenced when it is a row's s3_key, any legacy key a reader falls back
// to for that row, or a row's thumbnail, or when any row (of any owner) stores it as s3_key.
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
		case !strings.HasPrefix(o.Key, prefix) || !isAttachmentObjectKey(strings.TrimPrefix(o.Key, prefix)):
			res.SkippedUnknown++
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
		zap.Int("skipped_unknown_prefix", res.SkippedUnknown),
		zap.Int("too_recent", res.TooRecent),
		zap.Int("orphans", len(res.Orphans)),
		zap.Int("deleted", res.Deleted),
		zap.Int("failed", len(res.Failed)))
	return res, nil
}

// isAttachmentObjectKey reports whether rel, a key relative to users/{userID}/, has one of the
// layouts the FileKeyFor* builders produce. Every leaf starts with the attachment id:
//
//	images/{id}[_name]                 FileKeyForImage
//	images/thumbs/{id}.jpg             FileKeyForImageThumbnail
//	chats/{chatID}/{id}[_name]         FileKeyForChat
//	personalities/{pID}/{id}[_name]    FileKeyForPersonality
//	{id}[_name]                        FileKeyFallback
func isAttachmentObjectKey(rel string) bool {
	parts := strings.Split(rel, "/")
	switch {
	case len(parts) == 1:
		return startsWithAttachmentID(parts[0])
	case len(parts) == 2 && parts[0] == "images":
		return startsWithAttachmentID(parts[1])
	case len(parts) == 3 && parts[0] == "images" && parts[1] == "thumbs":
		id, ok := strings.CutSuffix(parts[2], ".jpg")
		return ok && isUUID(id)
	case len(parts) == 3 && (parts[0] == "chats" || parts[0] == "personalities"):
		return isUUID(parts[1]) && startsWithAttachmentID(parts[2])
	}
	return false
}

// startsWithAttachmentID matches filenameWithFallback's output: "{id}" or "{id}_{name}".
func startsWithAttachmentID(leaf string) bool {
	const n = 36 // canonical uuid length
	if len(leaf) < n || !isUUID(leaf[:n]) {
		return false
	}
	return len(leaf) == n || leaf[n] == '_'
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	_, err := uuid.Parse(s)
	return err == nil
}
