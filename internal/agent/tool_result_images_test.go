package agent

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
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

// A generated image's thumbnail is what the model sees in the tool loop; an attachment without
// one (an MCP screenshot) still goes at full size.
func TestToolResultImagesFromAttachments_PrefersTheThumbnail(t *testing.T) {
	t.Parallel()
	full := []byte{0x89, 0x50, 0x4e, 0x47}
	imgs := toolResultImagesFromAttachments([]*models.FileAttachment{
		{Name: "gen.png", FileType: "image/png", FileContent: base64.StdEncoding.EncodeToString(full), Thumbnail: []byte("jpeg")},
		{Name: "shot.png", FileType: "image/png", FileContent: base64.StdEncoding.EncodeToString(full)},
	})
	require.Len(t, imgs, 2)
	require.Equal(t, []byte("jpeg"), imgs[0].RawBytes)
	require.Equal(t, "image/jpeg", imgs[0].MediaType)
	require.Equal(t, full, imgs[1].RawBytes)
}

func TestShownToolImages_OnlyForModelsThatTakeImages(t *testing.T) {
	t.Parallel()
	shown := []tools.ResultImage{{Data: []byte("png"), MediaType: "image/png"}}
	require.Nil(t, shownToolImages(&chatContext{modelVisionSupport: false}, shown))
	require.Nil(t, shownToolImages(nil, shown))
	imgs := shownToolImages(&chatContext{modelVisionSupport: true}, shown)
	require.Len(t, imgs, 1)
	require.Equal(t, []byte("png"), imgs[0].RawBytes)
	require.Equal(t, "image/png", imgs[0].MediaType)
}
