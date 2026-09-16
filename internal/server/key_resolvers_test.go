package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	appmodels "github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// The key screen uses these resolvers to tell "nobody configured this" from
// "the deployment's key covers you". An empty map collapses those two into the
// first, so an install that set OPENAI_API_KEY as the README describes was told
// to go and add a key — on every navigation, because the setup guard redirects
// on exactly that signal.
func TestBuildKeyResolvers_CoversEveryCatalogProviderWithADeploymentKey(t *testing.T) {
	t.Parallel()

	registry := providerkeys.NewRegistry(nil, providerkeys.DeploymentKeys{OpenAI: "sk-deployment"})
	resolvers := buildKeyResolvers(registry)

	require.NotEmpty(t, resolvers, "an empty map makes the deployment fallback invisible")
	require.Contains(t, resolvers, string(appmodels.ModelProviderOpenAI))
	require.NotNil(t, resolvers[string(appmodels.ModelProviderOpenAI)])
}

func TestBuildKeyResolvers_NilRegistryDoesNotPanic(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() { _ = buildKeyResolvers(nil) })
}
