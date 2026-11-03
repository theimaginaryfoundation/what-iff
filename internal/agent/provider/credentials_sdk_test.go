package provider

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// The typed error has to survive each SDK's own error wrapping, or callers
// would see a generic transport failure. Both SDKs every provider client is
// built on are checked, through a real call.
//
// Both SDKs treat any transport error as a dropped connection and retry it, so
// a keyless request is attempted three times (about a second of backoff)
// before the error comes back. A RoundTripper cannot return a response and an
// error together, so there is no way to mark it non-retryable without losing
// the type; resolves pins the count so a change in SDK behaviour is noticed.
func TestNotConfiguredSurvivesTheSDKs(t *testing.T) {
	var resolves atomic.Int32
	none := func(context.Context) string { resolves.Add(1); return "" }
	cap := &capturingTransport{}
	client := CredentialHTTPClient([]CredentialRule{
		{Provider: models.ModelProviderOpenAI, BaseURL: DefaultOpenAIBaseURL, Style: BearerAuthorization, Resolve: none},
		{Provider: models.ModelProviderAnthropic, BaseURL: DefaultAnthropicBaseURL, Style: AnthropicAPIKey, Resolve: none},
	}, &http.Client{Transport: cap})

	t.Run("openai-go", func(t *testing.T) {
		resolves.Store(0)
		oai := openai.NewClient(option.WithHTTPClient(client), option.WithAPIKey("from-env"))
		_, err := oai.Embeddings.New(context.Background(), openai.EmbeddingNewParams{
			Model: openai.EmbeddingModelTextEmbedding3Small,
			Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String("hello")},
		})
		var notConfigured *providerkeys.NotConfiguredError
		require.ErrorAs(t, err, &notConfigured)
		require.Equal(t, models.ModelProviderOpenAI, notConfigured.Provider)
		require.EqualValues(t, 3, resolves.Load(), "attempts, including the SDK's retries")

		// The SDK hands it back inside a *url.Error, which is a net.Error. It
		// must still classify as an authentication failure, or the app's own
		// retry would wait out its network-error schedule before failing.
		require.Equal(t, telemetry.ErrorTypeAuth, telemetry.ClassifyError(err))
		_, retry := retryReason(err)
		require.False(t, retry, "a missing key is not retried")
	})

	t.Run("anthropic-sdk-go", func(t *testing.T) {
		resolves.Store(0)
		ant := anthropic.NewClient(anthropicoption.WithHTTPClient(client), anthropicoption.WithAPIKey("from-env"))
		_, err := ant.Messages.New(context.Background(), anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeHaiku4_5,
			MaxTokens: 16,
			Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hello"))},
		})
		var notConfigured *providerkeys.NotConfiguredError
		require.ErrorAs(t, err, &notConfigured)
		require.Equal(t, models.ModelProviderAnthropic, notConfigured.Provider)
		require.EqualValues(t, 3, resolves.Load(), "attempts, including the SDK's retries")
	})

	require.Nil(t, cap.last, "nothing reached a provider")
}
