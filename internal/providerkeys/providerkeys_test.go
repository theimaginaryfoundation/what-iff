package providerkeys

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestDeploymentResolver_AnswersFromConfiguration(t *testing.T) {
	t.Parallel()
	r := NewDeploymentResolver(map[models.ModelProvider]string{
		models.ModelProviderOpenAI:    "sk-openai",
		models.ModelProviderAnthropic: "sk-anthropic",
	})

	require.Equal(t, "sk-openai", r.Key(context.Background(), models.ModelProviderOpenAI))
	require.Equal(t, "sk-anthropic", r.Key(context.Background(), models.ModelProviderAnthropic))
	require.True(t, r.Configured(models.ModelProviderOpenAI))
}

// A provider set to the empty string must be indistinguishable from one never
// mentioned, so an operator blanking a variable turns the provider off rather
// than leaving a client that sends an empty credential.
func TestDeploymentResolver_BlankIsUnconfigured(t *testing.T) {
	t.Parallel()
	r := NewDeploymentResolver(map[models.ModelProvider]string{
		models.ModelProviderOpenAI: "   ",
		models.ModelProviderQwen:   "",
	})

	for _, p := range []models.ModelProvider{models.ModelProviderOpenAI, models.ModelProviderQwen, models.ModelProviderGoogle} {
		require.Empty(t, r.Key(context.Background(), p), "provider %q", p)
		require.False(t, r.Configured(p), "provider %q", p)
	}
}

// The deployment answer is the same for everyone; nothing about the caller may
// change it.
func TestDeploymentResolver_IgnoresTheCaller(t *testing.T) {
	t.Parallel()
	r := NewDeploymentResolver(map[models.ModelProvider]string{models.ModelProviderOpenAI: "sk-openai"})

	//nolint:staticcheck // a deliberately foreign context value
	ctx := context.WithValue(context.Background(), "user", "someone-else")
	require.Equal(t, "sk-openai", r.Key(ctx, models.ModelProviderOpenAI))
}

// A nil map is valid input: NewAgent passes nil when nothing is configured.
// len and range are both defined on a nil map, so this is a resolver with no
// credentials, not a panic.
func TestDeploymentResolver_NilMeansNothingConfigured(t *testing.T) {
	t.Parallel()
	r := NewDeploymentResolver(nil)
	require.Empty(t, r.Key(context.Background(), models.ModelProviderOpenAI))
	require.False(t, r.Configured(models.ModelProviderOpenAI))
}

// Callers hold the interface, so the concrete type must satisfy it.
func TestDeploymentResolverSatisfiesResolver(t *testing.T) {
	t.Parallel()
	var _ Resolver = NewDeploymentResolver(nil)
}

type fixedResolver string

func (f fixedResolver) Key(context.Context, models.ModelProvider) string { return string(f) }
func (fixedResolver) Configured(models.ModelProvider) bool               { return true }
func (fixedResolver) AccountsSupplyKeys() bool                           { return true }

// By default each account brings its own keys, and a key configured on the
// deployment is not used. AccountKeys is tested rather than New, because a
// build that supplies keys itself rebinds New.
func TestAccountKeysIgnoresTheDeploymentsKeys(t *testing.T) {
	t.Parallel()
	deployment := NewDeploymentResolver(map[models.ModelProvider]string{models.ModelProviderOpenAI: "sk-deployment"})

	r := AccountKeys(deployment, fixedResolver("sk-account"))
	require.Equal(t, "sk-account", r.Key(context.Background(), models.ModelProviderOpenAI))
	require.True(t, r.AccountsSupplyKeys())
}

// A missing key is recorded as a plain statement, not as the transport error
// wrapping it, which would include the request URL. Anything else is recorded
// as it is.
func TestFailureText(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("OpenAI API call failed: %w", &url.Error{
		Op: "Post", URL: "https://api.openai.com/v1/responses",
		Err: &NotConfiguredError{Provider: models.ModelProviderOpenAI},
	})
	require.Equal(t, "No OpenAI API key is set.", FailureText(wrapped))
	require.Equal(t, "No Xiaomi MiMo API key is set.", FailureText(&NotConfiguredError{Provider: models.ModelProviderXiaomi}))
	require.Equal(t, "No acme API key is set.", FailureText(&NotConfiguredError{Provider: "acme"}), "an unknown provider is named as it is")

	other := errors.New("model overloaded")
	require.Equal(t, "model overloaded", FailureText(other))
}

func TestNotConfiguredIsAnAuthFailure(t *testing.T) {
	t.Parallel()
	require.Equal(t, 401, (&NotConfiguredError{}).HTTPStatusCode())
}
