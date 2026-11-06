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
// datastore refuses a model the account can't use. It's deliberately simpler than the
// datastore: it doesn't check scope against owner or which models a scope may choose, which
// the datastore's own tests cover.
type fakeStore struct {
	mu       sync.Mutex
	settings []fakeSetting
	usable   map[uuid.UUID]bool
	// catalog holds the models GetModel finds; the rest are not found.
	catalog map[uuid.UUID]*models.Model
	// setErr, when set, is what SetUtilityModel fails with.
	setErr error
}

func (f *fakeStore) GetModel(_ context.Context, id uuid.UUID) (*models.Model, error) {
	if m, ok := f.catalog[id]; ok {
		return m, nil
	}
	return nil, datastore.ErrModelNotFound
}

// fakeKeys is a key resolver holding keys for the providers in keys.
type fakeKeys struct {
	accountsSupply bool
	keys           map[models.ModelProvider]bool
}

func (k fakeKeys) Key(_ context.Context, p models.ModelProvider) string {
	if k.keys[p] {
		return "key"
	}
	return ""
}

func (k fakeKeys) Configured(p models.ModelProvider) bool { return k.keys[p] }

func (k fakeKeys) AccountsSupplyKeys() bool { return k.accountsSupply }

type fakeSetting struct {
	models.UtilityModelSetting
	owner uuid.UUID
}

func (f *fakeStore) ListUtilityModelSettings(_ context.Context, userID uuid.UUID) ([]models.UtilityModelSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.UtilityModelSetting
	for _, s := range f.settings {
		if s.Scope == models.UtilityScopeDeployment || s.owner == userID {
			out = append(out, s.UtilityModelSetting)
		}
	}
	return out, nil
}

func (f *fakeStore) SetUtilityModel(ctx context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole, modelID uuid.UUID) error {
	if f.setErr != nil {
		return f.setErr
	}
	if !f.usable[modelID] {
		return datastore.ErrModelNotFound
	}
	_ = f.ClearUtilityModel(ctx, scope, userID, role)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings = append(f.settings, fakeSetting{models.UtilityModelSetting{Scope: scope, Role: role, ModelID: modelID}, userID})
	return nil
}

func (f *fakeStore) ClearUtilityModel(_ context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.settings[:0]
	for _, s := range f.settings {
		if !(s.Scope == scope && s.Role == role && s.owner == userID) {
			kept = append(kept, s)
		}
	}
	f.settings = kept
	return nil
}

type fixture struct {
	router  *mux.Router
	store   *fakeStore
	account uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{store: &fakeStore{usable: map[uuid.UUID]bool{}}, account: uuid.New()}
	f.router = mux.NewRouter()
	builtIn := func(r models.UtilityRole) string { return "built-in-" + string(r) }
	NewHandler(f.store, builtIn, fakeKeys{accountsSupply: true, keys: map[models.ModelProvider]bool{models.ModelProviderOpenAI: true}}, zap.NewNop()).RegisterRoutes(f.router)
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
	simple := roleModel(got, models.UtilityRoleSimple)
	require.Equal(t, &deployment, simple.DeploymentModelID)
	require.Nil(t, simple.AccountModelID, "another account's choice is not this one's")
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
	require.Contains(t, rec.Body.String(), "model_id is required")
	rec, _ = f.do(t, http.MethodPut, "/utility-models/memory", `{"model_id":`, true)
	require.Equal(t, http.StatusBadRequest, rec.Code, "unreadable body")
	require.Contains(t, rec.Body.String(), "Invalid request body")
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
	store := &fakeStore{usable: map[uuid.UUID]bool{}}
	account, model := uuid.New(), uuid.New()
	store.usable[model] = true
	require.NoError(t, store.SetUtilityModel(context.Background(), models.UtilityScopeAccount, account, models.UtilityRoleMemory, model))
	router := mux.NewRouter()
	NewHandler(store, func(models.UtilityRole) string { return "built-in" }, fakeKeys{}, zap.NewNop()).RegisterRoutes(router)
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

// A catalog model whose provider the account holds no key for would fail at every call, so
// choosing it is refused as provider_not_configured, naming the provider; a connection model
// carries its own key.
func TestSet_CatalogModelWithoutKeyIsRefused(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	claude, local, conn := uuid.New(), uuid.New(), uuid.New()
	f.store.usable[claude], f.store.usable[local] = true, true
	f.store.catalog = map[uuid.UUID]*models.Model{
		claude: {ID: claude, Name: "claude-haiku-4-5", Provider: string(models.ModelProviderAnthropic)},
		local:  {ID: local, Name: "qwen3:0.6b", Provider: "ollama", ConnectionID: &conn},
	}

	rec, _ := f.do(t, http.MethodPut, "/utility-models/memory", `{"model_id":"`+claude.String()+`"}`, true)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), models.ErrCodeProviderNotConfigured)
	require.Contains(t, rec.Body.String(), `"provider":"anthropic"`)
	require.Empty(t, f.store.settings, "nothing is saved")

	rec, _ = f.do(t, http.MethodPut, "/utility-models/memory", `{"model_id":"`+local.String()+`"}`, true)
	require.Equal(t, http.StatusOK, rec.Code, "a connection model needs no catalog key")
}

// An account deleted while the request ran is unauthorized, not a server error.
func TestSet_DeletedAccountIsUnauthorized(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.store.setErr = datastore.ErrUserNotFound
	rec, _ := f.do(t, http.MethodPut, "/utility-models/memory", `{"model_id":"`+uuid.New().String()+`"}`, true)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
