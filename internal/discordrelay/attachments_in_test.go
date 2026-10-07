package discordrelay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
)

type fakeIngester struct {
	mu       sync.Mutex
	uploads  []plugins.AttachmentUpload
	rejectBy map[string]error // by file name
}

func (f *fakeIngester) IngestAttachment(_ context.Context, up plugins.AttachmentUpload) (*models.FileAttachment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.rejectBy[up.Name]; err != nil {
		return nil, err
	}
	f.uploads = append(f.uploads, up)
	return &models.FileAttachment{ID: uuid.New(), Name: up.Name, FileType: "image/png"}, nil
}

type fakeFetcher struct {
	files map[string][]byte // by url
	errs  map[string]error
	max   int64
}

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string, max int64) ([]byte, error) {
	f.max = max
	if err := f.errs[rawURL]; err != nil {
		return nil, err
	}
	return f.files[rawURL], nil
}

func (h *harness) withFiles() (*fakeIngester, *fakeFetcher) {
	ing := &fakeIngester{rejectBy: map[string]error{}}
	fet := &fakeFetcher{files: map[string][]byte{}, errs: map[string]error{}}
	h.svc.Ingest, h.svc.Fetch = ing, fet
	return ing, fet
}

func (h *harness) tagWithFiles(id string, files ...Attachment) InboundMessage {
	m := h.tag(id)
	m.Attachments = files
	return m
}

func (h *harness) startedMessage(t *testing.T) models.ChatMessage {
	t.Helper()
	require.Eventually(t, func() bool { return len(h.turns.started()) == 1 }, time.Second, 5*time.Millisecond)
	return h.turns.started()[0].Message
}

func TestAPictureWithATagIsSavedToTheRelayThreadAndSentWithTheTurn(t *testing.T) {
	h := newHarness(t)
	ing, fet := h.withFiles()
	fet.files["https://cdn.discordapp.com/a/cat.png"] = []byte("png-bytes")

	h.svc.HandleInbound(h.ctx, h.target.Bot.ID, testBotUser,
		h.tagWithFiles("m1", Attachment{Filename: "cat.png", URL: "https://cdn.discordapp.com/a/cat.png", Size: 9}))

	msg := h.startedMessage(t)
	require.Len(t, ing.uploads, 1)
	assert.Equal(t, plugins.AttachmentUpload{UserID: h.target.Bot.OwnerID, ChatID: h.target.Binding.ChatID, Name: "cat.png", Data: []byte("png-bytes")}, ing.uploads[0])
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "cat.png", msg.Attachments[0].Name)
	assert.Equal(t, "alice (Discord, #general): hello", msg.Message, "nothing to explain when every file was added")
	assert.Equal(t, int64(MaxInboundFileBytes), fet.max)
}

func TestFilesThatCannotBeAddedAreNamedInThePromptNotDropped(t *testing.T) {
	h := newHarness(t)
	ing, fet := h.withFiles()
	ing.rejectBy["run.exe"] = fmt.Errorf("%w: exe", plugins.ErrAttachmentUnsupported)
	ing.rejectBy["huge.pdf"] = plugins.ErrAttachmentTooLarge
	ing.rejectBy["odd.png"] = errors.New("s3 down")
	fet.errs["https://cdn.discordapp.com/gone"] = errors.New("404")
	for _, n := range []string{"run.exe", "huge.pdf", "odd.png", "ok.png"} {
		fet.files["https://cdn.discordapp.com/"+n] = []byte("x")
	}

	h.svc.HandleInbound(h.ctx, h.target.Bot.ID, testBotUser, h.tagWithFiles("m1",
		Attachment{Filename: "run.exe", URL: "https://cdn.discordapp.com/run.exe"},
		Attachment{Filename: "huge.pdf", URL: "https://cdn.discordapp.com/huge.pdf"},
		Attachment{Filename: "odd.png", URL: "https://cdn.discordapp.com/odd.png"},
		Attachment{Filename: "gone.png", URL: "https://cdn.discordapp.com/gone"},
		Attachment{Filename: "big.zip", URL: "https://cdn.discordapp.com/big.zip", Size: MaxInboundFileBytes + 1},
		Attachment{Filename: "ok.png", URL: "https://cdn.discordapp.com/ok.png"},
	))

	msg := h.startedMessage(t)
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "ok.png", msg.Attachments[0].Name)
	for _, want := range []string{
		`[File "run.exe" was not added: this file type is not supported]`,
		`[File "huge.pdf" was not added: too large]`,
		`[File "odd.png" was not added: it could not be saved]`,
		`[File "gone.png" was not added: it could not be downloaded]`,
		`[File "big.zip" was not added: too large]`,
	} {
		assert.Contains(t, msg.Message, want)
	}
}

func TestOnlyFourFilesPerMessageAreAdded(t *testing.T) {
	h := newHarness(t)
	ing, fet := h.withFiles()
	var files []Attachment
	for i := 0; i < MaxInboundAttachments+2; i++ {
		u := fmt.Sprintf("https://cdn.discordapp.com/%d.png", i)
		fet.files[u] = []byte("x")
		files = append(files, Attachment{Filename: fmt.Sprintf("%d.png", i), URL: u})
	}

	h.svc.HandleInbound(h.ctx, h.target.Bot.ID, testBotUser, h.tagWithFiles("m1", files...))

	msg := h.startedMessage(t)
	assert.Len(t, ing.uploads, MaxInboundAttachments)
	assert.Len(t, msg.Attachments, MaxInboundAttachments)
	assert.Equal(t, 2, strings.Count(msg.Message, "at most 4 files"))
}

func TestWithoutAnIngesterFilesAreMentionedButTheTagStillGetsAnAnswer(t *testing.T) {
	h := newHarness(t)

	h.svc.HandleInbound(h.ctx, h.target.Bot.ID, testBotUser,
		h.tagWithFiles("m1", Attachment{Filename: "cat.png", URL: "https://cdn.discordapp.com/cat.png"}))

	msg := h.startedMessage(t)
	assert.Empty(t, msg.Attachments)
	assert.Contains(t, msg.Message, "1 file(s) posted with this message could not be added here")
}

func TestAMessageWithoutFilesIsUnchanged(t *testing.T) {
	h := newHarness(t)
	h.withFiles()

	h.svc.HandleInbound(h.ctx, h.target.Bot.ID, testBotUser, h.tag("m1"))

	msg := h.startedMessage(t)
	assert.Empty(t, msg.Attachments)
	assert.Equal(t, "alice (Discord, #general): hello", msg.Message)
}

func TestTheCDNFetcherOnlyFetchesFromDiscordOverHTTPS(t *testing.T) {
	f := NewCDNFetcher()
	for _, bad := range []string{
		"http://cdn.discordapp.com/a.png",
		"https://example.com/a.png",
		"https://cdn.discordapp.com.evil.test/a.png",
		"https://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
		"::not a url",
	} {
		_, err := f.Fetch(context.Background(), bad, 1024)
		assert.ErrorContains(t, err, "refusing to fetch", bad)
	}
	assert.True(t, allowedAttachmentURL(mustURL(t, "https://cdn.discordapp.com/a.png"), f.Hosts))
	assert.True(t, allowedAttachmentURL(mustURL(t, "https://media.discordapp.net/a.png"), f.Hosts))
}

func TestTheCDNFetcherReadsAtMostTheCapAndRejectsErrorStatuses(t *testing.T) {
	var status = http.StatusOK
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	// The default allowlist is the real Discord hosts, so this fetcher allows the test
	// server's host (https, which httptest.NewTLSServer provides) instead.
	f := &CDNFetcher{Client: srv.Client(), Hosts: map[string]bool{"127.0.0.1": true}}

	data, err := f.Fetch(context.Background(), srv.URL, 100)
	require.NoError(t, err)
	assert.Len(t, data, 100)

	_, err = f.Fetch(context.Background(), srv.URL, 99)
	assert.ErrorIs(t, err, errFetchTooLarge)

	status = http.StatusForbidden
	_, err = f.Fetch(context.Background(), srv.URL, 1000)
	assert.ErrorContains(t, err, "403")
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestTheCDNFetcherRefusesToConnectToNonPublicAddresses(t *testing.T) {
	for _, ip := range []string{
		"127.0.0.1", "10.0.0.5", "172.16.3.4", "192.168.1.1", "169.254.169.254", // loopback, private, cloud metadata
		"100.64.0.1", "0.0.0.0", "224.0.0.1", "::1", "fe80::1", "fc00::1",
	} {
		assert.False(t, isPublicIP(net.ParseIP(ip)), ip)
		assert.Error(t, publicOnlyControl("tcp", net.JoinHostPort(ip, "443"), nil), ip)
	}
	for _, ip := range []string{"8.8.8.8", "162.159.135.233", "2606:4700::6810:84e5"} {
		assert.True(t, isPublicIP(net.ParseIP(ip)), ip)
		assert.NoError(t, publicOnlyControl("tcp", net.JoinHostPort(ip, "443"), nil), ip)
	}
}

func TestTheRealFetcherCannotReachALocalServerEvenOnAnAllowedHost(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("secret")) }))
	defer srv.Close()

	// The allowlist lets the host through, but the fetcher NewCDNFetcher builds refuses the
	// loopback connection.
	f := NewCDNFetcher()
	f.Hosts = map[string]bool{"127.0.0.1": true}
	_, err := f.Fetch(context.Background(), srv.URL, 1024)
	assert.ErrorContains(t, err, "non-public address")
}
