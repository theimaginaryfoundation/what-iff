package agent

import (
	"encoding/base64"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// toolResultImagesFromAttachments converts tool-generated image attachments into
// vision payloads for the next agent-loop adapter Call.
func toolResultImagesFromAttachments(atts []*models.FileAttachment) []provider.UserMessageImage {
	if len(atts) == 0 {
		return nil
	}
	out := make([]provider.UserMessageImage, 0, len(atts))
	for _, att := range atts {
		if att == nil || !strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(att.FileContent))
		if err != nil || len(raw) == 0 {
			continue
		}
		mediaType := strings.TrimSpace(att.FileType)
		if mediaType == "" {
			mediaType = "image/png"
		}
		out = append(out, provider.UserMessageImage{
			RawBytes:  raw,
			MediaType: mediaType,
		})
	}
	return out
}
