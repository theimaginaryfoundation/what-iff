package buzzrelay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"go.uber.org/zap"
)

// Limits on files crossing the relay. Inbound is bounded below the app's 30 MB upload cap so
// one mention cannot make the server pull in a large download; outbound stays within what a
// Buzz relay takes for images and files by default.
const (
	MaxInboundAttachments = 4
	MaxInboundFileBytes   = 20 << 20
	MaxUploadFiles        = 10
	MaxUploadBytes        = 50 << 20
)

// KindBlossomAuth authorises a Blossom media upload (BUD-11).
const KindBlossomAuth = 24242

// Blob is what the community's media store says about an uploaded file (BUD-02).
type Blob struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Type   string `json:"type"`
	// Name is the file name as uploaded (an image re-encoded by PrepareMedia is a .png).
	Name string `json:"-"`
}

// Imeta is the file's imeta tag (NIP-92), as Buzz expects it on a message.
func (b *Blob) Imeta(filename string) []string {
	tag := []string{"imeta", "url " + b.URL, "m " + b.Type, "x " + b.SHA256, "size " + strconv.FormatInt(b.Size, 10)}
	if filename = strings.TrimSpace(filename); filename != "" {
		tag = append(tag, "filename "+filename)
	}
	return tag
}

// PrepareMedia readies a file for a Buzz media store: images are re-encoded as PNG (scaled to
// the app's upload size), because Buzz refuses images that carry metadata (EXIF, PNG text
// chunks) and stripping it is what the Buzz app does too. Anything else is returned as is.
func PrepareMedia(name, contentType string, data []byte) (string, string, []byte) {
	if !strings.HasPrefix(contentType, "image/") {
		contentType = http.DetectContentType(data)
	}
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return name, contentType, data
	}
	var out bytes.Buffer
	pngName, err := imageutil.NormalizeForUpload(bytes.NewReader(data), &out, name, imageutil.DefaultUploadImageMaxPx)
	if err != nil {
		return name, contentType, data // not decodable: let the relay judge it
	}
	return pngName, imageutil.NormalizedUploadContentType, out.Bytes()
}

// Upload stores a file in the community's media store (PUT /upload) as s. Images are passed
// through PrepareMedia first.
func (c *Client) Upload(ctx context.Context, ep Endpoint, s Signer, name, contentType string, data []byte) (*Blob, error) {
	name, contentType, data = PrepareMedia(name, contentType, data)
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	auth, err := blossomAuth(s, ep, "upload", digest, "Upload "+path.Base(name))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, ep.HTTP+"/upload", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-SHA-256", digest)
	s.setAuthTag(req, KindBlossomAuth)
	var blob Blob
	if err := c.do(req, &blob); err != nil {
		return nil, err
	}
	if blob.URL == "" || blob.SHA256 != digest {
		return nil, fmt.Errorf("%w: the media store returned an unexpected descriptor", ErrUnreachable)
	}
	blob.Name = name
	return &blob, nil
}

// blossomAuth is a BUD-11 Authorization header for one media operation (verb "upload" or
// "get") on the blob with hex digest x, valid for under a minute. The server tag names the
// community's host as Buzz binds it (port included; a default port normalises away).
func blossomAuth(s Signer, ep Endpoint, verb, x, content string) (string, error) {
	tags := nostr.Tags{
		{"t", verb},
		{"expiration", strconv.FormatInt(time.Now().Add(50*time.Second).Unix(), 10)},
		{"server", ep.Host()},
	}
	if x != "" {
		tags = append(tags, []string{"x", x})
	}
	ev := &nostr.Event{Kind: KindBlossomAuth, Tags: tags, Content: content}
	if err := sign(s, ev); err != nil {
		return "", err
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	return "Nostr " + base64.StdEncoding.EncodeToString(raw), nil
}

// errMediaTooLarge means a file is bigger than the cap.
var errMediaTooLarge = errors.New("buzz media: too large")

// FetchMedia downloads a file posted in a community as s, from that community's own host only
// (a message's URLs are the author's to choose), reading at most max bytes. Buzz media is
// members-only, so the request carries a Blossom "get" authorization for the blob.
func (c *Client) FetchMedia(ctx context.Context, ep Endpoint, s Signer, rawURL string, max int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	base, _ := url.Parse(ep.HTTP)
	if err != nil || base == nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) {
		return nil, fmt.Errorf("buzz media: refusing to fetch %q from outside the community", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	// The blob's digest is its file name (/media/<sha256>.<ext>).
	digest := strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
	auth, err := blossomAuth(s, ep, "get", digest, "Get "+path.Base(u.Path))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	s.setAuthTag(req, KindBlossomAuth)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("buzz media: fetch returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errMediaTooLarge
	}
	return data, nil
}

// MediaRef is a file attached to a message (one imeta tag).
type MediaRef struct {
	URL      string
	Type     string
	Size     int64
	Filename string
}

// MediaOf reads a message's imeta tags.
func MediaOf(ev nostr.Event) []MediaRef {
	var out []MediaRef
	for _, tag := range ev.Tags {
		if len(tag) < 2 || tag[0] != "imeta" {
			continue
		}
		var m MediaRef
		for _, part := range tag[1:] {
			key, value, _ := strings.Cut(part, " ")
			switch key {
			case "url":
				m.URL = value
			case "m":
				m.Type = value
			case "size":
				m.Size, _ = strconv.ParseInt(value, 10, 64)
			case "filename":
				m.Filename = value
			}
		}
		if m.URL == "" {
			continue
		}
		if m.Filename == "" {
			m.Filename = path.Base(m.URL)
		}
		out = append(out, m)
	}
	return out
}

// ingestInbound saves the files posted with a mention to the relay thread, so the persona sees
// them with the message. Files that cannot be added are named in notes for the persona to pass
// on, rather than dropped silently. room is how many more files the turn takes (a batched turn
// shares MaxInboundAttachments across its mentions).
func (s *Service) ingestInbound(ctx context.Context, ep Endpoint, signer Signer, ownerID, chatID uuid.UUID, ev nostr.Event, room int) (atts []*models.FileAttachment, notes []string) {
	media := MediaOf(ev)
	if len(media) == 0 {
		return nil, nil
	}
	if s.Ingest == nil {
		return nil, []string{fmt.Sprintf("[%d file(s) posted with this message could not be added here]", len(media))}
	}
	skip := func(m MediaRef, why string) {
		notes = append(notes, fmt.Sprintf("[File %q was not added: %s]", m.Filename, why))
	}
	// The cap is on attempts, not successes: a message can carry any number of imeta tags, and
	// each failed fetch would otherwise hold the thread's turn queue and add a note. room is
	// what the batch has left of MaxInboundAttachments.
	room = max(0, min(room, MaxInboundAttachments))
	extra := len(media) - room
	if extra > 0 {
		media = media[:room]
	}
	for i, m := range media {
		switch {
		case m.Size > MaxInboundFileBytes:
			skip(m, "too large")
			continue
		}
		data, err := s.Relay.FetchMedia(ctx, ep, signer, m.URL, MaxInboundFileBytes)
		if err != nil {
			if errors.Is(err, errMediaTooLarge) {
				skip(m, "too large")
			} else {
				s.Logger.Warn("buzz relay: fetch attachment", zap.Int("index", i), zap.Error(err))
				skip(m, "it could not be downloaded")
			}
			continue
		}
		att, err := s.Ingest.IngestAttachment(ctx, plugins.AttachmentUpload{UserID: ownerID, ChatID: chatID, Name: m.Filename, Data: data})
		switch {
		case err == nil:
			atts = append(atts, att)
		case errors.Is(err, plugins.ErrAttachmentUnsupported):
			skip(m, "this file type is not supported")
		case errors.Is(err, plugins.ErrAttachmentTooLarge):
			skip(m, "too large")
		default:
			s.Logger.Warn("buzz relay: ingest attachment", zap.Int("index", i), zap.Error(err))
			skip(m, "it could not be saved")
		}
	}
	if extra > 0 {
		notes = append(notes, fmt.Sprintf("[%d more file(s) were not added: at most %d files are read at a time]", extra, MaxInboundAttachments))
	}
	return atts, notes
}

// uploadReplyFiles uploads the files the reply produced to the community's media store and
// returns their imeta tags; files that could not be posted are returned by name.
func (s *Service) uploadReplyFiles(ctx context.Context, ep Endpoint, signer Signer, reply *models.ChatMessage) (media [][]string, skipped []string) {
	files, skipped := relaykit.LoadReplyFiles(ctx, s.Files, reply, MaxUploadFiles, MaxUploadBytes, s.Logger)
	for _, f := range files {
		blob, err := s.Relay.Upload(ctx, ep, signer, f.Name, f.ContentType, f.Data)
		if err != nil {
			s.Logger.Warn("buzz relay: upload reply file", zap.String("name", f.Name), zap.Error(err))
			skipped = append(skipped, f.Name)
			continue
		}
		media = append(media, blob.Imeta(blob.Name))
	}
	return media, skipped
}

// skippedNote tells the channel which files could not be attached.
func skippedNote(names []string) string {
	return fmt.Sprintf("Couldn't attach to Buzz (too large, too many, or unavailable): %s", strings.Join(names, ", "))
}
