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

// Background work its role's choice keeps from running answers 409 with a code per cause and the
// role in details; anything else is left for the caller.
func TestRespondIfUtilityRoleUnavailable(t *testing.T) {
	for err, want := range map[error]string{
		fmt.Errorf("parse: %w", &models.UtilityRoleUnavailableError{Role: models.UtilityRoleSimple, Err: models.ErrUtilityRoleNotSet}):      models.ErrCodeUtilityModelNotSet,
		&models.UtilityRoleUnavailableError{Role: models.UtilityRoleSimple, Err: models.ErrUtilityRoleOff}:                                  models.ErrCodeUtilityRoleOff,
		&models.UtilityRoleUnavailableError{Role: models.UtilityRoleSimple, Err: fmt.Errorf("%w: gone", models.ErrUtilityModelUnavailable)}: models.ErrCodeUtilityModelUnavailable,
	} {
		rec := httptest.NewRecorder()
		require.True(t, RespondIfUtilityRoleUnavailable(rec, zap.NewNop(), err))
		require.Equal(t, http.StatusConflict, rec.Code)
		var body struct {
			models.ErrorResponse
			Details map[string]string `json:"details"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, want, body.Code)
		require.Equal(t, map[string]string{"role": "simple"}, body.Details)
	}

	rec := httptest.NewRecorder()
	require.False(t, RespondIfUtilityRoleUnavailable(rec, zap.NewNop(), errors.New("db down")))
	require.Zero(t, rec.Body.Len())
}
