package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

type stubAttachmentContextStore struct {
	text string
}

func (s stubAttachmentContextStore) UploadFile(_ context.Context, _ string, _ []byte, _ string) error {
	return nil
}
func (s stubAttachmentContextStore) DownloadFile(_ context.Context, _ string) ([]byte, error) {
	if s.text != "" {
		return []byte(s.text), nil
	}
	return nil, nil
}
func (s stubAttachmentContextStore) DeleteFile(_ context.Context, _ string) error { return nil }

func TestBuildFullAttachmentContext_InlinesSmallTextFile(t *testing.T) {
	t.Parallel()

	b, err := newMessageContextBuilder(nil, &telemetry.Telemetry{Logger: zap.NewNop()}, stubAttachmentContextStore{text: "hello doc"}, nil, nil)
	require.NoError(t, err)

	hint := b.buildFullAttachmentContext(context.Background(), uuid.New(), uuid.New(), []*models.FileAttachment{
		{Name: "notes.txt", FileType: "text/plain", ID: uuid.New()},
	})
	require.Contains(t, hint, "notes.txt")
	require.Contains(t, hint, "hello doc")
	require.NotContains(t, hint, "too large to inline")
}

func TestBuildFullAttachmentContext_LargeFileTooBigWithoutChunks(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("word ", 9000)
	b, err := newMessageContextBuilder(nil, &telemetry.Telemetry{Logger: zap.NewNop()}, stubAttachmentContextStore{text: big}, nil, nil)
	require.NoError(t, err)

	hint := b.buildFullAttachmentContext(context.Background(), uuid.New(), uuid.New(), []*models.FileAttachment{
		{Name: "big.txt", FileType: "text/plain", ID: uuid.New()},
	})
	require.Contains(t, hint, "too large to inline")
	require.Contains(t, hint, "find_context")
}

func TestFormatAttachmentChunkPreview(t *testing.T) {
	t.Parallel()

	out := formatAttachmentChunkPreview([]datastore.FileChunkResult{
		{FileName: "doc.md", Sequence: 0, Content: "alpha"},
		{FileName: "doc.md", Sequence: 1, Content: "beta"},
	})
	require.Contains(t, out, "doc.md (chunk 1)")
	require.Contains(t, out, "alpha")
	require.Contains(t, out, "beta")
}
