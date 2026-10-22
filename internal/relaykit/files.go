package relaykit

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// FileReader reads stored attachment bytes (the object store satisfies it). DownloadFile
// returns (nil, nil) when the key does not exist.
type FileReader interface {
	DownloadFile(ctx context.Context, key string) ([]byte, error)
}

// ReplyFile is a reply attachment loaded for posting.
type ReplyFile struct {
	Name        string
	ContentType string
	Data        []byte
}

// LoadReplyFiles reads the files saved with a reply (generated images, say) for posting,
// within a platform's limits: at most maxFiles, maxBytes in all. Files left out, whether too
// many, too large or unreadable, are returned by name so the post can say so instead of
// dropping them silently. Existing files the agent only fetched to look at (ContextOnly) are
// not the reply's output and are neither posted nor mentioned.
func LoadReplyFiles(ctx context.Context, files FileReader, reply *models.ChatMessage, maxFiles, maxBytes int, logger *zap.Logger) (out []ReplyFile, skipped []string) {
	if files == nil || reply == nil {
		return nil, nil
	}
	total := 0
	for _, att := range reply.Attachments {
		if att == nil || att.S3Key == "" || att.ContextOnly {
			continue
		}
		if len(out) >= maxFiles {
			skipped = append(skipped, att.Name)
			continue
		}
		data, err := files.DownloadFile(ctx, att.S3Key)
		if err != nil || len(data) == 0 {
			if err != nil && logger != nil {
				logger.Warn("relay: read reply attachment", zap.String("attachment_id", att.ID.String()), zap.Error(err))
			}
			skipped = append(skipped, att.Name)
			continue
		}
		if total+len(data) > maxBytes {
			skipped = append(skipped, att.Name)
			continue
		}
		total += len(data)
		out = append(out, ReplyFile{Name: att.Name, ContentType: att.FileType, Data: data})
	}
	return out, skipped
}
