package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDenyNetworkHTTPClient_BlocksPlainRequests(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer server.Close()

	client := DenyNetworkHTTPClient()
	resp, err := client.Get(server.URL)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNetworkDenied)
	assert.Nil(t, resp)
	assert.Equal(t, int64(0), hits.Load(), "server must never be reached")
}

func TestDenyNetworkHTTPClient_BlocksOpenAIClientCall(t *testing.T) {
	// Even with a reachable base URL and a "valid" key, an SDK client built on
	// the deny client must fail before the request leaves the process.
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := openai.NewClient(
		option.WithAPIKey("dummy-key"),
		option.WithBaseURL(server.URL),
		option.WithHTTPClient(DenyNetworkHTTPClient()),
		option.WithMaxRetries(0),
	)
	_, err := client.Embeddings.New(context.Background(), openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String("hello")},
		Model: openai.EmbeddingModelTextEmbedding3Small,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNetworkDenied)
	assert.Equal(t, int64(0), hits.Load(), "httptest server must never be hit through the deny transport")
}
