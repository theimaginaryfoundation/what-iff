package providerconnections

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/reauth"
)

// prefixSealer stands in for the token key: reversible, and visibly not the key.
type prefixSealer struct{}

func (prefixSealer) EncryptToken(p string) (string, error) { return "sealed:" + p, nil }
func (prefixSealer) DecryptToken(c string) (string, error) {
	return strings.TrimPrefix(c, "sealed:"), nil
}

// aliceOpenAI is a made-up OpenAI key.
const aliceOpenAI = "sk-alice-key-1234"

// ownerPassword confirms only the password "right".
type ownerPassword struct{}

func (ownerPassword) Confirm(_ context.Context, _ uuid.UUID, proof string) error {
	if proof != "right" {
		return reauth.ErrNotConfirmed
	}
	return nil
}

type fixture struct {
	store      *Store
	router     *mux.Router
	db         *sql.DB
	alice, bob uuid.UUID
	openai     *seen
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE users (id uuid PRIMARY KEY)`,
		`CREATE TABLE provider_connections (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			user_id uuid NULL,
			preset text NOT NULL,
			flavor text NOT NULL DEFAULT '',
			name text NOT NULL,
			base_url text NOT NULL DEFAULT '',
			key text NOT NULL DEFAULT '',
			hint text NOT NULL DEFAULT '',
			source text NOT NULL DEFAULT '',
			builtin text NULL,
			UNIQUE (user_id, builtin),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
	f := fixture{db: db, alice: uuid.New(), bob: uuid.New(), openai: &seen{}}
	for _, id := range []uuid.UUID{f.alice, f.bob} {
		_, err := db.Exec(`INSERT INTO users (id) VALUES (?)`, id)
		require.NoError(t, err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { _ = client.Close() })

	// OpenAI's preset points at a fake OpenAI-compatible server.
	srv := openAIServer(t, f.openai)
	presets := Presets(map[models.ModelProvider]string{models.ModelProviderOpenAI: srv.URL + "/v1"}, "localhost")
	f.store = NewStore(client, prefixSealer{})
	f.router = mux.NewRouter()
	NewHandler(f.store, NewLister(nil, false), presets, ownerPassword{}, zap.NewNop()).RegisterRoutes(f.router)
	return f
}

// call sends a request as userID and returns the recorder.
func (f fixture) call(t *testing.T, userID uuid.UUID, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		payload = strings.NewReader(string(b))
	} else {
		payload = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, payload)
	req = req.WithContext(middleware.ContextWithUser(req.Context(), userID, ""))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body.Code
}

// localServer is a fake OpenAI-compatible server on this computer.
func localServer(t *testing.T) (string, *seen) {
	s := &seen{}
	return openAIServer(t, s).URL + "/v1", s
}

func TestCreate_BuiltinListsThenStoresAKeyTheResolverReads(t *testing.T) {
	f := newFixture(t)
	rec := f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": "openai", "key": aliceOpenAI, "current_password": "right",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decode[connected](t, rec)
	require.Equal(t, "OpenAI", got.Connection.Name)
	require.True(t, got.Connection.Builtin)
	require.Equal(t, "1234", got.Connection.Hint)
	require.Equal(t, []ListedModel{{ID: "alpha"}, {ID: "zeta"}}, got.Models)
	require.Equal(t, "Bearer sk-alice-key-1234", f.openai.requests[0].Get("Authorization"), "the key being added is the one tested")
	require.NotContains(t, rec.Body.String(), "sk-alice-key-1234", "write-only")

	key, err := f.store.BuiltinKey(context.Background(), f.alice, models.ModelProviderOpenAI)
	require.NoError(t, err)
	require.Equal(t, "sk-alice-key-1234", key, "the same row the key resolver reads")
}

func TestCreate_SecondBuiltinIsAConflictAndCallsNothing(t *testing.T) {
	f := newFixture(t)
	_, err := f.store.SetBuiltinKey(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-alice-key-1234")
	require.NoError(t, err)

	rec := f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": "openai", "key": "sk-alice-key-9999", "current_password": "right",
	})
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Equal(t, models.ErrCodeProviderConnectionExists, errorCode(t, rec))
	require.Empty(t, f.openai.requests, "no listing for a connection that cannot be added")
}

func TestCreate_LocalServerWithoutAKey(t *testing.T) {
	f := newFixture(t)
	url, s := localServer(t)
	rec := f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url + "/", "current_password": "right",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decode[connected](t, rec)
	require.Equal(t, "Ollama", got.Connection.Name)
	require.Equal(t, url, got.Connection.BaseURL, "trimmed")
	require.False(t, got.Connection.HasKey)
	require.False(t, got.Connection.Builtin)
	require.Empty(t, got.Connection.Flavor, "a preset implies its flavor; only Custom stores one")
	require.Empty(t, s.requests[0].Get("Authorization"))
}

func TestCreate_Custom(t *testing.T) {
	f := newFixture(t)
	url, _ := localServer(t)
	body := map[string]string{
		"preset": PresetCustom, "name": "Home GPU box", "flavor": "openai", "base_url": url, "current_password": "right",
	}
	rec := f.call(t, f.alice, http.MethodPost, "/provider-connections", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, FlavorOpenAI, decode[connected](t, rec).Connection.Flavor)

	// Any number of them.
	body["name"] = "Lab server"
	require.Equal(t, http.StatusCreated, f.call(t, f.alice, http.MethodPost, "/provider-connections", body).Code)
	list := decode[[]Connection](t, f.call(t, f.alice, http.MethodGet, "/provider-connections", nil))
	require.Len(t, list, 2)
}

func TestCreate_RejectsBadInputBeforeCallingOut(t *testing.T) {
	f := newFixture(t)
	url, s := localServer(t)
	for name, body := range map[string]map[string]string{
		"unknown preset":         {"preset": "acme", "current_password": "right"},
		"built-in without a key": {"preset": "openai", "current_password": "right"},
		"short key":              {"preset": "openai", "key": "short", "current_password": "right"},
		"built-in address":       {"preset": "openai", "key": aliceOpenAI, "base_url": url, "current_password": "right"},
		"custom without flavor":  {"preset": PresetCustom, "name": "Box", "base_url": url, "current_password": "right"},
		"custom without name":    {"preset": PresetCustom, "flavor": "openai", "base_url": url, "current_password": "right"},
		"custom without address": {"preset": PresetCustom, "name": "Box", "flavor": "openai", "current_password": "right"},
		"not a URL":              {"preset": PresetOllama, "base_url": "localhost:11434", "current_password": "right"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.call(t, f.alice, http.MethodPost, "/provider-connections", body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
	require.Empty(t, s.requests)
	require.Empty(t, f.openai.requests)
}

func TestCreate_NeedsTheOwnersPassword(t *testing.T) {
	f := newFixture(t)
	url, s := localServer(t)
	rec := f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url, "current_password": "wrong",
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, models.ErrCodeReauthenticationRequired, errorCode(t, rec))
	require.Empty(t, s.requests, "an unconfirmed request reaches no address")
}

func TestCreate_FailedListingStoresNothing(t *testing.T) {
	f := newFixture(t)
	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL + "/v1"
	closed.Close()

	rec := f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url, "current_password": "right",
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, models.ErrCodeProviderUnreachable, errorCode(t, rec))
	require.Contains(t, rec.Body.String(), `"preset":"ollama"`)
	require.Empty(t, decode[[]Connection](t, f.call(t, f.alice, http.MethodGet, "/provider-connections", nil)))
}

func TestCustomAddressesOff(t *testing.T) {
	saved := CustomAddressesAllowed
	t.Cleanup(func() { CustomAddressesAllowed = saved })
	CustomAddressesAllowed = func() bool { return false }
	f := newFixture(t)
	url, s := localServer(t)

	presets := decode[[]Preset](t, f.call(t, f.alice, http.MethodGet, "/provider-connections/presets", nil))
	for _, p := range presets {
		require.False(t, p.AddressEditable, "not offered: %s", p.ID)
	}
	rec := f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url, "current_password": "right",
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, models.ErrCodeCustomAddressNotAllowed, errorCode(t, rec))
	require.Empty(t, s.requests)
}

func TestUpdate(t *testing.T) {
	f := newFixture(t)
	url, s := localServer(t)
	created := decode[connected](t, f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url, "current_password": "right",
	}))
	path := "/provider-connections/" + created.Connection.ID.String()
	before := len(s.requests)

	// A rename needs no password and calls nothing.
	rec := f.call(t, f.alice, http.MethodPatch, path, map[string]string{"name": "Desk Ollama"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "Desk Ollama", decode[connected](t, rec).Connection.Name)
	require.Len(t, s.requests, before)

	// A new key needs the owner, and is tested before it is saved.
	rec = f.call(t, f.alice, http.MethodPatch, path, map[string]string{"key": "local-key-5678"})
	require.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.call(t, f.alice, http.MethodPatch, path, map[string]string{"key": "local-key-5678", "current_password": "right"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "5678", decode[connected](t, rec).Connection.Hint)
	require.Equal(t, "Bearer local-key-5678", s.requests[len(s.requests)-1].Get("Authorization"))

	// A new address that does not answer leaves the connection as it was.
	closed := httptest.NewServer(http.NotFoundHandler())
	dead := closed.URL + "/v1"
	closed.Close()
	rec = f.call(t, f.alice, http.MethodPatch, path, map[string]string{"base_url": dead, "current_password": "right"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	list := decode[[]Connection](t, f.call(t, f.alice, http.MethodGet, "/provider-connections", nil))
	require.Equal(t, url, list[0].BaseURL)
}

func TestUpdate_BuiltinAddressIsFixed(t *testing.T) {
	f := newFixture(t)
	c, err := f.store.SetBuiltinKey(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-alice-key-1234")
	require.NoError(t, err)
	rec := f.call(t, f.alice, http.MethodPatch, "/provider-connections/"+c.ID.String(), map[string]string{
		"base_url": "http://localhost:9/v1", "current_password": "right",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestModelsAndDelete(t *testing.T) {
	f := newFixture(t)
	url, _ := localServer(t)
	created := decode[connected](t, f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url, "current_password": "right",
	}))
	path := "/provider-connections/" + created.Connection.ID.String()

	rec := f.call(t, f.alice, http.MethodGet, path+"/models", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, decode[[]ListedModel](t, rec), 2)

	require.Equal(t, http.StatusNoContent, f.call(t, f.alice, http.MethodDelete, path, nil).Code, "no password to remove one")
	require.Equal(t, http.StatusNotFound, f.call(t, f.alice, http.MethodGet, path+"/models", nil).Code)
}

func TestAnotherAccountsConnectionIsNotFound(t *testing.T) {
	f := newFixture(t)
	url, _ := localServer(t)
	created := decode[connected](t, f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url, "current_password": "right",
	}))
	path := "/provider-connections/" + created.Connection.ID.String()

	require.Equal(t, http.StatusNotFound, f.call(t, f.bob, http.MethodGet, path+"/models", nil).Code)
	require.Equal(t, http.StatusNotFound, f.call(t, f.bob, http.MethodPatch, path, map[string]string{"name": "Mine"}).Code)
	require.Equal(t, http.StatusNotFound, f.call(t, f.bob, http.MethodDelete, path, nil).Code)
	require.Empty(t, decode[[]Connection](t, f.call(t, f.bob, http.MethodGet, "/provider-connections", nil)))
	require.Len(t, decode[[]Connection](t, f.call(t, f.alice, http.MethodGet, "/provider-connections", nil)), 1)
}

func TestRoutesRequireAnAccount(t *testing.T) {
	f := newFixture(t)
	for _, route := range [][2]string{
		{http.MethodGet, "/provider-connections"},
		{http.MethodPost, "/provider-connections"},
		{http.MethodGet, "/provider-connections/presets"},
		{http.MethodPatch, "/provider-connections/" + uuid.NewString()},
		{http.MethodDelete, "/provider-connections/" + uuid.NewString()},
		{http.MethodGet, "/provider-connections/" + uuid.NewString() + "/models"},
	} {
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, httptest.NewRequest(route[0], route[1], strings.NewReader("{}")))
		require.Equal(t, http.StatusUnauthorized, rec.Code, route)
	}
}

// Turning custom addresses off also stops connections already stored with one.
func TestCustomAddressesOff_StopsStoredConnections(t *testing.T) {
	f := newFixture(t)
	url, s := localServer(t)
	created := decode[connected](t, f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url, "current_password": "right",
	}))
	path := "/provider-connections/" + created.Connection.ID.String()
	before := len(s.requests)

	saved := CustomAddressesAllowed
	t.Cleanup(func() { CustomAddressesAllowed = saved })
	CustomAddressesAllowed = func() bool { return false }

	rec := f.call(t, f.alice, http.MethodGet, path+"/models", nil)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, models.ErrCodeCustomAddressNotAllowed, errorCode(t, rec))
	rec = f.call(t, f.alice, http.MethodPatch, path, map[string]string{"key": "local-key-5678", "current_password": "right"})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Len(t, s.requests, before, "the address is not reached")

	require.Equal(t, http.StatusOK, f.call(t, f.alice, http.MethodPatch, path, map[string]string{"name": "Old box"}).Code, "renaming reaches nothing")
	require.Equal(t, http.StatusNoContent, f.call(t, f.alice, http.MethodDelete, path, nil).Code, "and it can still be removed")
}

// A local server at the preset's own address stores no address, so it follows
// the preset if LOCAL_SERVER_HOST changes; another address is stored.
func TestLocalAddressIsStoredOnlyWhenItDiffers(t *testing.T) {
	f := newFixture(t)
	url, _ := localServer(t)

	require.Equal(t, "", storedAddress("http://localhost:11434/v1", Preset{BaseURL: "http://localhost:11434/v1"}))
	require.Equal(t, "", storedAddress("http://localhost:11434/v1", Preset{BaseURL: "http://localhost:11434/v1/"}), "trailing slash alike")
	require.Equal(t, "http://localhost:11500/v1", storedAddress("http://localhost:11500/v1", Preset{BaseURL: "http://localhost:11434/v1"}))
	require.Equal(t, "http://box:8000/v1", storedAddress("http://box:8000/v1", Preset{}), "Custom has no default")

	// Through the route, an address other than the default is kept.
	created := decode[connected](t, f.call(t, f.alice, http.MethodPost, "/provider-connections", map[string]string{
		"preset": PresetOllama, "base_url": url, "current_password": "right",
	}))
	require.Equal(t, url, created.Connection.BaseURL)
}
