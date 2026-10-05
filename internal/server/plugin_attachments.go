package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// agentAttachmentIngester is the plugins.AttachmentIngester the server hands to plugins. It runs a
// file through the same steps as the chat upload endpoint (handlerutils.ProcessUpload, then
// StoreChatAttachment), so a plugin's file is checked, normalized, archived and chunked like one
// the user attached in the app.
type agentAttachmentIngester struct {
	ds       attachmentDatastore
	provider attachmentProvider
	files    storage.FileStore
	pipeline *filechunker.FileChunkPipeline
	logger   *zap.Logger
}

// attachmentDatastore is the slice of the datastore the ingester uses: the chat ownership check
// and the attachment records StoreChatAttachment writes.
type attachmentDatastore interface {
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
	handlerutils.AttachmentRecords
}

// attachmentProvider is the file provider (OpenAI) the upload goes through and rolls back from.
type attachmentProvider interface {
	handlerutils.FileAttachmentUploader
	handlerutils.FileAttachmentDeleter
}

// newAgentAttachmentIngester wires the ingester to the agent's provider, object store and chunk
// pipeline. A missing agent, provider or datastore leaves the matching field nil, which
// IngestAttachment reports as errAttachmentNotConfigured; the nil checks are on the concrete
// pointers so a nil *OpenAIProvider never becomes a non-nil interface.
func newAgentAttachmentIngester(a *agent.Agent, ds *datastore.Datastore) agentAttachmentIngester {
	i := agentAttachmentIngester{}
	if a == nil {
		return i
	}
	if ds != nil {
		i.ds = ds
	}
	if a.OpenAIProvider != nil {
		i.provider = a.OpenAIProvider
	}
	i.files = a.FileStore()
	i.pipeline = a.ChunkPipeline()
	i.logger = a.Logger()
	return i
}

var (
	errAttachmentMissingTarget = errors.New("plugin attachment: user id, chat id and file name are required")
	errAttachmentEmpty         = errors.New("plugin attachment: file is empty")
	errAttachmentNotConfigured = errors.New("plugin attachment: no file provider or datastore is configured")
)

func (i agentAttachmentIngester) IngestAttachment(ctx context.Context, up plugins.AttachmentUpload) (*models.FileAttachment, error) {
	if up.UserID == uuid.Nil || up.ChatID == uuid.Nil || up.Name == "" {
		return nil, errAttachmentMissingTarget
	}
	if len(up.Data) == 0 {
		return nil, errAttachmentEmpty
	}
	// Reject before anything copies or buffers the bytes; ProcessUpload enforces the same limit
	// again while streaming, but by then a huge slice has already been handed to us.
	if len(up.Data) > handlerutils.MaxUploadBytes {
		return nil, fmt.Errorf("%w: file is %d bytes (max %d)", plugins.ErrAttachmentTooLarge, len(up.Data), handlerutils.MaxUploadBytes)
	}
	if i.provider == nil || i.ds == nil {
		return nil, errAttachmentNotConfigured
	}
	// Ownership: the chat must belong to the user, as the upload route's chat lookup requires.
	// Only a missing chat becomes a plugin-level error; anything else is an internal failure.
	if _, err := i.ds.GetChat(ctx, up.UserID, up.ChatID); err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			return nil, plugins.ErrAttachmentChatNotFound
		}
		return nil, fmt.Errorf("plugin attachment: chat lookup: %w", err)
	}

	ctx = middleware.ContextWithUser(ctx, up.UserID, "")
	attrs := map[string]string{"chat_id": up.ChatID.String()}
	logger := i.logger
	if logger == nil {
		logger = zap.NewNop()
	}
	attachment, tempFilePath, err := handlerutils.ProcessUpload(ctx, i.provider, up.UserID, attrs, bytes.NewReader(up.Data), up.Name)
	if err != nil {
		return nil, pluginAttachmentError(err)
	}
	created, err := handlerutils.StoreChatAttachment(ctx, logger, handlerutils.AttachmentStorage{
		Records:  i.ds,
		Files:    i.files,
		Provider: i.provider,
		Pipeline: i.pipeline,
	}, up.UserID, up.ChatID, attachment, tempFilePath)
	if err != nil {
		return nil, pluginAttachmentError(err)
	}
	return created, nil
}

// pluginAttachmentError maps a rejected upload to the plugin-facing sentinels.
func pluginAttachmentError(err error) error {
	switch {
	case errors.Is(err, handlerutils.ErrUnsupportedFileType):
		return fmt.Errorf("%w: %v", plugins.ErrAttachmentUnsupported, err)
	case errors.Is(err, handlerutils.ErrFileTooLarge):
		return fmt.Errorf("%w: %v", plugins.ErrAttachmentTooLarge, err)
	}
	return err
}
