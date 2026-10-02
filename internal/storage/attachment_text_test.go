package storage

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type mapFileStore map[string][]byte

func (m mapFileStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	m[key] = content
	return nil
}

func (m mapFileStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	return m[key], nil
}

func (m mapFileStore) DeleteFile(_ context.Context, key string) error {
	delete(m, key)
	return nil
}

func TestResolveAttachmentRawText_KeepsLeadingAndTrailingWhitespace(t *testing.T) {
	store := mapFileStore{"k": []byte("\n\n  indented: true\nend\n")}
	att := &models.FileAttachment{ID: uuid.New(), Name: "cfg.yaml", FileType: "text/plain", S3Key: "k"}

	raw, ok := ResolveAttachmentRawText(context.Background(), zap.NewNop(), store, uuid.New(), att)
	assert.True(t, ok)
	assert.Equal(t, "\n\n  indented: true\nend\n", raw, "line numbers and the first line's indentation are preserved")

	trimmed, ok := ResolveAttachmentTextContent(context.Background(), zap.NewNop(), store, uuid.New(), att)
	assert.True(t, ok)
	assert.Equal(t, "indented: true\nend", trimmed, "the existing helper still trims")

	store["blank"] = []byte(" \n\t ")
	_, ok = ResolveAttachmentRawText(context.Background(), zap.NewNop(), store, uuid.New(), &models.FileAttachment{ID: uuid.New(), FileType: "text/plain", S3Key: "blank"})
	assert.False(t, ok, "whitespace-only content is still treated as no text")
}
