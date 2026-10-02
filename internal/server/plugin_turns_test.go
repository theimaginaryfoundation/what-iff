package server

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
)

func TestAgentTurnStarterRejectsATurnWithoutAUserOrChat(t *testing.T) {
	// A nil agent proves the guard runs before any agent call.
	s := agentTurnStarter{}
	for name, turn := range map[string]plugins.UserTurn{
		"no user": {Message: models.ChatMessage{ChatID: uuid.New(), Message: "hi"}},
		"no chat": {UserID: uuid.New(), Message: models.ChatMessage{Message: "hi"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.StartUserTurn(context.Background(), turn)
			assert.ErrorIs(t, err, errTurnMissingTarget)
		})
	}
}

func TestAgentAttachmentIngesterRejectsAFileWithoutAUserChatNameOrBytes(t *testing.T) {
	// A nil agent and datastore prove the guards run before either is touched.
	s := agentAttachmentIngester{}
	for name, up := range map[string]plugins.AttachmentUpload{
		"no user": {ChatID: uuid.New(), Name: "a.png", Data: []byte("x")},
		"no chat": {UserID: uuid.New(), Name: "a.png", Data: []byte("x")},
		"no name": {UserID: uuid.New(), ChatID: uuid.New(), Data: []byte("x")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.IngestAttachment(context.Background(), up)
			assert.ErrorIs(t, err, errAttachmentMissingTarget)
		})
	}

	_, err := s.IngestAttachment(context.Background(), plugins.AttachmentUpload{UserID: uuid.New(), ChatID: uuid.New(), Name: "a.png"})
	assert.ErrorIs(t, err, errAttachmentEmpty)

	_, err = s.IngestAttachment(context.Background(), plugins.AttachmentUpload{UserID: uuid.New(), ChatID: uuid.New(), Name: "a.png", Data: []byte("x")})
	assert.ErrorIs(t, err, errAttachmentNotConfigured)
}

func TestPluginAttachmentErrorMapsRejectionsToThePluginSentinels(t *testing.T) {
	unsupported := pluginAttachmentError(&handlerutils.UploadError{Status: 400, Message: "Unsupported file type", Err: handlerutils.ErrUnsupportedFileType})
	assert.ErrorIs(t, unsupported, plugins.ErrAttachmentUnsupported)

	tooLarge := pluginAttachmentError(&handlerutils.UploadError{Status: 400, Message: "File too large", Err: handlerutils.ErrFileTooLarge})
	assert.ErrorIs(t, tooLarge, plugins.ErrAttachmentTooLarge)

	internal := errors.New("s3 down")
	got := pluginAttachmentError(&handlerutils.UploadError{Status: 500, Message: "x", Err: internal})
	assert.NotErrorIs(t, got, plugins.ErrAttachmentUnsupported)
	assert.NotErrorIs(t, got, plugins.ErrAttachmentTooLarge)
}
