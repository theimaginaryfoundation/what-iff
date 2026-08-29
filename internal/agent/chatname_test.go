package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- mockChatName ---

func TestMockChatName(t *testing.T) {
	t.Parallel()
	longMsg := strings.Repeat("a", 50)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty string", "", "Mock Chat"},
		{"whitespace only collapses to empty", "   \t\n  ", "Mock Chat"},
		{"short message returned as-is", "hello   there  friend", "hello there friend"},
		{"long message truncated with ellipsis", longMsg, longMsg[:40] + "…"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, mockChatName(tc.in))
		})
	}
}

// --- generateChatName ---

func TestGenerateChatName_MockModeReturnsDeterministicName(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop(), mockLLM: true}
	got, err := a.generateChatName(context.Background(), "hello world")
	require.NoError(t, err)
	require.Equal(t, "hello world", got)
}

func TestGenerateChatName_LocalModeReturnsDeterministicName(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop(), localLLM: true}
	got, err := a.generateChatName(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "Mock Chat", got)
}

func TestGenerateChatName_ProviderErrorIsReturned(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	_, err := a.generateChatName(context.Background(), "hi")
	require.Error(t, err)
}

func TestGenerateChatName_SuccessReturnsOutputText(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", "My New Chat"))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	got, err := a.generateChatName(context.Background(), "hi")
	require.NoError(t, err)
	require.Equal(t, "My New Chat", got)
}
