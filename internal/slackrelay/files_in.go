package slackrelay

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"go.uber.org/zap"
)

// Limits on what the messages of one turn may bring into the relay thread (the same as
// Discord's): the app's own upload cap is 30 MB, and this is lower so one mention cannot make
// the server pull in a large download.
const (
	MaxInboundAttachments = 4
	MaxInboundFileBytes   = 20 << 20
)

// ingestInbound saves the files posted with a mention to the relay thread, so the persona sees
// them with the message. Files that cannot be added (deleted, hidden by the workspace's plan,
// a type the app does not take, too large, too many, a failed download) are named in notes
// for the persona to pass on, rather than dropped silently. room is how many more files the
// turn takes (a batched turn shares MaxInboundAttachments across its mentions).
func (s *Service) ingestInbound(ctx context.Context, app models.SlackAppCredentials, chatID uuid.UUID, m Message, room int) (atts []*models.FileAttachment, notes []string) {
	if len(m.Files) == 0 {
		return nil, nil
	}
	if s.Ingest == nil {
		return nil, []string{fmt.Sprintf("[%d file(s) posted with this message could not be added here]", len(m.Files))}
	}
	for i, f := range m.Files {
		name := f.Name
		if name == "" {
			name = f.Title
		}
		skip := func(why string) {
			notes = append(notes, fmt.Sprintf("[File %q was not added: %s]", name, why))
		}
		src := f.URLPrivateDownload
		if src == "" {
			src = f.URLPrivate
		}
		switch {
		case f.Mode == "tombstone":
			skip("it was deleted")
			continue
		case f.Mode == "hidden_by_limit":
			skip("the workspace's plan hides it")
			continue
		case f.FileAccess == "check_file_info" || src == "":
			skip("Slack did not share its contents with the app")
			continue
		case len(atts) >= room:
			skip(fmt.Sprintf("at most %d files are added at a time", MaxInboundAttachments))
			continue
		case f.Size > MaxInboundFileBytes:
			skip("too large")
			continue
		}
		data, err := s.Slack.Download(ctx, app.BotToken, src, MaxInboundFileBytes)
		if err != nil {
			if errors.Is(err, errFileTooLarge) {
				skip("too large")
			} else {
				s.Logger.Warn("slack relay: download file", zap.Int("index", i), zap.Error(err))
				skip("it could not be downloaded")
			}
			continue
		}
		att, err := s.Ingest.IngestAttachment(ctx, plugins.AttachmentUpload{UserID: app.OwnerID, ChatID: chatID, Name: name, Data: data})
		switch {
		case err == nil:
			atts = append(atts, att)
		case errors.Is(err, plugins.ErrAttachmentUnsupported):
			skip("this file type is not supported")
		case errors.Is(err, plugins.ErrAttachmentTooLarge):
			skip("too large")
		default:
			s.Logger.Warn("slack relay: ingest file", zap.Int("index", i), zap.Error(err))
			skip("it could not be saved")
		}
	}
	return atts, notes
}
