package discordrelay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"go.uber.org/zap"
)

// Limits on what one Discord message may bring into the relay thread. The app's own
// upload cap is 30 MB; this is lower so one tag cannot make the server pull in a
// large download.
const (
	MaxInboundAttachments = 4
	MaxInboundFileBytes   = 20 << 20
)

// Attachment is a file posted with a Discord message.
type Attachment struct {
	Filename    string
	ContentType string
	URL         string
	Size        int
}

// Fetcher downloads an attachment's bytes, reading at most max of them.
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string, max int64) ([]byte, error)
}

// errFetchTooLarge means the file is bigger than the cap.
var errFetchTooLarge = errors.New("discord attachment: too large")

// discordCDNHosts are the only hosts an attachment is fetched from. The URL comes from
// a Discord event, but the check keeps the server from fetching anywhere else.
func discordCDNHosts() map[string]bool {
	return map[string]bool{
		"cdn.discordapp.com":   true,
		"media.discordapp.net": true,
	}
}

func allowedAttachmentURL(u *url.URL, hosts map[string]bool) bool {
	allowed, ok := hosts[strings.ToLower(u.Hostname())]
	return u.Scheme == "https" && ok && allowed
}

// CDNFetcher fetches from Discord's CDN over HTTPS only, following no redirect that
// leaves it.
type CDNFetcher struct {
	Client *http.Client
	// Hosts is the set of hosts it may fetch from (Discord's CDN hosts by default).
	Hosts map[string]bool
}

// NewCDNFetcher is the fetcher the plugin uses. Besides the host allowlist it refuses to
// connect to any address that is not public, checked after DNS resolution on every
// connection, so a name that resolved to an internal address (or is rebound to one) still
// cannot reach the server's own network.
func NewCDNFetcher() *CDNFetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: publicOnlyControl}
	hosts := discordCDNHosts()
	return &CDNFetcher{Hosts: hosts, Client: &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2:   true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !allowedAttachmentURL(req.URL, hosts) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}}
}

// cgnat is the shared address space (100.64.0.0/10), which net.IP.IsPrivate does not cover.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isPublicIP reports whether ip is an address on the public internet: not loopback,
// private, link-local (including the cloud metadata address), shared, multicast or unspecified.
func isPublicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !cgnat.Contains(ip)
}

// publicOnlyControl is a net.Dialer Control hook: it runs with the resolved address just
// before each connection is made.
func publicOnlyControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !isPublicIP(ip) {
		return fmt.Errorf("discord attachment: refusing to connect to non-public address %s", host)
	}
	return nil
}

func (f *CDNFetcher) Fetch(ctx context.Context, rawURL string, max int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !allowedAttachmentURL(u, f.Hosts) {
		return nil, fmt.Errorf("discord attachment: refusing to fetch %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discord attachment: fetch returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errFetchTooLarge
	}
	return data, nil
}

// ingestInbound saves the files posted with a tag to the relay thread, so the persona
// sees them with the message. Files that cannot be added (a type the app does not take,
// too large, too many, a failed download) are named in notes for the persona to pass on,
// rather than dropped silently.
func (s *Service) ingestInbound(ctx context.Context, ownerID, chatID uuid.UUID, msg InboundMessage) (atts []*models.FileAttachment, notes []string) {
	if len(msg.Attachments) == 0 {
		return nil, nil
	}
	if s.Ingest == nil || s.Fetch == nil {
		return nil, []string{fmt.Sprintf("[%d file(s) posted with this message could not be added here]", len(msg.Attachments))}
	}
	skip := func(a Attachment, why string) {
		notes = append(notes, fmt.Sprintf("[File %q was not added: %s]", a.Filename, why))
	}
	for i, a := range msg.Attachments {
		switch {
		case len(atts) >= MaxInboundAttachments:
			skip(a, fmt.Sprintf("at most %d files are added per message", MaxInboundAttachments))
			continue
		case a.Size > MaxInboundFileBytes:
			skip(a, "too large")
			continue
		}
		data, err := s.Fetch.Fetch(ctx, a.URL, MaxInboundFileBytes)
		if err != nil {
			if errors.Is(err, errFetchTooLarge) {
				skip(a, "too large")
			} else {
				s.Logger.Warn("discord relay: fetch attachment", zap.Int("index", i), zap.Error(err))
				skip(a, "it could not be downloaded")
			}
			continue
		}
		att, err := s.Ingest.IngestAttachment(ctx, plugins.AttachmentUpload{UserID: ownerID, ChatID: chatID, Name: a.Filename, Data: data})
		switch {
		case err == nil:
			atts = append(atts, att)
		case errors.Is(err, plugins.ErrAttachmentUnsupported):
			skip(a, "this file type is not supported")
		case errors.Is(err, plugins.ErrAttachmentTooLarge):
			skip(a, "too large")
		default:
			s.Logger.Warn("discord relay: ingest attachment", zap.Int("index", i), zap.Error(err))
			skip(a, "it could not be saved")
		}
	}
	return atts, notes
}
