package accountexport

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Import embeddings must go through the HTTP client the server passes in (the shared,
// instrumented provider client, or the deny-network client under a non-vendor backend),
// not the SDK's default client.
func TestNewHandlerUsesProvidedHTTPClient(t *testing.T) {
	var hits atomic.Int64
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		assert.Equal(t, "/v1/embeddings", r.URL.Path)
		body := `{"object":"list","data":[` +
			`{"object":"embedding","index":0,"embedding":[0.1,0.2]},` +
			`{"object":"embedding","index":1,"embedding":[0.3,0.4]}],` +
			`"model":"text-embedding-3-small","usage":{"prompt_tokens":2,"total_tokens":2}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	h := NewHandler(nil, zap.NewNop(), nil, nil, "test-key", httpClient)
	require.NotNil(t, h.oaiClient)

	got, err := h.createEmbeddings(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{0.1, 0.2}, {0.3, 0.4}}, got)
	assert.Equal(t, int64(1), hits.Load(), "embedding request must use the provided HTTP client")
}

func TestNewHandlerWithoutKeyDisablesMemoryImport(t *testing.T) {
	h := NewHandler(nil, zap.NewNop(), nil, nil, "", &http.Client{})
	assert.Nil(t, h.oaiClient)
	_, err := h.createEmbeddings(context.Background(), []string{"a"})
	assert.ErrorIs(t, err, errMemoryImportUnavailable)
}
