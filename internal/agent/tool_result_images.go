package agent

import (
	"encoding/base64"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// toolResultImagesFromAttachments converts tool-generated image attachments into
// vision payloads for the next agent-loop adapter Call. An attachment carrying a Thumbnail
// (generate_image's output) is shown as that thumbnail: the model only needs to see what it
// made, and full-size images in the tool loop could OOM the API (what-iff#419, ADR 0x038).
// Others (an MCP screenshot, a file find_context fetched) are shown at full size.
func toolResultImagesFromAttachments(atts []*models.FileAttachment) []provider.UserMessageImage {
	if len(atts) == 0 {
		return nil
	}
	out := make([]provider.UserMessageImage, 0, len(atts))
	for _, att := range atts {
		if att == nil || !strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			continue
		}
		if len(att.Thumbnail) > 0 {
			out = append(out, provider.UserMessageImage{RawBytes: att.Thumbnail, MediaType: "image/jpeg"})
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

// shownToolImages are the pictures a tool showed with its result (tools.AddResultImage), for a
// model that takes images; none for one that does not.
func shownToolImages(chatCtx *chatContext, images []tools.ResultImage) []provider.UserMessageImage {
	if len(images) == 0 || chatCtx == nil || !chatCtx.modelVisionSupport {
		return nil
	}
	out := make([]provider.UserMessageImage, 0, len(images))
	for _, img := range images {
		out = append(out, provider.UserMessageImage{RawBytes: img.Data, MediaType: img.MediaType})
	}
	return out
}
