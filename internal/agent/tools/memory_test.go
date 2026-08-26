package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
)

// CreateEmbedding is a thin wrapper over embedding.CreateEmbedding. The
// cheapest reachable branch is the underlying API-error path: point the
// client at a server that always answers 400 so the call fails fast with no
// real network egress.
func TestCreateEmbedding_APIErrorIsWrapped(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := openai.NewClient(
		option.WithBaseURL(server.URL),
		option.WithAPIKey("test-key"),
		option.WithMaxRetries(0),
	)
	tool := &VectorStoreMemoryTool{oaiClient: &client}

	_, err := tool.CreateEmbedding(context.Background(), "hello")
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to generate embedding")
}
