package agent

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

func TestFailureCode(t *testing.T) {
	missingKey := fmt.Errorf("OpenAI API call failed: %w", &url.Error{
		Op: "Post", URL: "https://api.openai.com/v1/responses",
		Err: &providerkeys.NotConfiguredError{Provider: models.ModelProviderOpenAI},
	})
	require.Equal(t, models.ErrCodeProviderNotConfigured, failureCode(missingKey))
	require.Equal(t, models.ErrCodeQuotaExceeded, failureCode(fmt.Errorf("%w for user x", ErrQuotaExceeded)))
	require.Empty(t, failureCode(errors.New("model overloaded")), "a failure without a stable code records none")
}
