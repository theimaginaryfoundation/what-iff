package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
)

// agentAttachmentIngester is the plugins.AttachmentIngester the server hands to plugins. It runs a
// file through the same steps as the chat upload endpoint (handlerutils.ProcessUpload, then
// StoreChatAttachment), so a plugin's file is checked, normalized, archived and chunked like one
// the user attached in the app.
type agentAttachmentIngester struct {
	agent *agent.Agent
	ds    *datastore.Datastore
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
	if i.agent == nil || i.agent.OpenAIProvider == nil || i.ds == nil {
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
	logger := i.agent.Logger()
	attachment, tempFilePath, err := handlerutils.ProcessUpload(ctx, i.agent.OpenAIProvider, up.UserID, attrs, bytes.NewReader(up.Data), up.Name)
	if err != nil {
		return nil, pluginAttachmentError(err)
	}
	created, err := handlerutils.StoreChatAttachment(ctx, logger, handlerutils.AttachmentStorage{
		Records:  i.ds,
		Files:    i.agent.FileStore(),
		Provider: i.agent.OpenAIProvider,
		Pipeline: i.agent.ChunkPipeline(),
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
