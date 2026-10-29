package slackrelay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWebAPI is a Slack Web API on an httptest server: each method answers from a handler, and
// every request is recorded.
type fakeWebAPI struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	calls   []recordedCall
	methods map[string]func(form url.Values) (int, any)
	// files are served at /files/<name>; uploads land at /upload.
	files    map[string]string
	uploaded []byte
}

type recordedCall struct {
	Method string
	Auth   string
	Form   url.Values
}

func newFakeWebAPI(t *testing.T) *fakeWebAPI {
	f := &fakeWebAPI{t: t, methods: map[string]func(url.Values) (int, any){}, files: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWebAPI) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/files/"):
		f.record("GET "+r.URL.Path, r.Header.Get("Authorization"), nil)
		body, ok := f.files[strings.TrimPrefix(r.URL.Path, "/files/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
		return
	case r.URL.Path == "/upload":
		require.NoError(f.t, r.ParseMultipartForm(1<<20))
		file, _, err := r.FormFile("file")
		require.NoError(f.t, err)
		data, _ := io.ReadAll(file)
		f.mu.Lock()
		f.uploaded = data
		f.mu.Unlock()
		f.record("POST /upload", r.Header.Get("Authorization"), nil)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/api/")
	require.NoError(f.t, r.ParseForm())
	f.record(method, r.Header.Get("Authorization"), r.PostForm)
	h, ok := f.methods[method]
	if !ok {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "unknown_method"})
		return
	}
	status, body := h(r.PostForm)
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "7")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeWebAPI) record(method, auth string, form url.Values) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recordedCall{Method: method, Auth: auth, Form: form})
}

func (f *fakeWebAPI) api() *API {
	host := strings.Split(strings.TrimPrefix(f.srv.URL, "http://"), ":")[0]
	return &API{Base: f.srv.URL + "/api/", HTTP: f.srv.Client(), FileHosts: map[string]bool{host: true}, allowHTTP: true}
}

func ok(fields map[string]any) (int, any) {
	out := map[string]any{"ok": true}
	for k, v := range fields {
		out[k] = v
	}
	return http.StatusOK, out
}

func TestAPIAuthAndErrors(t *testing.T) {
	f := newFakeWebAPI(t)
	f.methods["auth.test"] = func(url.Values) (int, any) {
		return ok(map[string]any{"team_id": "T1", "team": "Acme", "url": "https://acme.slack.com/", "user_id": "UVIX", "bot_id": "BVIX"})
	}
	id, err := f.api().AuthTest(context.Background(), "xoxb-1")
	require.NoError(t, err)
	assert.Equal(t, Identity{TeamID: "T1", Team: "Acme", URL: "https://acme.slack.com/", UserID: "UVIX", BotID: "BVIX"}, *id)
	assert.Equal(t, "Bearer xoxb-1", f.calls[0].Auth, "the token goes in the header, not the form")
	assert.Empty(t, f.calls[0].Form.Get("token"))

	f.methods["auth.test"] = func(url.Values) (int, any) { return ok(map[string]any{"team_id": "T1", "user_id": "UME"}) }
	_, err = f.api().AuthTest(context.Background(), "xoxp-user")
	assert.ErrorContains(t, err, "not_a_bot_token", "a user token is refused")

	f.methods["auth.test"] = func(url.Values) (int, any) {
		return http.StatusOK, map[string]any{"ok": false, "error": "invalid_auth"}
	}
	_, err = f.api().AuthTest(context.Background(), "xoxb-bad")
	assert.True(t, IsAuthError(err))
	assert.False(t, IsTransient(err))

	f.methods["chat.postMessage"] = func(url.Values) (int, any) { return http.StatusTooManyRequests, map[string]any{"ok": false} }
	_, err = f.api().PostMessage(context.Background(), "xoxb-1", "C1", "", "hi")
	var slackErr *Error
	require.ErrorAs(t, err, &slackErr)
	assert.Equal(t, 7*time.Second, slackErr.RetryAfter)
	assert.True(t, IsTransient(err))

	f.methods["chat.postMessage"] = func(url.Values) (int, any) {
		return http.StatusOK, map[string]any{"ok": false, "error": "not_in_channel"}
	}
	_, err = f.api().PostMessage(context.Background(), "xoxb-1", "C1", "", "hi")
	assert.True(t, IsChannelError(err))
}

func TestAPIPostMessage(t *testing.T) {
	f := newFakeWebAPI(t)
	f.methods["chat.postMessage"] = func(url.Values) (int, any) { return ok(map[string]any{"ts": "123.456"}) }
	ts, err := f.api().PostMessage(context.Background(), "xoxb-1", "C1", "100.1", "hello &lt;b&gt;")
	require.NoError(t, err)
	assert.Equal(t, "123.456", ts)
	form := f.calls[0].Form
	assert.Equal(t, "C1", form.Get("channel"))
	assert.Equal(t, "100.1", form.Get("thread_ts"))
	assert.Equal(t, "hello &lt;b&gt;", form.Get("text"))
	assert.Equal(t, "false", form.Get("link_names"), "mentions are never parsed from the text")
}

func TestAPIChannelsPages(t *testing.T) {
	f := newFakeWebAPI(t)
	f.methods["conversations.list"] = func(form url.Values) (int, any) {
		if form.Get("cursor") == "" {
			return ok(map[string]any{"channels": []Channel{{ID: "C1", Name: "general", IsMember: true}},
				"response_metadata": map[string]any{"next_cursor": "page2"}})
		}
		return ok(map[string]any{"channels": []Channel{{ID: "G1", Name: "secret", IsPrivate: true, IsMember: true}}})
	}
	chans, err := f.api().Channels(context.Background(), "xoxb-1")
	require.NoError(t, err)
	assert.Equal(t, []Channel{{ID: "C1", Name: "general", IsMember: true}, {ID: "G1", Name: "secret", IsPrivate: true, IsMember: true}}, chans)
	assert.Equal(t, "public_channel,private_channel", f.calls[0].Form.Get("types"))
}

func TestAPIDownloadSendsTheTokenOnlyToSlacksFileHost(t *testing.T) {
	f := newFakeWebAPI(t)
	f.files["a.png"] = "png-bytes"
	api := f.api()

	data, err := api.Download(context.Background(), "xoxb-1", f.srv.URL+"/files/a.png", 100)
	require.NoError(t, err)
	assert.Equal(t, "png-bytes", string(data))
	assert.Equal(t, "Bearer xoxb-1", f.calls[0].Auth)

	_, err = api.Download(context.Background(), "xoxb-1", f.srv.URL+"/files/a.png", 3)
	assert.ErrorIs(t, err, errFileTooLarge)

	for _, bad := range []string{"https://evil.example/files/a.png", "file:///etc/passwd", "not a url"} {
		_, err = api.Download(context.Background(), "xoxb-1", bad, 100)
		assert.ErrorIs(t, err, errFileHost, bad)
	}
	assert.Len(t, f.calls, 2, "nothing was requested from another host")

	prod := NewAPI()
	assert.False(t, prod.fileURLAllowed(mustURL(t, "http://files.slack.com/x")), "plain http is refused")
	assert.True(t, prod.fileURLAllowed(mustURL(t, "https://files.slack.com/files-pri/T1-F1/a.png")))
	assert.False(t, prod.fileURLAllowed(mustURL(t, "https://files.slack.com.evil.example/x")))
}

func TestAPIUploadFileInThreeSteps(t *testing.T) {
	f := newFakeWebAPI(t)
	f.methods["files.getUploadURLExternal"] = func(form url.Values) (int, any) {
		assert.Equal(t, "chart.png", form.Get("filename"))
		assert.Equal(t, "3", form.Get("length"))
		return ok(map[string]any{"upload_url": f.srv.URL + "/upload", "file_id": "F1"})
	}
	f.methods["files.completeUploadExternal"] = func(form url.Values) (int, any) { return ok(nil) }
	require.NoError(t, f.api().UploadFile(context.Background(), "xoxb-1", "C1", "100.1", "chart.png", []byte("png")))
	assert.Equal(t, "png", string(f.uploaded))
	last := f.calls[len(f.calls)-1]
	assert.Equal(t, "files.completeUploadExternal", last.Method)
	assert.JSONEq(t, `[{"id":"F1","title":"chart.png"}]`, last.Form.Get("files"))
	assert.Equal(t, "C1", last.Form.Get("channel_id"))
	assert.Equal(t, "100.1", last.Form.Get("thread_ts"))

	f.methods["files.getUploadURLExternal"] = func(url.Values) (int, any) {
		return ok(map[string]any{"upload_url": "https://evil.example/upload", "file_id": "F2"})
	}
	assert.ErrorIs(t, f.api().UploadFile(context.Background(), "xoxb-1", "C1", "", "x.png", []byte("x")), errFileHost)
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}
