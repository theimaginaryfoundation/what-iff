package provideraccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerconnections"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// accountResolver answers like account keys: per request, true for
// AccountsSupplyKeys.
type accountResolver map[models.ModelProvider]string

func (a accountResolver) Key(_ context.Context, p models.ModelProvider) string { return a[p] }
func (accountResolver) Configured(models.ModelProvider) bool                   { return true }
func (accountResolver) AccountsSupplyKeys() bool                               { return true }

func get(t *testing.T, resolver providerkeys.Resolver) (models.ProviderAccess, string) {
	t.Helper()
	router := mux.NewRouter()
	NewHandler(resolver, zap.NewNop()).RegisterRoutes(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/provider-access", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got models.ProviderAccess
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got, rec.Body.String()
}

func TestDeploymentKeys(t *testing.T) {
	got, body := get(t, providerkeys.NewDeploymentResolver(map[models.ModelProvider]string{
		models.ModelProviderOpenAI:  "sk-deployment-openai",
		models.ModelProviderMistral: "sk-deployment-mistral",
	}))

	require.Equal(t, models.ProviderAccess{
		KeysFrom:        models.KeysFromDeployment,
		Providers:       []models.ModelProvider{models.ModelProviderOpenAI, models.ModelProviderMistral},
		CustomAddresses: true,
	}, got)
	require.NotContains(t, body, "sk-deployment", "the response names providers, never keys")
}

func TestAccountKeys(t *testing.T) {
	got, body := get(t, accountResolver{models.ModelProviderAnthropic: "sk-account-anthropic"})

	require.Equal(t, models.ProviderAccess{
		KeysFrom:        models.KeysFromAccount,
		Providers:       []models.ModelProvider{models.ModelProviderAnthropic},
		CustomAddresses: true,
	}, got)
	require.NotContains(t, body, "sk-account")
}

// An account with no keys yet gets an empty list, not null, so the frontend
// can treat it as "nothing set up" without a special case.
func TestNoKeysIsAnEmptyList(t *testing.T) {
	_, body := get(t, accountResolver{})
	require.JSONEq(t, `{"keys_from":"account","providers":[],"custom_addresses":true}`, body)
}

// Whether accounts may choose a provider's address is the deployment's to
// say, through providerconnections.CustomAddressesAllowed.
func TestCustomAddressesFollowTheDeployment(t *testing.T) {
	saved := providerconnections.CustomAddressesAllowed
	t.Cleanup(func() { providerconnections.CustomAddressesAllowed = saved })
	providerconnections.CustomAddressesAllowed = func() bool { return false }

	got, _ := get(t, accountResolver{})
	require.False(t, got.CustomAddresses)
}
