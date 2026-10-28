package handlerutils

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

func TestRespondIfProviderNotConfigured(t *testing.T) {
	wrapped := fmt.Errorf("create embeddings: %w", &url.Error{
		Op: "Post", URL: "https://api.openai.com/v1/embeddings",
		Err: &providerkeys.NotConfiguredError{Provider: models.ModelProviderOpenAI},
	})

	rec := httptest.NewRecorder()
	require.True(t, RespondIfProviderNotConfigured(rec, zap.NewNop(), wrapped))
	require.Equal(t, http.StatusConflict, rec.Code)

	var body struct {
		models.ErrorResponse
		Details map[string]string `json:"details"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, models.ErrCodeProviderNotConfigured, body.Code)
	require.Equal(t, "No OpenAI API key is set.", body.Message)
	require.Equal(t, map[string]string{"provider": "openai"}, body.Details)
	require.NotContains(t, rec.Body.String(), "api.openai.com", "no transport detail reaches the client")
}

// Any other error is left for the caller to answer.
func TestRespondIfProviderNotConfigured_OtherErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	require.False(t, RespondIfProviderNotConfigured(rec, zap.NewNop(), errors.New("db down")))
	require.Zero(t, rec.Body.Len())
}
