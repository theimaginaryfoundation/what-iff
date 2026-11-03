package buzzrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
)

// Endpoint is one Buzz community: the WebSocket URL clients connect to and the HTTP base of
// its bridge. Buzz selects the community from the host, so both share it.
type Endpoint struct {
	WS   string // ws(s)://host[:port]
	HTTP string // http(s)://host[:port]
}

// Host is the endpoint's host[:port].
func (e Endpoint) Host() string {
	u, _ := url.Parse(e.HTTP)
	if u == nil {
		return ""
	}
	return u.Host
}

// Secure reports whether the endpoint uses TLS.
func (e Endpoint) Secure() bool { return strings.HasPrefix(e.WS, "wss://") }

// ErrBadRelayURL is returned for a relay URL that cannot be a Buzz community.
var ErrBadRelayURL = errors.New("enter the community's address, like https://acme.communities.buzz.xyz")

// ParseEndpoint reads a community address as people paste it: a bare host, an https:// or
// wss:// URL, or an invite link (https://host/invite/<code>), in which case the invite code
// is returned too. Paths other than an invite are ignored (the host is the community).
// Plain http:// and ws:// are accepted only when insecure is true (local development).
func ParseEndpoint(raw string, insecure bool) (Endpoint, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, "", ErrBadRelayURL
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return Endpoint{}, "", ErrBadRelayURL
	}
	secure := true
	switch strings.ToLower(u.Scheme) {
	case "https", "wss":
	case "http", "ws":
		if !insecure {
			return Endpoint{}, "", errors.New("the community's address must use https:// (or wss://)")
		}
		secure = false
	default:
		return Endpoint{}, "", ErrBadRelayURL
	}
	host := strings.ToLower(u.Host)
	// The scheme's own port is dropped, so wss://host and wss://host:443 are one community
	// (the stored URL is how a persona's communities are told apart).
	if port := u.Port(); (secure && port == "443") || (!secure && port == "80") {
		host = strings.ToLower(u.Hostname())
		if strings.Contains(host, ":") {
			host = "[" + host + "]" // IPv6
		}
	}
	code := ""
	if rest, ok := strings.CutPrefix(u.Path, "/invite/"); ok {
		code = strings.Trim(rest, "/")
	}
	if secure {
		return Endpoint{WS: "wss://" + host, HTTP: "https://" + host}, code, nil
	}
	return Endpoint{WS: "ws://" + host, HTTP: "http://" + host}, code, nil
}

// Errors from the relay, for callers to tell apart.
var (
	// ErrNoAccess: the relay authenticated us but refuses: not a member, banned, or the
	// channel is closed to us.
	ErrNoAccess = errors.New("buzz: the community does not let this persona in")
	// ErrUnreachable: the relay could not be reached or answered with a server error.
	ErrUnreachable = errors.New("buzz: the community could not be reached")
)

// RejectedError is an event the relay refused, with its reason ("restricted: ...").
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string { return "buzz: event rejected: " + e.Reason }

// IsAccessReason reports whether a relay's rejection is about who we are rather than the
// event (NIP-01 machine-readable prefixes).
func IsAccessReason(reason string) bool {
	for _, p := range []string{"restricted:", "blocked:", "auth-required:"} {
		if strings.HasPrefix(reason, p) {
			return true
		}
	}
	return false
}

// Client talks to Buzz relays: the HTTP bridge and WebSocket connections. Unless
// AllowPrivate is set it connects only to public addresses (checked after DNS on every
// connection), because a relay URL is user input.
type Client struct {
	HTTP   *http.Client
	Dialer *websocket.Dialer
	// AllowPrivate allows loopback and private addresses and plain http/ws (local development).
	AllowPrivate bool
}

// NewClient returns the client the plugin uses.
func NewClient(allowPrivate bool) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = relaykit.PublicOnlyControl("buzz relay")
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: 4,
	}
	return &Client{
		AllowPrivate: allowPrivate,
		HTTP: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
			// The bridge never needs a redirect, and following one could leave the community.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		Dialer: &websocket.Dialer{
			NetDialContext:   dialer.DialContext,
			HandshakeTimeout: 15 * time.Second,
		},
	}
}

// RelayInfo is the part of a relay's NIP-11 document the relay uses.
type RelayInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Self is the relay's own pubkey (it signs the channel state events).
	Self       string `json:"self"`
	Software   string `json:"software"`
	Version    string `json:"version"`
	Limitation struct {
		AuthRequired     bool `json:"auth_required"`
		RestrictedWrites bool `json:"restricted_writes"`
		MaxMessageLength int  `json:"max_message_length"`
	} `json:"limitation"`
}

// IsBuzz reports whether the document comes from Buzz (other Nostr relays lack its bridge).
func (i *RelayInfo) IsBuzz() bool { return strings.Contains(i.Software, "buzz") }

// Info reads the relay's NIP-11 document.
func (c *Client) Info(ctx context.Context, ep Endpoint) (*RelayInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.HTTP+"/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/nostr+json")
	var info RelayInfo
	if err := c.do(req, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Signer is who a request or event is from: the persona's keys and, when the owner gave one,
// their attestation (sent as the x-auth-tag header so the relay can admit the persona
// through the owner's membership).
type Signer struct {
	Keys        *nostr.Keys
	Attestation *nostr.Attestation
}

// admission is the attestation to present for access (the NIP-42 AUTH event and the
// x-auth-tag header), or nil when there is none or it has expired. Only its created_at clauses
// are checked: NIP-AA does not evaluate kind= clauses at admission, so an attestation scoped to
// kind 9 still admits the persona. Leaving an expired one off lets the relay judge the persona
// on its own membership instead of rejecting an invalid tag.
func (s Signer) admission() *nostr.Attestation {
	if s.Attestation == nil || !s.Attestation.ValidAt(time.Now()) {
		return nil
	}
	return s.Attestation
}

// setAuthTag adds the x-auth-tag header (the admission attestation) to a request.
func (s Signer) setAuthTag(req *http.Request) {
	if att := s.admission(); att != nil {
		req.Header.Set("x-auth-tag", att.JSON())
	}
}

// Filter is a NIP-01 filter.
type Filter map[string]any

// Query runs filters through the HTTP bridge (POST /query).
func (c *Client) Query(ctx context.Context, ep Endpoint, s Signer, filters ...Filter) ([]nostr.Event, error) {
	body, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	req, err := c.signedPost(ctx, ep, s, "/query", body)
	if err != nil {
		return nil, err
	}
	var events []nostr.Event
	if err := c.do(req, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// Publish sends a signed event through the HTTP bridge (POST /events). A refused event is a
// *RejectedError carrying the relay's reason.
func (c *Client) Publish(ctx context.Context, ep Endpoint, s Signer, ev *nostr.Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := c.signedPost(ctx, ep, s, "/events", body)
	if err != nil {
		return err
	}
	var res struct {
		Accepted bool   `json:"accepted"`
		Message  string `json:"message"`
	}
	if err := c.do(req, &res); err != nil {
		return err
	}
	if !res.Accepted {
		return &RejectedError{Reason: res.Message}
	}
	return nil
}

// JoinPolicy is a community's join terms, when it has any.
type JoinPolicy struct {
	TermsMarkdown          string `json:"terms_markdown"`
	PrivacyMarkdown        string `json:"privacy_markdown"`
	AgeAttestationRequired bool   `json:"age_attestation_required"`
	Version                string `json:"version"`
}

// JoinPolicy reads the community's join terms (nil when it has none).
func (c *Client) JoinPolicy(ctx context.Context, ep Endpoint) (*JoinPolicy, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.HTTP+"/api/join-policy", nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Policy *JoinPolicy `json:"policy"`
	}
	if err := c.do(req, &res); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return res.Policy, nil
}

// ClaimInvite joins the community as s with an invite code. version is the join policy
// version the owner accepted for the persona ("" when the community has no terms), and
// ageConfirmed the owner's confirmation of its age requirement. It reports whether the
// persona was already a member.
func (c *Client) ClaimInvite(ctx context.Context, ep Endpoint, s Signer, code, policyVersion string, ageConfirmed bool) (bool, error) {
	claim := map[string]any{"code": code}
	if policyVersion != "" {
		receipt, err := c.acceptPolicy(ctx, ep, code, policyVersion, ageConfirmed)
		if err != nil {
			return false, err
		}
		claim["policy_receipt"] = receipt
	}
	body, _ := json.Marshal(claim)
	req, err := c.signedPost(ctx, ep, s, "/api/invites/claim", body)
	if err != nil {
		return false, err
	}
	var res struct {
		Status string `json:"status"`
	}
	if err := c.do(req, &res); err != nil {
		return false, err
	}
	return res.Status == "already_member", nil
}

// MintInvite creates an invite link to the community as s, who must be its owner or an admin
// (POST /api/invites). What Iff never mints invites for people; this is for rigs and tests.
func (c *Client) MintInvite(ctx context.Context, ep Endpoint, s Signer, ttl time.Duration, maxUses int) (string, error) {
	req := map[string]any{"ttl_secs": int(ttl.Seconds())}
	if maxUses > 0 {
		req["max_uses"] = maxUses
	}
	body, _ := json.Marshal(req)
	httpReq, err := c.signedPost(ctx, ep, s, "/api/invites", body)
	if err != nil {
		return "", err
	}
	var res struct {
		URL string `json:"url"`
	}
	if err := c.do(httpReq, &res); err != nil {
		return "", err
	}
	return res.URL, nil
}

func (c *Client) acceptPolicy(ctx context.Context, ep Endpoint, code, version string, ageConfirmed bool) (string, error) {
	body, _ := json.Marshal(map[string]any{"code": code, "policy_version": version, "age_confirmed": ageConfirmed})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.HTTP+"/api/invites/accept-policy", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var res struct {
		Receipt string `json:"receipt"`
	}
	if err := c.do(req, &res); err != nil {
		return "", err
	}
	return res.Receipt, nil
}

// signedPost builds a NIP-98 signed JSON POST to path.
func (c *Client) signedPost(ctx context.Context, ep Endpoint, s Signer, path string, body []byte) (*http.Request, error) {
	target := ep.HTTP + path
	auth, err := nostr.HTTPAuthHeader(s.Keys, http.MethodPost, target, body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	s.setAuthTag(req)
	return req, nil
}

var errNotFound = errors.New("buzz: not found")

// maxResponseBytes bounds what one bridge response may be (a query is capped by the relay
// at 1000 events; this leaves room for long messages).
const maxResponseBytes = 16 << 20

// do sends req and decodes a JSON response into out, mapping failures to the package's
// errors. Relay error bodies are {"error": "..."}; their text is kept for the reason.
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrNoAccess, errorText(raw, resp.Status))
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", ErrUnreachable, errorText(raw, resp.Status))
	case resp.StatusCode >= 400:
		return &RejectedError{Reason: errorText(raw, resp.Status)}
	case resp.StatusCode >= 300:
		return fmt.Errorf("%w: unexpected redirect", ErrUnreachable)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: unexpected response: %v", ErrUnreachable, err)
	}
	return nil
}

// errorText is a relay error body's message, or the HTTP status when it has none.
func errorText(raw []byte, status string) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if e.Error != "" {
			return e.Error
		}
		if e.Message != "" {
			return e.Message
		}
	}
	return status
}
