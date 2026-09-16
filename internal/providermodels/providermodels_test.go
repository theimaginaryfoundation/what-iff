package providermodels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := map[string]Kind{
		"gpt-5.6-luna":           KindChat,
		"gpt-4o-2024-08-06":      KindSnapshot,
		"text-embedding-3-small": KindEmbedding,
		"tts-1":                  KindAudio,
		"gpt-4o-mini-tts":        KindAudio,
		"dall-e-3":               KindImage,
		"omni-moderation-latest": KindModeration,
		"gpt-5.1-codex":          KindCode,
		"davinci-002":            KindLegacy,
	}
	for id, want := range tests {
		require.Equalf(t, want, Classify(id), "classifying %q", id)
	}
}

// Providers keep returning models past their shutdown date, so coming back from
// the API is not evidence a model still works.
func TestRetiredAsOf(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	require.True(t, retiredAsOf("2026-07-23", now), "a past shutdown date is retired")
	require.False(t, retiredAsOf("2026-10-23", now), "a future shutdown date is still usable")
	require.False(t, retiredAsOf("", now), "no shutdown date means no retirement")
	require.False(t, retiredAsOf("not-a-date", now), "an unparseable date must not retire a working model")
}

type staticKeys struct{ key string }

func (s staticKeys) KeyFor(context.Context, string) string { return s.key }

func TestOpenAILister_ParsesAndClassifies(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[
			{"id":"gpt-5.6-luna","shutdown_date":null},
			{"id":"gpt-4.1-nano","shutdown_date":"2026-10-23"},
			{"id":"gpt-5-chat-latest","shutdown_date":"2026-07-23"},
			{"id":"text-embedding-3-small","shutdown_date":null}
		]}`))
	}))
	defer srv.Close()

	lister := &OpenAILister{
		BaseURL: srv.URL,
		Now:     func() time.Time { return time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC) },
	}
	got, err := lister.List(context.Background(), "sk-test")
	require.NoError(t, err)
	require.Len(t, got, 4)

	byID := map[string]Model{}
	for _, m := range got {
		byID[m.ID] = m
	}
	require.False(t, byID["gpt-5.6-luna"].Retired)
	require.Equal(t, "2026-10-23", byID["gpt-4.1-nano"].RetiresOn)
	require.False(t, byID["gpt-4.1-nano"].Retired, "retiring next month is not retired today")
	require.True(t, byID["gpt-5-chat-latest"].Retired, "past its shutdown date")
	require.Equal(t, KindEmbedding, byID["text-embedding-3-small"].Kind)
}

// An error body can echo the caller's key back, so it must never reach the
// message a user or a log line sees.
func TestOpenAILister_ErrorOmitsBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: sk-secret123"}}`))
	}))
	defer srv.Close()

	_, err := (&OpenAILister{BaseURL: srv.URL}).List(context.Background(), "sk-secret123")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sk-secret123")
	require.Contains(t, err.Error(), "401")
}

func TestOpenAILister_RequiresKey(t *testing.T) {
	t.Parallel()

	_, err := (&OpenAILister{}).List(context.Background(), "  ")
	require.ErrorContains(t, err, "no API key")
}
