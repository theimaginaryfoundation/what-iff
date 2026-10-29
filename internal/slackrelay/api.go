package slackrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
)

// DefaultAPIBase is Slack's Web API.
const DefaultAPIBase = "https://slack.com/api/"

// Error is a Web API call Slack answered with ok:false (Code is its error string, such as
// "invalid_auth" or "not_in_channel"), or with 429 (Code "ratelimited", RetryAfter set).
type Error struct {
	Method     string
	Code       string
	RetryAfter time.Duration
}

func (e *Error) Error() string { return "slack " + e.Method + ": " + e.Code }

// Codes Slack answers when a token no longer works: the app is then marked invalid_token.
var authCodes = map[string]bool{
	"invalid_auth": true, "not_authed": true, "account_inactive": true, "token_revoked": true,
	"token_expired": true, "app_uninstalled": true, "team_access_not_granted": true,
	"not_allowed_token_type": true, "invalid_token": true,
}

// Codes that mean the app cannot post in a channel: the binding is then marked broken.
var channelCodes = map[string]bool{
	"not_in_channel": true, "channel_not_found": true, "is_archived": true,
	"restricted_action": true, "restricted_action_read_only_channel": true,
	"restricted_action_thread_only_channel": true, "restricted_action_non_threadable_channel": true,
	"ekm_access_denied": true, "team_access_not_granted_to_channel": true,
}

// Codes worth retrying: Slack's own transient failures.
var transientCodes = map[string]bool{
	"ratelimited": true, "internal_error": true, "fatal_error": true,
	"service_unavailable": true, "request_timeout": true,
}

// IsAuthError reports whether err is Slack refusing the token itself.
func IsAuthError(err error) bool {
	var e *Error
	return errors.As(err, &e) && authCodes[e.Code]
}

// IsChannelError reports whether err is Slack refusing the app in one channel.
func IsChannelError(err error) bool {
	var e *Error
	return errors.As(err, &e) && channelCodes[e.Code]
}

// IsTransient reports whether err may succeed if tried again: a network failure, a rate
// limit, or one of Slack's own transient errors. Slack refusing the request is not.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return transientCodes[e.Code]
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, errFileTooLarge) && !errors.Is(err, errFileHost)
}

// API is a small client for the Web API methods the relay uses. Every call is a form POST
// with the token in the Authorization header, which every method accepts.
type API struct {
	// Base is the Web API's URL, ending in "/" (DefaultAPIBase).
	Base string
	HTTP *http.Client
	// FileHosts are the hosts files are downloaded from and uploaded to with the bot token
	// (files.slack.com). The token is never sent anywhere else.
	FileHosts map[string]bool
	// allowHTTP lets tests use an httptest server for file transfers.
	allowHTTP bool
}

// NewAPI is the client the plugin uses: Slack's own hosts, over TLS, through the public-address
// dial guard, with no redirect off Slack's file host.
func NewAPI() *API {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: relaykit.PublicOnlyControl("slack")}
	a := &API{Base: DefaultAPIBase, FileHosts: map[string]bool{"files.slack.com": true}}
	a.HTTP = &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2:   true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !a.fileURLAllowed(req.URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return a
}

// response is the envelope every Web API answer shares.
type response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// call POSTs params to a Web API method and decodes the answer into out (which may embed
// nothing of response; ok and error are read separately).
func (a *API) call(ctx context.Context, token, method string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Base+method, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("slack %s: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if wait <= 0 {
			wait = 1
		}
		return &Error{Method: method, Code: "ratelimited", RetryAfter: time.Duration(wait) * time.Second}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("slack %s: %w", method, err)
	}
	if resp.StatusCode >= 500 {
		return &Error{Method: method, Code: "service_unavailable"}
	}
	var env response
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("slack %s: %s: %w", method, resp.Status, err)
	}
	if !env.OK {
		code := env.Error
		if code == "" {
			code = "unknown_error"
		}
		return &Error{Method: method, Code: code}
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("slack %s: %w", method, err)
		}
	}
	return nil
}

// Identity is who a bot token speaks as (auth.test).
type Identity struct {
	TeamID string `json:"team_id"`
	Team   string `json:"team"`
	URL    string `json:"url"`
	UserID string `json:"user_id"`
	User   string `json:"user"`
	BotID  string `json:"bot_id"`
}

// AuthTest checks a bot token and says whose it is.
func (a *API) AuthTest(ctx context.Context, botToken string) (*Identity, error) {
	var out Identity
	if err := a.call(ctx, botToken, "auth.test", url.Values{}, &out); err != nil {
		return nil, err
	}
	if out.BotID == "" || out.UserID == "" {
		return nil, &Error{Method: "auth.test", Code: "not_a_bot_token"}
	}
	return &out, nil
}

// BotInfo is a bot's app (bots.info).
type BotInfo struct {
	ID     string `json:"id"`
	AppID  string `json:"app_id"`
	Name   string `json:"name"`
	UserID string `json:"user_id"`
}

// Bot reads a bot's app id and name.
func (a *API) Bot(ctx context.Context, botToken, botID string) (*BotInfo, error) {
	var out struct {
		Bot BotInfo `json:"bot"`
	}
	if err := a.call(ctx, botToken, "bots.info", url.Values{"bot": {botID}}, &out); err != nil {
		return nil, err
	}
	return &out.Bot, nil
}

// OpenConnection asks for a Socket Mode WebSocket URL (apps.connections.open, app-level token).
func (a *API) OpenConnection(ctx context.Context, appToken string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := a.call(ctx, appToken, "apps.connections.open", url.Values{}, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", &Error{Method: "apps.connections.open", Code: "no_url"}
	}
	return out.URL, nil
}

// PostMessage posts text (Slack mrkdwn) to a channel, in a thread when threadTS is set, and
// returns the new message's ts. Mentions are never parsed from the text (link_names off), and
// the text is escaped by the caller (ToMrkdwn).
func (a *API) PostMessage(ctx context.Context, botToken, channel, threadTS, text string) (string, error) {
	params := url.Values{"channel": {channel}, "text": {text}, "link_names": {"false"}}
	if threadTS != "" {
		params.Set("thread_ts", threadTS)
	}
	var out struct {
		TS string `json:"ts"`
	}
	if err := a.call(ctx, botToken, "chat.postMessage", params, &out); err != nil {
		return "", err
	}
	return out.TS, nil
}

// AddReaction and RemoveReaction set the bot's emoji reaction on a message.
func (a *API) AddReaction(ctx context.Context, botToken, channel, ts, name string) error {
	return a.call(ctx, botToken, "reactions.add", url.Values{"channel": {channel}, "timestamp": {ts}, "name": {name}}, nil)
}

func (a *API) RemoveReaction(ctx context.Context, botToken, channel, ts, name string) error {
	return a.call(ctx, botToken, "reactions.remove", url.Values{"channel": {channel}, "timestamp": {ts}, "name": {name}}, nil)
}

// User is a workspace member, as the relay names them.
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	IsBot    bool   `json:"is_bot"`
	Deleted  bool   `json:"deleted"`
	Profile  struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
	} `json:"profile"`
}

// DisplayName is what a person is called in Slack: their display name, else their full name,
// else their handle.
func (u User) DisplayName() string {
	for _, n := range []string{u.Profile.DisplayName, u.Profile.RealName, u.RealName, u.Name} {
		if n = strings.TrimSpace(n); n != "" {
			return n
		}
	}
	return ""
}

// UserInfo reads one member (users.info).
func (a *API) UserInfo(ctx context.Context, botToken, userID string) (*User, error) {
	var out struct {
		User User `json:"user"`
	}
	if err := a.call(ctx, botToken, "users.info", url.Values{"user": {userID}}, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

// Channel is a conversation the app can see.
type Channel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsPrivate  bool   `json:"is_private"`
	IsMember   bool   `json:"is_member"`
	IsArchived bool   `json:"is_archived"`
	NumMembers int    `json:"num_members,omitempty"`
}

// maxChannelPages bounds conversations.list (200 channels a page).
const maxChannelPages = 10

// Channels lists the workspace's public channels and the private channels the app is in,
// without archived ones.
func (a *API) Channels(ctx context.Context, botToken string) ([]Channel, error) {
	var all []Channel
	cursor := ""
	for range maxChannelPages {
		params := url.Values{"types": {"public_channel,private_channel"}, "exclude_archived": {"true"}, "limit": {"200"}}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		var out struct {
			Channels []Channel `json:"channels"`
			Meta     struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := a.call(ctx, botToken, "conversations.list", params, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Channels...)
		cursor = out.Meta.NextCursor
		if cursor == "" {
			break
		}
	}
	return all, nil
}

// ChannelInfo reads one conversation (conversations.info).
func (a *API) ChannelInfo(ctx context.Context, botToken, channel string) (*Channel, error) {
	var out struct {
		Channel Channel `json:"channel"`
	}
	if err := a.call(ctx, botToken, "conversations.info", url.Values{"channel": {channel}}, &out); err != nil {
		return nil, err
	}
	return &out.Channel, nil
}

// JoinChannel adds the app to a public channel (conversations.join).
func (a *API) JoinChannel(ctx context.Context, botToken, channel string) (*Channel, error) {
	var out struct {
		Channel Channel `json:"channel"`
	}
	if err := a.call(ctx, botToken, "conversations.join", url.Values{"channel": {channel}}, &out); err != nil {
		return nil, err
	}
	return &out.Channel, nil
}

// ThreadParent reads the first message of a thread (conversations.replies, limit 1).
func (a *API) ThreadParent(ctx context.Context, botToken, channel, threadTS string) (*Message, error) {
	var out struct {
		Messages []Message `json:"messages"`
	}
	params := url.Values{"channel": {channel}, "ts": {threadTS}, "limit": {"1"}}
	if err := a.call(ctx, botToken, "conversations.replies", params, &out); err != nil {
		return nil, err
	}
	if len(out.Messages) == 0 || out.Messages[0].TS != threadTS {
		return nil, nil
	}
	return &out.Messages[0], nil
}

// errFileHost and errFileTooLarge refuse a file transfer.
var (
	errFileHost     = errors.New("slack file: not on Slack's file host")
	errFileTooLarge = errors.New("slack file: too large")
)

func (a *API) fileURLAllowed(u *url.URL) bool {
	if u == nil || (u.Scheme != "https" && !(a.allowHTTP && u.Scheme == "http")) {
		return false
	}
	return a.FileHosts[strings.ToLower(u.Hostname())]
}

// Download fetches a file's bytes (url_private_download) with the bot token, from Slack's
// file host only, reading at most limit bytes.
func (a *API) Download(ctx context.Context, botToken, rawURL string, limit int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !a.fileURLAllowed(u) {
		return nil, errFileHost
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+botToken)
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slack file: download returned %s", resp.Status)
	}
	// Slack answers an unauthorised file request with its HTML sign-in page, not a 401.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return nil, fmt.Errorf("slack file: download returned a web page (is files:read granted?)")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errFileTooLarge
	}
	return data, nil
}

// UploadFile shares a file in a channel (in a thread when threadTS is set), in Slack's three
// steps: files.getUploadURLExternal, a POST of the bytes to the URL it gives, and
// files.completeUploadExternal.
func (a *API) UploadFile(ctx context.Context, botToken, channel, threadTS, name string, data []byte) error {
	var ticket struct {
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	params := url.Values{"filename": {name}, "length": {strconv.Itoa(len(data))}}
	if err := a.call(ctx, botToken, "files.getUploadURLExternal", params, &ticket); err != nil {
		return err
	}
	u, err := url.Parse(ticket.UploadURL)
	if err != nil || !a.fileURLAllowed(u) {
		return errFileHost
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("slack file upload: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack file upload: %s", resp.Status)
	}
	files, _ := json.Marshal([]map[string]string{{"id": ticket.FileID, "title": name}})
	done := url.Values{"files": {string(files)}, "channel_id": {channel}}
	if threadTS != "" {
		done.Set("thread_ts", threadTS)
	}
	return a.call(ctx, botToken, "files.completeUploadExternal", done, nil)
}
