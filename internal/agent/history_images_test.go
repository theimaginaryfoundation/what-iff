package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

type historyImageStore struct{ content []byte }

func (s historyImageStore) UploadFile(context.Context, string, []byte, string) error { return nil }
func (s historyImageStore) DownloadFile(context.Context, string) ([]byte, error) {
	return s.content, nil
}
func (s historyImageStore) DeleteFile(context.Context, string) error { return nil }

func TestHistoryImagesForMessage_LoadsBytesWhenFileIDMissing(t *testing.T) {
	t.Parallel()

	fileID := ""
	attID := uuid.New()
	msg := &models.ChatMessage{
		Attachments: []*models.FileAttachment{
			{ID: attID, Name: "a.png", FileType: "image/png", FileID: &fileID},
		},
	}
	var store storage.FileStore = historyImageStore{content: []byte("hello")}
	imgs := historyImagesForMessage(t.Context(), nil, store, uuid.New(), uuid.New(), msg, false)
	require.Len(t, imgs, 1)
	require.NotEmpty(t, imgs[0].RawBytes)
}

func TestAppendHistoryTurn_WithImages(t *testing.T) {
	t.Parallel()

	var ctx provider.ModelContext
	ctx.AppendHistoryTurn(provider.RoleUser, "look at this", []provider.UserMessageImage{
		{FileID: "file-123", MediaType: "image/png"},
	}, true)
	require.Len(t, ctx.Segments, 1)
	require.Len(t, ctx.Segments[0].UserImages, 1)
}
