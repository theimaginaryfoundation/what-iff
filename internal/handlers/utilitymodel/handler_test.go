package utilitymodel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// fakeStore keeps settings in memory and refuses any model not in usable, the way the
// datastore refuses a model the account can't use.
type fakeStore struct {
	mu       sync.Mutex
	settings []models.UtilityModelSetting
	owners   map[int]uuid.UUID
	usable   map[uuid.UUID]bool
}

func (f *fakeStore) ListUtilityModelSettings(_ context.Context, userID uuid.UUID) ([]models.UtilityModelSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.UtilityModelSetting
	for i, s := range f.settings {
		if s.Scope == models.UtilityScopeDeployment || f.owners[i] == userID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStore) SetUtilityModel(ctx context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole, modelID uuid.UUID) error {
	if !f.usable[modelID] {
		return datastore.ErrModelNotFound
	}
	_ = f.ClearUtilityModel(ctx, scope, userID, role)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners[len(f.settings)] = userID
	f.settings = append(f.settings, models.UtilityModelSetting{Scope: scope, Role: role, ModelID: modelID})
	return nil
}

func (f *fakeStore) ClearUtilityModel(_ context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, s := range f.settings {
		if s.Scope == scope && s.Role == role && f.owners[i] == userID {
			f.settings[i].Role = "" // gone
		}
	}
	return nil
}

type fixture struct {
	router  *mux.Router
	store   *fakeStore
	account uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{store: &fakeStore{owners: map[int]uuid.UUID{}, usable: map[uuid.UUID]bool{}}, account: uuid.New()}
	f.router = mux.NewRouter()
	builtIn := func(r models.UtilityRole) string { return "built-in-" + string(r) }
	NewHandler(f.store, builtIn, true, zap.NewNop()).RegisterRoutes(f.router)
	return f
}

func (f *fixture) do(t *testing.T, method, path, body string, signedIn bool) (*httptest.ResponseRecorder, models.UtilityModels) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if signedIn {
		req = req.WithContext(apicontext.WithUserID(req.Context(), f.account))
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	var got models.UtilityModels
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	}
	return rec, got
}

func roleModel(got models.UtilityModels, role models.UtilityRole) models.UtilityRoleModel {
	for _, r := range got.Roles {
		if r.Role == role {
			return r
		}
	}
	return models.UtilityRoleModel{}
}

func TestList_EveryRoleInOrderWithBothScopes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	deployment, other := uuid.New(), uuid.New()
	f.store.usable[deployment], f.store.usable[other] = true, true
	require.NoError(t, f.store.SetUtilityModel(context.Background(), models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleSimple, deployment))
	require.NoError(t, f.store.SetUtilityModel(context.Background(), models.UtilityScopeAccount, uuid.New(), models.UtilityRoleSimple, other))

	rec, got := f.do(t, http.MethodGet, "/utility-models", "", true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, got.Roles, len(models.UtilityRoles))
	for i, r := range got.Roles {
		require.Equal(t, models.UtilityRoles[i], r.Role)
		require.Equal(t, "built-in-"+string(r.Role), r.BuiltInModel)
	}
	quick := roleModel(got, models.UtilityRoleSimple)
	require.Equal(t, &deployment, quick.DeploymentModelID)
	require.Nil(t, quick.AccountModelID, "another account's choice is not this one's")
	require.Contains(t, rec.Body.String(), `"account_model_id":null`, "an unset choice is null, not missing")
	require.True(t, got.AccountsChoose)
}

func TestSetAndClear(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	model := uuid.New()
	f.store.usable[model] = true

	rec, got := f.do(t, http.MethodPut, "/utility-models/memory", `{"model_id":"`+model.String()+`"}`, true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, &model, roleModel(got, models.UtilityRoleMemory).AccountModelID)

	rec, got = f.do(t, http.MethodDelete, "/utility-models/memory", "", true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Nil(t, roleModel(got, models.UtilityRoleMemory).AccountModelID)
}

func TestSet_Refusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	model := uuid.New()
	f.store.usable[model] = true
	body := `{"model_id":"` + model.String() + `"}`

	rec, _ := f.do(t, http.MethodPut, "/utility-models/memory", `{"model_id":"`+uuid.NewString()+`"}`, true)
	require.Equal(t, http.StatusBadRequest, rec.Code, "a model the account can't use")
	rec, _ = f.do(t, http.MethodPut, "/utility-models/memory", `{}`, true)
	require.Equal(t, http.StatusBadRequest, rec.Code, "no model")
	rec, _ = f.do(t, http.MethodPut, "/utility-models/images", body, true)
	require.Equal(t, http.StatusNotFound, rec.Code, "not a role")
	rec, _ = f.do(t, http.MethodDelete, "/utility-models/images", "", true)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec, _ = f.do(t, http.MethodPut, "/utility-models/memory", body, false)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	rec, _ = f.do(t, http.MethodGet, "/utility-models", "", false)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, f.store.settings, "a refused write changes nothing")
}

// Where the deployment supplies the keys it pays for background work, so it chooses: an
// account's writes are refused, and a choice stored before isn't reported as in force.
func TestDeploymentChooses(t *testing.T) {
	t.Parallel()
	store := &fakeStore{owners: map[int]uuid.UUID{}, usable: map[uuid.UUID]bool{}}
	account, model := uuid.New(), uuid.New()
	store.usable[model] = true
	require.NoError(t, store.SetUtilityModel(context.Background(), models.UtilityScopeAccount, account, models.UtilityRoleMemory, model))
	router := mux.NewRouter()
	NewHandler(store, func(models.UtilityRole) string { return "built-in" }, false, zap.NewNop()).RegisterRoutes(router)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(apicontext.WithUserID(req.Context(), account))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodGet, "/utility-models", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got models.UtilityModels
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.False(t, got.AccountsChoose)
	require.Nil(t, roleModel(got, models.UtilityRoleMemory).AccountModelID)

	require.Equal(t, http.StatusForbidden, do(http.MethodPut, "/utility-models/memory", `{"model_id":"`+model.String()+`"}`).Code)
	require.Equal(t, http.StatusForbidden, do(http.MethodDelete, "/utility-models/memory", "").Code)
}
