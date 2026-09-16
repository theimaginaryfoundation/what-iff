package providerkey

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
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
)

// newTestRouter stands up an in-memory database with just the tables these
// routes touch. The datastore package keeps its own harness unexported, and
// the behaviour worth pinning here — that a route only ever reaches the
// caller's own key — needs a real store to mean anything.
func newTestRouter(t *testing.T) (*mux.Router, *sql.DB, func()) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)

	for _, stmt := range []string{
		`CREATE TABLE users (
			id uuid PRIMARY KEY, created_at datetime NOT NULL, updated_at datetime NOT NULL,
			username varchar NOT NULL, email varchar NOT NULL, password_hash varchar NOT NULL,
			timezone varchar NOT NULL DEFAULT 'America/New_York',
			status varchar NOT NULL DEFAULT 'active',
			enable_experimental_models bool NOT NULL DEFAULT false)`,
		`CREATE TABLE user_provider_keys (
			id uuid PRIMARY KEY, created_at datetime NOT NULL, updated_at datetime NOT NULL,
			provider varchar(40) NOT NULL, encrypted_key varchar NOT NULL, key_hint varchar(12),
			user_provider_keys uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE)`,
		`CREATE UNIQUE INDEX userproviderkey_provider_user_provider_keys
			ON user_provider_keys (provider, user_provider_keys)`,
	} {
		_, execErr := db.Exec(stmt)
		require.NoError(t, execErr)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	r := mux.NewRouter()
	NewHandler(ds, zap.NewNop(), nil).RegisterRoutes(r)
	return r, db, func() { _ = client.Close(); _ = db.Close() }
}

func insertUser(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO users (id, created_at, updated_at, username, email, password_hash)
		VALUES (?, datetime('now'), datetime('now'), ?, ?, 'x')`, id, name, name+"@example.test")
	require.NoError(t, err)
	return id
}

// asUser builds a request carrying an authenticated actor the way the auth
// middleware would.
func asUser(method, target, body string, userID uuid.UUID) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	ctx := apicontext.WithUserID(req.Context(), userID)
	ctx = context.WithValue(ctx, middleware.UserIDKey, userID)
	return req.WithContext(ctx)
}

func do(t *testing.T, r *mux.Router, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// The account comes from the request context, never the payload or the path,
// so one user's writes and reads cannot reach another's credential.
func TestKeysAreScopedToTheAuthenticatedAccount(t *testing.T) {
	r, db, cleanup := newTestRouter(t)
	defer cleanup()
	alice := insertUser(t, db, "alice")
	bob := insertUser(t, db, "bob")

	rec := do(t, r, asUser(http.MethodPut, "/provider-keys/openai", `{"key":"sk-alice-abcd"}`, alice))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Bob sees nothing of Alice's.
	rec = do(t, r, asUser(http.MethodGet, "/provider-keys", "", bob))
	require.Equal(t, http.StatusOK, rec.Code)
	var statuses []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &statuses))
	for _, s := range statuses {
		require.Equal(t, false, s["configured"], "bob must not inherit alice's key: %v", s)
	}

	// Bob deleting "his" openai key must not touch Alice's row.
	rec = do(t, r, asUser(http.MethodDelete, "/provider-keys/openai", "", bob))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = do(t, r, asUser(http.MethodGet, "/provider-keys", "", alice))
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &statuses))
	require.Equal(t, true, statuses[0]["configured"], "alice's key was collateral damage")
	require.Equal(t, "…abcd", statuses[0]["key_hint"])
}

// The stored key must never come back out of the API.
func TestResponsesNeverCarryTheKey(t *testing.T) {
	r, db, cleanup := newTestRouter(t)
	defer cleanup()
	alice := insertUser(t, db, "alice")

	const secret = "sk-alice-supersecret-9999"
	rec := do(t, r, asUser(http.MethodPut, "/provider-keys/openai", `{"key":"`+secret+`"}`, alice))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), secret)

	rec = do(t, r, asUser(http.MethodGet, "/provider-keys", "", alice))
	require.NotContains(t, rec.Body.String(), secret)
}

// A provider whose key we would not actually use is refused rather than stored
// and reported as configured.
func TestUnsupportedProviderIsRefused(t *testing.T) {
	r, db, cleanup := newTestRouter(t)
	defer cleanup()
	alice := insertUser(t, db, "alice")

	rec := do(t, r, asUser(http.MethodPut, "/provider-keys/anthropic", `{"key":"sk-ant-1234"}`, alice))
	require.Equal(t, http.StatusNotImplemented, rec.Code)

	var rows int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_provider_keys`).Scan(&rows))
	require.Zero(t, rows, "a refused provider must not leave a row behind")
}

func TestUnknownProviderRejected(t *testing.T) {
	r, db, cleanup := newTestRouter(t)
	defer cleanup()
	alice := insertUser(t, db, "alice")

	rec := do(t, r, asUser(http.MethodPut, "/provider-keys/not-a-provider", `{"key":"x"}`, alice))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUnauthenticatedRejected(t *testing.T) {
	r, _, cleanup := newTestRouter(t)
	defer cleanup()
	rec := do(t, r, httptest.NewRequest(http.MethodGet, "/provider-keys", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
