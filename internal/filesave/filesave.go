// Package filesave replaces the content of a user's file from the file workspace (ADR 0x043 §3):
// a new object is uploaded and the row (with the chat copies that share its object) is swapped to
// it, guarded by the content version the editor loaded. PUT /file-attachment/{id}/content maps
// its errors to statuses; the rules live here so other writers can share them.
package filesave

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filechange"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

var (
	// ErrNotFound means the id is not one of the caller's gallery files.
	ErrNotFound = errors.New("file not found")
	// ErrSkillFile means the file belongs to a skill bundle, which the skill editor manages.
	ErrSkillFile = datastore.ErrSkillFileImmutable
	// ErrTypeMismatch means the body's content type is not the file's: a save keeps the file's
	// type, so an image is saved in its own format and a .md stays Markdown.
	ErrTypeMismatch = errors.New("the content type does not match the file's")
	// ErrInvalidImage means the body of an image file could not be decoded as an image.
	ErrInvalidImage = errors.New("the content is not a valid image")
	// ErrTooLarge means the body is over the service's cap.
	ErrTooLarge = errors.New("the content is too large")
	// ErrAgentFileTooLarge means the body is over the cap on agent-authored files, which the
	// agent's own tools enforce: a bigger file could never be edited by the agent again.
	ErrAgentFileTooLarge = errors.New("the content is too large for an agent file")
	// ErrNoStore means no object store is configured, so nothing can be saved.
	ErrNoStore = errors.New("file storage is not configured")
)

// ConflictError means the file changed since the version the caller loaded (If-Match): the save
// was not applied. Current is the version the file is at now.
type ConflictError struct{ Current int }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("the file changed since it was loaded (now at version %d)", e.Current)
}

// Store is the datastore surface a save needs; *datastore.Datastore satisfies it.
type Store interface {
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	SwapFileObject(ctx context.Context, userID, id uuid.UUID, oldKey, newKey string, opts datastore.SwapFileObjectOptions) (bool, error)
	ClearFileChunks(ctx context.Context, fileAttachmentID uuid.UUID) error
	storage.AttachmentKeyRefs
}

// Chunker re-indexes a file's text for search after a save; *filechunker.FileChunkPipeline
// satisfies it. Nil skips re-indexing.
type Chunker interface {
	ProcessAndStore(ctx context.Context, fileAttachmentID uuid.UUID, content []byte, fileName, contentType string) error
}

// Service saves file content. MaxBytes caps a body (the upload limit by default) and
// MaxChunkBytes caps what is re-indexed in the background.
type Service struct {
	ds            Store
	files         storage.FileStore
	chunker       Chunker
	logger        *zap.Logger
	MaxBytes      int
	MaxChunkBytes int
	// rechunk runs the re-index; tests replace it to run synchronously.
	rechunk func(fn func())
}

// New returns a Service. files may be nil, in which case every save fails with ErrNoStore;
// chunker may be nil (no re-indexing).
func New(ds Store, files storage.FileStore, chunker *filechunker.FileChunkPipeline, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &Service{ds: ds, files: files, logger: logger, MaxBytes: 30 << 20, MaxChunkBytes: 16 << 20, rechunk: func(fn func()) { go fn() }}
	if chunker != nil {
		s.chunker = chunker
	}
	return s
}

// Request is one save.
type Request struct {
	ID uuid.UUID
	// Content is the whole new file; empty is a valid file.
	Content []byte
	// ContentType is the body's media type, "" to skip the check. Anything else must match the
	// file's type (parameters such as charset are ignored).
	ContentType string
	// IfMatch is the content version the caller loaded; nil overwrites unconditionally.
	IfMatch *int
	// ChatID is the conversation the save was made from, if any; carried on the change event.
	ChatID *uuid.UUID
}

// Replace stores req.Content as the file's new content and returns the row as it is afterwards.
func (s *Service) Replace(ctx context.Context, userID uuid.UUID, req Request) (*models.FileAttachment, error) {
	if s.files == nil {
		return nil, ErrNoStore
	}
	if len(req.Content) > s.MaxBytes {
		return nil, ErrTooLarge
	}
	att, err := s.ds.GetFileAttachment(ctx, userID, req.ID)
	if err != nil {
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if att.ContextOnly {
		return nil, ErrNotFound
	}
	if att.IsSkillFile() {
		return nil, ErrSkillFile
	}
	if att.AgentAuthored() && len(req.Content) > models.AgentFileMaxBytes {
		return nil, ErrAgentFileTooLarge
	}
	if ct := mediaType(req.ContentType); ct != "" && ct != mediaType(att.FileType) {
		return nil, ErrTypeMismatch
	}
	isImage := strings.HasPrefix(att.FileType, models.ImageMIMEPrefix)
	var thumb []byte
	if isImage {
		if thumb, err = imageutil.GenerateThumbnail(req.Content, imageutil.DefaultThumbnailMaxPx); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
		}
	}

	key := newObjectKey(userID, att)
	if err := s.files.UploadFile(ctx, key, req.Content, att.FileType); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(req.Content)
	swapped, err := s.ds.SwapFileObject(ctx, userID, att.ID, att.S3Key, key, datastore.SwapFileObjectOptions{
		ExpectVersion:  req.IfMatch,
		SizeBytes:      int64(len(req.Content)),
		SHA256:         hex.EncodeToString(sum[:]),
		MoveSharedRows: true,
	})
	if err != nil {
		// The swap may have committed before the error surfaced, so key is kept: an orphaned
		// object beats a row pointing at a deleted one.
		return nil, err
	}
	if !swapped {
		s.dropObject(ctx, key)
		cur, err := s.ds.GetFileAttachment(ctx, userID, att.ID)
		if err != nil {
			if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return nil, &ConflictError{Current: cur.ContentVersion}
	}
	storage.ReleaseReplacedObject(context.WithoutCancel(ctx), s.logger, s.files, s.ds, att.S3Key)
	if isImage {
		// Best effort, like uploads: a missing thumbnail is regenerated on demand.
		if err := s.files.UploadFile(ctx, storage.FileKeyForImageThumbnail(userID, att.ID), thumb, "image/jpeg"); err != nil {
			s.logger.Warn("file save: thumbnail upload failed", zap.String("file_attachment_id", att.ID.String()), zap.Error(err))
		}
	}
	s.reindex(userID, att, req.Content)

	updated, err := s.ds.GetFileAttachment(ctx, userID, att.ID)
	if err != nil {
		return nil, err
	}
	filechange.Notify(ctx, filechange.Event{Kind: filechange.KindContent, UserID: userID, File: updated, OldKey: att.S3Key, ChatID: req.ChatID})
	return updated, nil
}

// reindex re-chunks a file that was indexed for search (or whose indexing was attempted), in the
// background. Files never chunked (agent files, binaries) are left alone.
func (s *Service) reindex(userID uuid.UUID, att *models.FileAttachment, content []byte) {
	if s.chunker == nil || att.ChunkStatus == "" || len(content) > s.MaxChunkBytes {
		return
	}
	id, name, contentType := att.ID, att.Name, att.FileType
	logger := s.logger
	s.rechunk(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("file save: panic in re-indexing recovered", zap.Any("panic", r), zap.String("file_attachment_id", id.String()))
			}
		}()
		ctx := apicontext.WithUserID(context.Background(), userID)
		if err := s.ds.ClearFileChunks(ctx, id); err != nil {
			logger.Error("file save: clearing chunks failed", zap.String("file_attachment_id", id.String()), zap.Error(err))
			return
		}
		if err := s.chunker.ProcessAndStore(ctx, id, content, name, contentType); err != nil {
			logger.Warn("file save: re-indexing failed", zap.String("file_attachment_id", id.String()), zap.Error(err))
		}
	})
}

func (s *Service) dropObject(ctx context.Context, key string) {
	if err := s.files.DeleteFile(context.WithoutCancel(ctx), key); err != nil {
		s.logger.Warn("file save: failed to delete an unused object", zap.String("key", key), zap.Error(err))
	}
}

// newObjectKey picks where the new version lives: agent files stay under the agent prefix (agent
// reads refuse anything else), images under the image store, and everything else under files/.
func newObjectKey(userID uuid.UUID, att *models.FileAttachment) string {
	switch {
	case att.AgentAuthored():
		return storage.FileKeyForAgentFile(userID, uuid.New(), att.Name)
	case strings.HasPrefix(att.FileType, models.ImageMIMEPrefix):
		return storage.FileKeyForImage(userID, uuid.New(), att.Name)
	default:
		return storage.FileKeyForUserFile(userID, uuid.New(), att.Name)
	}
}

func mediaType(ct string) string {
	ct, _, _ = strings.Cut(ct, ";")
	return strings.ToLower(strings.TrimSpace(ct))
}
