package agent

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// maxHistoryImages caps how many images from prior turns are inlined into a request. History
// images are there for continuity, which a handful of the most recent ones give; past that each
// one only adds request size, and every turn re-sends them all (what-iff#419, ADR 0x038).
const maxHistoryImages = 5

// historyImageSet is the set of prior-turn image attachment IDs a request inlines.
type historyImageSet map[uuid.UUID]struct{}

// selectHistoryImages picks the attachment IDs of the most recent maxImages images across the
// replayed turns: history (chronological, post-checkpoint) is newest, then carry-over (the
// pre-checkpoint turns kept for continuity). Within a message the newest attachments come first,
// so a reply that generated more images than the cap keeps its last ones, the ones the
// conversation is most likely about. Older images are left out of the request.
func selectHistoryImages(carryOver [][2]*models.ChatMessage, history []*models.ChatMessage, maxImages int) historyImageSet {
	selected := historyImageSet{}
	take := func(msg *models.ChatMessage) {
		if msg == nil {
			return
		}
		for _, att := range newestImagesFirst(msg.Attachments) {
			if len(selected) >= maxImages {
				return
			}
			selected[att.ID] = struct{}{}
		}
	}
	for i := len(history) - 1; i >= 0 && len(selected) < maxImages; i-- {
		take(history[i])
	}
	for i := len(carryOver) - 1; i >= 0 && len(selected) < maxImages; i-- {
		take(carryOver[i][1])
		take(carryOver[i][0])
	}
	return selected
}

// newestImagesFirst returns the image attachments in atts, newest first: by CreatedAt, then by
// later position (attachments load in insertion order, and a tool's images share a timestamp
// closely enough that position is the better tie-break).
func newestImagesFirst(atts []*models.FileAttachment) []*models.FileAttachment {
	var images []*models.FileAttachment
	for i := len(atts) - 1; i >= 0; i-- {
		if att := atts[i]; att != nil && strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			images = append(images, att)
		}
	}
	slices.SortStableFunc(images, func(a, b *models.FileAttachment) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return images
}

// historyImagesForMessage collects vision payloads for the image attachments on a prior message
// that are in inline. Prior turns send each image's thumbnail (falling back to the original when
// none was stored): the model needs continuity there, not detail, and the current turn's
// attachments are sent at full size separately. An image the store cannot produce is skipped.
func historyImagesForMessage(
	ctx context.Context,
	logger *zap.Logger,
	fileStore storage.FileStore,
	userID uuid.UUID,
	chatID uuid.UUID,
	msg *models.ChatMessage,
	inline historyImageSet,
) []provider.UserMessageImage {
	if msg == nil || len(msg.Attachments) == 0 || len(inline) == 0 {
		return nil
	}
	var out []provider.UserMessageImage
	for _, att := range msg.Attachments {
		if att == nil || !strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			continue
		}
		if _, ok := inline[att.ID]; !ok {
			continue
		}
		raw, mediaType := resolveImageBytes(ctx, logger, fileStore, userID, chatID, att, true)
		if len(raw) == 0 {
			continue
		}
		out = append(out, provider.UserMessageImage{MediaType: mediaType, RawBytes: raw})
	}
	return out
}

// loadImageBytesForAttachments resolves the full-size bytes of each image attachment, for the
// current turn's inline image input (every provider's vision input is built from the store).
//
// fileStore and logger may both be nil. A nil store skips object-storage lookups and
// a nil logger skips debug logging.
func loadImageBytesForAttachments(
	ctx context.Context,
	logger *zap.Logger,
	fileStore storage.FileStore,
	userID, chatID uuid.UUID,
	attachments []*models.FileAttachment,
) map[uuid.UUID][]byte {
	result := make(map[uuid.UUID][]byte)
	for _, att := range attachments {
		if att == nil || !strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			continue
		}
		if raw, _ := resolveImageBytes(ctx, logger, fileStore, userID, chatID, att, false); len(raw) > 0 {
			result[att.ID] = raw
		}
	}
	return result
}

// resolveImageBytes is the canonical lookup of an image attachment's bytes for model context:
// storage.ResolveAttachmentImageBytes, then the chat-scoped key for attachments that predate
// s3_key. wantThumb prefers the stored thumbnail. It returns the bytes and their media type, or
// nil (logged) when nothing was found.
func resolveImageBytes(
	ctx context.Context,
	logger *zap.Logger,
	fileStore storage.FileStore,
	userID, chatID uuid.UUID,
	att *models.FileAttachment,
	wantThumb bool,
) ([]byte, string) {
	raw, mediaType := storage.ResolveAttachmentImageBytes(ctx, logger, fileStore, userID, att, wantThumb)
	if len(raw) == 0 && fileStore != nil && att.S3Key == "" {
		chatKey := storage.FileKeyForChat(userID, chatID, att.ID, att.Name)
		data, err := fileStore.DownloadFile(ctx, chatKey)
		if err == nil && len(data) > 0 {
			raw, mediaType = data, att.FileType
		} else if err != nil && logger != nil {
			logger.Debug("vision: chat key miss",
				zap.String("attachment_id", att.ID.String()),
				zap.String("key", chatKey),
				zap.Error(err))
		}
	}
	if len(raw) == 0 {
		if logger != nil {
			logger.Warn("vision: could not resolve image bytes",
				zap.String("attachment_id", att.ID.String()),
				zap.String("s3_key", att.S3Key))
		}
		return nil, ""
	}
	if mediaType == "" {
		mediaType = att.FileType
	}
	return raw, mediaType
}
