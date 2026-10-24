package discordrelay

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
)

// Discord accepts at most 10 files, and (without a boosted server) about 10 MiB, per
// message. The cap is applied to the whole set so one post never exceeds it.
const (
	MaxUploadFiles = 10
	MaxUploadBytes = 10 << 20
)

// FileReader reads stored attachment bytes (relaykit.FileReader; the object store satisfies it).
type FileReader = relaykit.FileReader

// outFile is a reply attachment loaded for upload.
type outFile = relaykit.ReplyFile

// toFiles builds fresh readers, so a retried post can send the same bytes again.
func toFiles(files []outFile) []File {
	out := make([]File, 0, len(files))
	for _, f := range files {
		out = append(out, File{Name: f.Name, ContentType: f.ContentType, Reader: bytes.NewReader(f.Data)})
	}
	return out
}

// loadReplyFiles reads the files saved with a reply for upload, within Discord's limits
// (relaykit.LoadReplyFiles): files left out are returned by name, and files the agent only
// fetched to look at are not posted.
func (s *Service) loadReplyFiles(ctx context.Context, reply *models.ChatMessage) (files []outFile, skipped []string) {
	if s.Files == nil {
		return nil, nil
	}
	return relaykit.LoadReplyFiles(ctx, s.Files, reply, MaxUploadFiles, MaxUploadBytes, s.Logger)
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
