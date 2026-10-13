package remotesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/netguard"
)

// The remote's webhook read routes (ADR 0x023), as a sync client uses them. Everything here is a
// GET with the stored wht_ token; nothing is ever written to the remote.

const (
	// PageSize is what the client asks for on every list; the remote clamps to the same maximum.
	PageSize = 100
	// maxResponseBytes bounds one response. A page of 100 messages is far below it; anything
	// bigger is not a What Iff server answering the routes we asked for.
	maxResponseBytes = 16 << 20
	requestTimeout   = 30 * time.Second
	userAgent        = "what-iff-remote-sync/1"
)

// ErrorKind classifies a failed remote call. The kind decides what a run does next and what the
// owner is told; upstream bodies are never shown.
type ErrorKind string

const (
	// KindUnreachable: DNS, connection, TLS or timeout. Retried next run.
	KindUnreachable ErrorKind = "unreachable"
	// KindUnauthorized: 401, the remote does not know the token (mistyped or revoked).
	KindUnauthorized ErrorKind = "unauthorized"
	// KindForbidden: 403, the token exists but lacks chat:read.
	KindForbidden ErrorKind = "forbidden"
	// KindNotFound: 404 for a thread (deleted remotely), or no webhook routes at all.
	KindNotFound ErrorKind = "not_found"
	// KindRateLimited: 429. Retried next run.
	KindRateLimited ErrorKind = "rate_limited"
	// KindServer: 5xx. Retried next run.
	KindServer ErrorKind = "server"
	// KindBadResponse: a status or body this client does not understand (a redirect, HTML, an
	// oversized body). Usually a wrong URL.
	KindBadResponse ErrorKind = "bad_response"
)

// Error is a failed remote call.
type Error struct {
	Kind   ErrorKind
	Status int
	Err    error
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("remote %s (HTTP %d)", e.Kind, e.Status)
	}
	if e.Err != nil {
		return fmt.Sprintf("remote %s: %v", e.Kind, e.Err)
	}
	return "remote " + string(e.Kind)
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf returns the kind of a remote error, or "" for any other error.
func KindOf(err error) ErrorKind {
	var re *Error
	if errors.As(err, &re) {
		return re.Kind
	}
	return ""
}

// NeedsOwner reports whether err means the token itself was refused, so retrying is pointless
// until the owner changes the settings.
func NeedsOwner(err error) bool {
	k := KindOf(err)
	return k == KindUnauthorized || k == KindForbidden
}

// UserMessage is what the settings page shows for a failed check or run.
func UserMessage(err error) string {
	switch KindOf(err) {
	case KindUnreachable:
		return "Could not reach the remote server. Check the address, or try again later."
	case KindUnauthorized:
		return "The remote server did not accept the token. It may have been revoked; create a new one under Integrations → Webhooks there."
	case KindForbidden:
		return "The token cannot read threads. Create one with “Read threads” (chat:read) under Integrations → Webhooks on the remote server."
	case KindNotFound:
		return "That address does not look like a What Iff server (no webhook routes). Check the address."
	case KindRateLimited:
		return "The remote server asked us to slow down. Sync will try again later."
	case KindServer:
		return "The remote server had a problem. Sync will try again later."
	case KindBadResponse:
		return "The remote server answered in a way sync does not understand. Check the address."
	default:
		return "Sync failed unexpectedly. It will try again later."
	}
}

// DefaultBaseURL is the hosted app's API origin, the remote most people sync from.
const DefaultBaseURL = "https://api.whatiff.chat"

// hostedAppHosts are the hosted app's web addresses, which people paste from their browser; its
// API is served from DefaultBaseURL instead.
var hostedAppHosts = map[string]bool{"whatiff.chat": true, "www.whatiff.chat": true, "app.whatiff.chat": true}

// NormalizeBaseURL checks and tidies a remote server address: https only (http when
// allowLocal), a host, no credentials, query or fragment, and no trailing slash or /api
// suffix (the client adds /api itself).
func NormalizeBaseURL(raw string, allowLocal bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("enter the remote server's address, e.g. https://whatiff.chat")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("the remote address is not a valid URL")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowLocal {
			return "", errors.New("the remote address must use https")
		}
	default:
		return "", errors.New("the remote address must use https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("the remote address must be just the server, e.g. https://whatiff.chat")
	}
	if hostedAppHosts[strings.ToLower(u.Hostname())] && u.Scheme == "https" {
		return DefaultBaseURL, nil
	}
	p := strings.TrimRight(u.Path, "/")
	p = strings.TrimSuffix(p, "/api")
	u.Path = strings.TrimRight(p, "/")
	u.RawPath = ""
	return u.String(), nil
}

// Client reads one remote account through its webhook routes.
type Client struct {
	base  string
	host  string
	token string
	http  *http.Client
}

// NewHTTPClient is the HTTP client sync uses. Unless allowLocal, it connects to public addresses
// only (checked after DNS, on every connection), so a user-chosen address cannot reach the
// server's own network or the cloud metadata service.
func NewHTTPClient(allowLocal bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowLocal {
		dialer = netguard.PublicOnlyDialer(10 * time.Second)
	}
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}
}

// NewClient builds a client for an already-normalized base URL. httpClient may be nil, which
// means NewHTTPClient(false). Whatever client is used, redirects are never followed: a token
// must never be replayed to another host.
func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid remote base URL")
	}
	if httpClient == nil {
		httpClient = NewHTTPClient(false)
	}
	// Never follow redirects, whatever client was passed in.
	c := *httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(baseURL, "/"), host: u.Host, token: token, http: &c}, nil
}

// Host is the remote's host name, used to label forks.
func (c *Client) Host() string { return c.host }

// Check proves the token can read: the cheapest chat:read route, one persona.
func (c *Client) Check(ctx context.Context) error {
	var page struct {
		Results []json.RawMessage `json:"results"`
	}
	return c.get(ctx, "/api/webhooks/personality", url.Values{"limit": {"1"}}, &page)
}

// RemoteChat is a thread as the remote's thread list returns it.
type RemoteChat struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	PersonalityName string     `json:"personality_name"`
	LastMessageTime *time.Time `json:"last_message_time"`
	Archived        *bool      `json:"archived"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ListChats returns one page of threads, newest activity first; archived=true lists the archive.
func (c *Client) ListChats(ctx context.Context, page int, archived bool) ([]RemoteChat, error) {
	var out struct {
		Results []RemoteChat `json:"results"`
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(PageSize)}}
	if archived {
		q.Set("archived", "true")
	}
	if err := c.get(ctx, "/api/webhooks/chat", q, &out); err != nil {
		return nil, err
	}
	for i := range out.Results {
		if out.Results[i].ID == uuid.Nil {
			return nil, &Error{Kind: KindBadResponse, Err: errors.New("thread without an id")}
		}
		if out.Results[i].Archived == nil {
			out.Results[i].Archived = &archived
		}
	}
	return out.Results, nil
}

// ListMessages returns one page of a thread's messages, newest first, and the cursor for the
// next (older) page. An empty cursor starts at the newest message.
func (c *Client) ListMessages(ctx context.Context, chatID uuid.UUID, cursor string) ([]models.WebhookMessage, string, error) {
	var out struct {
		Results    []models.WebhookMessage `json:"results"`
		NextCursor string                  `json:"next_cursor"`
	}
	q := url.Values{"limit": {strconv.Itoa(PageSize)}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if err := c.get(ctx, "/api/webhooks/chat/"+chatID.String()+"/messages", q, &out); err != nil {
		return nil, "", err
	}
	return out.Results, out.NextCursor, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return &Error{Kind: KindBadResponse, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Kind: KindUnreachable, Err: err}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return &Error{Kind: KindUnauthorized, Status: resp.StatusCode}
	case resp.StatusCode == http.StatusForbidden:
		return &Error{Kind: KindForbidden, Status: resp.StatusCode}
	case resp.StatusCode == http.StatusNotFound:
		return &Error{Kind: KindNotFound, Status: resp.StatusCode}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &Error{Kind: KindRateLimited, Status: resp.StatusCode}
	case resp.StatusCode >= 500:
		return &Error{Kind: KindServer, Status: resp.StatusCode}
	default:
		return &Error{Kind: KindBadResponse, Status: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return &Error{Kind: KindUnreachable, Err: err}
	}
	if len(body) > maxResponseBytes {
		return &Error{Kind: KindBadResponse, Err: errors.New("response too large")}
	}
	if err := json.Unmarshal(body, into); err != nil {
		return &Error{Kind: KindBadResponse, Err: errors.New("response is not the expected JSON")}
	}
	return nil
}
