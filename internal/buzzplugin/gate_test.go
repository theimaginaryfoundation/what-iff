package buzzplugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// onlyFor is a per-user experimental gate that has the relay on for one user.
type onlyFor uuid.UUID

func (g onlyFor) ExperimentalEnabled(_ context.Context, userID uuid.UUID, feature string) bool {
	return feature == models.ExperimentalFeatureBuzzRelay && userID == uuid.UUID(g)
}

func withExperimentalGate(t *testing.T, g featuregate.ExperimentalGate) {
	t.Helper()
	orig := featuregate.ActiveExperimental
	featuregate.ActiveExperimental = g
	t.Cleanup(func() { featuregate.ActiveExperimental = orig })
}

func TestRoutesAreNotFoundForUsersWithoutTheFeature(t *testing.T) {
	t.Setenv("BUZZ_RELAY_ENABLED", "true")
	tester, other := uuid.New(), uuid.New()
	withExperimentalGate(t, onlyFor(tester))

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := experimentalOnly(zap.NewNop())(ok)
	call := func(user *uuid.UUID) int {
		req := httptest.NewRequest(http.MethodGet, "/api/buzz/agents", nil)
		if user != nil {
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, *user))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusNoContent, call(&tester))
	assert.Equal(t, http.StatusNotFound, call(&other), "the web app hides Buzz on a 404")
	assert.Equal(t, http.StatusNotFound, call(nil))
}

func TestGatewayKeepsOnlyConnectionsOfOwnersWithTheFeature(t *testing.T) {
	t.Setenv("BUZZ_RELAY_ENABLED", "true")
	tester, other := uuid.New(), uuid.New()
	withExperimentalGate(t, onlyFor(tester))

	conn := func(owner uuid.UUID) models.BuzzConnection {
		return models.BuzzConnection{Agent: models.BuzzAgentCredentials{ID: uuid.New(), OwnerID: owner}}
	}
	all := []models.BuzzConnection{conn(tester), conn(other), conn(tester)}
	list := mayUse(func(context.Context) ([]models.BuzzConnection, error) {
		return append([]models.BuzzConnection(nil), all...), nil
	})

	got, err := list(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, c := range got {
		assert.Equal(t, tester, c.Agent.OwnerID)
	}

	t.Setenv("BUZZ_RELAY_ENABLED", "false")
	got, err = list(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got, "switching the relay off closes every connection")
}
