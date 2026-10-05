package discordrelay

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Discord accepts at most 10 files, and (without a boosted server) about 10 MiB, per
// message. The cap is applied to the whole set so one post never exceeds it.
const (
	MaxUploadFiles = 10
	MaxUploadBytes = 10 << 20
)

// FileReader reads stored attachment bytes (the object store satisfies it).
// DownloadFile returns (nil, nil) when the key does not exist.
type FileReader interface {
	DownloadFile(ctx context.Context, key string) ([]byte, error)
}

// outFile is a reply attachment loaded for upload.
type outFile struct {
	name        string
	contentType string
	data        []byte
}

// toFiles builds fresh readers, so a retried post can send the same bytes again.
func toFiles(files []outFile) []File {
	out := make([]File, 0, len(files))
	for _, f := range files {
		out = append(out, File{Name: f.name, ContentType: f.contentType, Reader: bytes.NewReader(f.data)})
	}
	return out
}

// loadReplyFiles reads the files saved with a reply (generated images, say) for
// upload, within Discord's limits. Files left out, whether too many, too large or
// unreadable, are returned by name so the post can say so instead of dropping
// them silently.
func (s *Service) loadReplyFiles(ctx context.Context, reply *models.ChatMessage) (files []outFile, skipped []string) {
	if s.Files == nil {
		return nil, nil
	}
	total := 0
	for _, att := range reply.Attachments {
		if att == nil || att.S3Key == "" {
			continue
		}
		if len(files) >= MaxUploadFiles {
			skipped = append(skipped, att.Name)
			continue
		}
		data, err := s.Files.DownloadFile(ctx, att.S3Key)
		if err != nil || len(data) == 0 {
			if err != nil {
				s.Logger.Warn("discord relay: read reply attachment", zap.String("attachment_id", att.ID.String()), zap.Error(err))
			}
			skipped = append(skipped, att.Name)
			continue
		}
		if total+len(data) > MaxUploadBytes {
			skipped = append(skipped, att.Name)
			continue
		}
		total += len(data)
		files = append(files, outFile{name: att.Name, contentType: att.FileType, data: data})
	}
	return files, skipped
}

// withNote adds a short italic note after the last part, as a part of its own when
// it would not fit, and returns parts with the note included.
func withNote(parts []string, note string) []string {
	note = "_" + note + "_"
	if n := len(parts); n > 0 && utf8.RuneCountInString(parts[n-1])+2+utf8.RuneCountInString(note) <= MaxMessageRunes {
		parts[n-1] += "\n\n" + note
		return parts
	}
	return append(parts, note)
}

// skippedNote tells the channel which files could not be attached.
func skippedNote(names []string) string {
	return fmt.Sprintf("Couldn't attach to Discord (too large, too many, or unavailable): %s", strings.Join(names, ", "))
}

// noPermissionNote is added when Discord refuses the files but takes the text.
const noPermissionNote = "I don't have permission to attach files in this channel, so the files are only in What Iff."
