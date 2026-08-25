package agent

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestToolResultImagesFromAttachments_DecodesPNG(t *testing.T) {
	t.Parallel()
	raw := []byte{0x89, 0x50, 0x4e, 0x47}
	imgs := toolResultImagesFromAttachments([]*models.FileAttachment{
		{Name: "out.png", FileType: "image/png", FileContent: base64.StdEncoding.EncodeToString(raw)},
	})
	require.Len(t, imgs, 1)
	require.Equal(t, raw, imgs[0].RawBytes)
	require.Equal(t, "image/png", imgs[0].MediaType)
}

func TestToolResultImagesFromAttachments_SkipsNonImages(t *testing.T) {
	t.Parallel()
	imgs := toolResultImagesFromAttachments([]*models.FileAttachment{
		{Name: "doc.txt", FileType: "text/plain", FileContent: "aGk="},
	})
	require.Empty(t, imgs)
}
