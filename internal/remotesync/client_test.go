package remotesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in, want, err string
		insecure      bool
	}{
		{in: "https://api.whatiff.chat", want: "https://api.whatiff.chat"},
		{in: "api.whatiff.chat/", want: "https://api.whatiff.chat"},
		{in: "https://api.whatiff.chat/api/", want: "https://api.whatiff.chat"},
		{in: "https://whatiff.chat", want: DefaultBaseURL},
		{in: "https://app.whatiff.chat/chat/123", want: DefaultBaseURL},
		{in: "https://example.test/whatiff/api", want: "https://example.test/whatiff"},
		{in: "http://localhost:8080", err: "must use https"},
		{in: "http://localhost:8080/api", insecure: true, want: "http://localhost:8080"},
		{in: "ftp://example.test", err: "must use https"},
		{in: "https://user:pw@example.test", err: "just the server"},
		{in: "https://example.test/?x=1", err: "just the server"},
		{in: "   ", err: "enter the remote server"},
	}
	for _, c := range cases {
		got, err := NormalizeBaseURL(c.in, c.insecure)
		if c.err != "" {
			require.Error(t, err, c.in)
			assert.Contains(t, err.Error(), c.err, c.in)
			continue
		}
		require.NoError(t, err, c.in)
		assert.Equal(t, c.want, got, c.in)
	}
}

func TestClientSendsTheTokenAndReadsPages(t *testing.T) {
	chatID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer wht_abc", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/webhooks/chat":
			assert.Equal(t, "2", r.URL.Query().Get("page"))
			assert.Equal(t, "100", r.URL.Query().Get("limit"))
			assert.Equal(t, "true", r.URL.Query().Get("archived"))
			_, _ = w.Write([]byte(`{"results":[{"id":"` + chatID.String() + `","name":"Trip","personality_name":"Vix","last_message_time":"2026-10-01T12:00:00Z"}],"total_count":1,"page":2}`))
		case "/api/webhooks/chat/" + chatID.String() + "/messages":
			assert.Equal(t, "abc", r.URL.Query().Get("cursor"))
			_, _ = w.Write([]byte(`{"results":[{"id":"` + uuid.NewString() + `","chat_id":"` + chatID.String() + `","message":"hi","origin":"User","sent_at":"2026-10-01T12:00:00Z"}],"next_cursor":"def"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "wht_abc", NewHTTPClient(true))
	require.NoError(t, err)
	chats, err := c.ListChats(context.Background(), 2, true)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	assert.Equal(t, "Vix", chats[0].PersonalityName)
	require.NotNil(t, chats[0].Archived)
	assert.True(t, *chats[0].Archived, "archived filled in from the list asked for")

	msgs, next, err := c.ListMessages(context.Background(), chatID, "abc")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "hi", msgs[0].Message)
	assert.Equal(t, "def", next)
}

func TestClientClassifiesFailures(t *testing.T) {
	status := 0
	body := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusFound {
			http.Redirect(w, r, "https://elsewhere.test/steal", http.StatusFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL, "wht_abc", NewHTTPClient(true))
	require.NoError(t, err)

	for _, tc := range []struct {
		status int
		body   string
		kind   ErrorKind
	}{
		{http.StatusUnauthorized, "", KindUnauthorized},
		{http.StatusForbidden, "", KindForbidden},
		{http.StatusNotFound, "", KindNotFound},
		{http.StatusTooManyRequests, "", KindRateLimited},
		{http.StatusBadGateway, "", KindServer},
		{http.StatusFound, "", KindBadResponse},
		{http.StatusOK, "<html>login</html>", KindBadResponse},
	} {
		status, body = tc.status, tc.body
		err := c.Check(context.Background())
		assert.Equal(t, tc.kind, KindOf(err), "HTTP %d", tc.status)
		assert.NotEmpty(t, UserMessage(err))
	}
	assert.True(t, NeedsOwner(&Error{Kind: KindForbidden}))
	assert.False(t, NeedsOwner(&Error{Kind: KindServer}))

	srv.Close()
	assert.Equal(t, KindUnreachable, KindOf(c.Check(context.Background())))
}

func TestTheDefaultClientRefusesLocalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "wht_abc", nil)
	require.NoError(t, err)
	assert.Equal(t, KindUnreachable, KindOf(c.Check(context.Background())), "loopback is not public")

	c, err = NewClient(srv.URL, "wht_abc", NewHTTPClient(true))
	require.NoError(t, err)
	assert.NoError(t, c.Check(context.Background()))
}
