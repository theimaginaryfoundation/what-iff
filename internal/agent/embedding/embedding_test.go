package embedding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(handler http.HandlerFunc) *openai.Client {
	server := httptest.NewServer(handler)
	client := openai.NewClient(
		option.WithBaseURL(server.URL),
		option.WithAPIKey("test-key"),
	)
	return &client
}

func TestCreateEmbedding(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		handler        http.HandlerFunc
		expectErr      bool
		errContains    string
		validateResult func(t *testing.T, result []float32)
	}{
		{
			name:  "successful embedding creation",
			input: "hello world",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"object": "list",
					"data": [
						{
							"object": "embedding",
							"index": 0,
							"embedding": [0.1, 0.2, 0.3, 0.4, 0.5]
						}
					],
					"model": "text-embedding-3-small",
					"usage": {
						"prompt_tokens": 2,
						"total_tokens": 2
					}
				}`))
			}),
			expectErr: false,
			validateResult: func(t *testing.T, result []float32) {
				require.Len(t, result, 5)
				assert.InDelta(t, 0.1, float64(result[0]), 0.001)
				assert.InDelta(t, 0.2, float64(result[1]), 0.001)
				assert.InDelta(t, 0.3, float64(result[2]), 0.001)
				assert.InDelta(t, 0.4, float64(result[3]), 0.001)
				assert.InDelta(t, 0.5, float64(result[4]), 0.001)
			},
		},
		{
			name:  "empty data array returns error",
			input: "hello world",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"object": "list",
					"data": [],
					"model": "text-embedding-3-small",
					"usage": {
						"prompt_tokens": 0,
						"total_tokens": 0
					}
				}`))
			}),
			expectErr:   true,
			errContains: "empty embedding response",
		},
		{
			name:  "API error returns error",
			input: "hello world",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{
					"error": {
						"message": "Internal server error",
						"type": "server_error",
						"code": "internal_error"
					}
				}`))
			}),
			expectErr:   true,
			errContains: "failed to generate embedding",
		},
		{
			name:  "empty input still calls API",
			input: "",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"object": "list",
					"data": [
						{
							"object": "embedding",
							"index": 0,
							"embedding": [0.0, 0.0, 0.0]
						}
					],
					"model": "text-embedding-3-small",
					"usage": {
						"prompt_tokens": 0,
						"total_tokens": 0
					}
				}`))
			}),
			expectErr: false,
			validateResult: func(t *testing.T, result []float32) {
				require.Len(t, result, 3)
				for _, v := range result {
					assert.InDelta(t, 0.0, float64(v), 0.001)
				}
			},
		},
		{
			name:  "rate limit error returns error",
			input: "hello world",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{
					"error": {
						"message": "Rate limit exceeded",
						"type": "rate_limit_error",
						"code": "rate_limit_exceeded"
					}
				}`))
			}),
			expectErr:   true,
			errContains: "failed to generate embedding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(tt.handler)

			result, err := CreateEmbedding(context.Background(), client, tt.input)

			if tt.expectErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				if tt.validateResult != nil {
					tt.validateResult(t, result)
				}
			}
		})
	}
}
