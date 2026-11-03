package accountkeys

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/reauth"
)

// prefixSealer stands in for the token key: reversible, and visibly not the key.
type prefixSealer struct{ err error }

func (s prefixSealer) EncryptToken(p string) (string, error) {
	return "sealed:" + p, s.err
}

func (s prefixSealer) DecryptToken(c string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return strings.TrimPrefix(c, "sealed:"), nil
}

type fixture struct {
	keys       *Keys
	client     *ent.Client
	db         *sql.DB
	alice, bob uuid.UUID
}

func newFixture(t *testing.T, sealer Sealer) fixture {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE users (id uuid PRIMARY KEY)`,
		`CREATE TABLE account_provider_keys (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			user_id uuid NOT NULL,
			provider text NOT NULL,
			key text NOT NULL,
			hint text NOT NULL DEFAULT '',
			UNIQUE (user_id, provider),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
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
		`CREATE UNIQUE INDEX providerconnection_builtin ON provider_connections (builtin) WHERE user_id IS NULL`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
	drv := entsql.OpenDB(dialect.SQLite, db)
	f := fixture{db: db, alice: uuid.New(), bob: uuid.New()}
	for _, id := range []uuid.UUID{f.alice, f.bob} {
		_, err := db.Exec(`INSERT INTO users (id) VALUES (?)`, id)
		require.NoError(t, err)
	}
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })
	f.client = client
	f.keys = New(client, sealer, ownerPassword{}, zap.NewNop())
	return f
}

func asUser(id uuid.UUID) context.Context {
	return middleware.ContextWithUser(context.Background(), id, "")
}

// --- Storage

func TestSet_StoresSealedAndAnswersWithTheKey(t *testing.T) {
	f := newFixture(t, prefixSealer{})

	summary, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "  sk-test-abcd1234  ")
	require.NoError(t, err)
	require.Equal(t, models.ModelProviderOpenAI, summary.Provider)
	require.Equal(t, "1234", summary.Hint)

	var stored string
	require.NoError(t, f.db.QueryRow(`SELECT key FROM provider_connections`).Scan(&stored))
	require.Equal(t, "sealed:sk-test-abcd1234", stored, "stored trimmed and sealed")
	require.Equal(t, "sk-test-abcd1234", f.keys.Key(asUser(f.alice), models.ModelProviderOpenAI))
}

func TestSet_AgainReplacesTheKey(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	ctx := context.Background()

	_, err := f.keys.Set(ctx, f.alice, models.ModelProviderOpenAI, "sk-first-key-1111")
	require.NoError(t, err)
	summary, err := f.keys.Set(ctx, f.alice, models.ModelProviderOpenAI, "sk-second-key-2222")
	require.NoError(t, err)
	require.Equal(t, "2222", summary.Hint)

	require.Equal(t, "sk-second-key-2222", f.keys.Key(asUser(f.alice), models.ModelProviderOpenAI))
	list, err := f.keys.List(ctx, f.alice)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestSet_Validation(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	ctx := context.Background()

	_, err := f.keys.Set(ctx, f.alice, "acme", "sk-valid-key-0000")
	require.ErrorIs(t, err, ErrUnknownProvider)

	for name, key := range map[string]string{
		"empty":        "   ",
		"too short":    "sk-1234",
		"inner space":  "sk-has a-space",
		"inner tab":    "sk-has\ta-tab-00",
		"inner return": "sk-line\nbreak-00",
		"too long":     strings.Repeat("k", 513),
	} {
		_, err := f.keys.Set(ctx, f.alice, models.ModelProviderOpenAI, key)
		require.ErrorIs(t, err, ErrInvalidKey, name)
	}
	list, err := f.keys.List(ctx, f.alice)
	require.NoError(t, err)
	require.Empty(t, list, "a rejected key is not stored")
}

func TestSet_SealerFailureStoresNothing(t *testing.T) {
	f := newFixture(t, prefixSealer{err: errors.New("no token key")})

	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-test-abcd1234")
	require.Error(t, err)
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM provider_connections`).Scan(&n))
	require.Zero(t, n)
}

func TestList_IsPerAccountInProviderOrder(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	ctx := context.Background()
	for _, p := range []models.ModelProvider{models.ModelProviderZAI, models.ModelProviderAnthropic, models.ModelProviderOpenAI} {
		_, err := f.keys.Set(ctx, f.alice, p, "sk-key-for-"+string(p))
		require.NoError(t, err)
	}

	list, err := f.keys.List(ctx, f.alice)
	require.NoError(t, err)
	var got []models.ModelProvider
	for _, k := range list {
		got = append(got, k.Provider)
	}
	require.Equal(t, []models.ModelProvider{models.ModelProviderAnthropic, models.ModelProviderOpenAI, models.ModelProviderZAI}, got)

	list, err = f.keys.List(ctx, f.bob)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestDelete(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	ctx := context.Background()
	_, err := f.keys.Set(ctx, f.alice, models.ModelProviderAnthropic, "sk-ant-alice-9999")
	require.NoError(t, err)

	require.ErrorIs(t, f.keys.Delete(ctx, f.bob, models.ModelProviderAnthropic), ErrKeyNotFound, "another account's key is not found")
	require.Equal(t, "sk-ant-alice-9999", f.keys.Key(asUser(f.alice), models.ModelProviderAnthropic))

	require.NoError(t, f.keys.Delete(ctx, f.alice, models.ModelProviderAnthropic))
	require.Empty(t, f.keys.Key(asUser(f.alice), models.ModelProviderAnthropic))
	require.ErrorIs(t, f.keys.Delete(ctx, f.alice, models.ModelProviderAnthropic), ErrKeyNotFound)
}

// Deleting an account deletes its keys. The cascade is on this table's own
// foreign key, so it comes from the generated migration, not the User schema.
func TestKeysAreDeletedWithTheAccount(t *testing.T) {
	fks := migrate.ProviderConnectionsTable.ForeignKeys
	require.Len(t, fks, 1)
	require.Equal(t, migrate.UsersTable, fks[0].RefTable)
	require.Equal(t, entschema.Cascade, fks[0].OnDelete)

	f := newFixture(t, prefixSealer{})
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-test-abcd1234")
	require.NoError(t, err)
	_, err = f.db.Exec(`DELETE FROM users WHERE id = ?`, f.alice)
	require.NoError(t, err)

	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM provider_connections`).Scan(&n))
	require.Zero(t, n)
}

// The fixture writes its tables by hand. These are the schema facts it and the
// one-built-in-per-provider rule rely on.
func TestProviderConnectionsSchema(t *testing.T) {
	fks := migrate.ProviderConnectionsTable.ForeignKeys
	require.Len(t, fks, 1)
	require.Equal(t, migrate.UsersTable, fks[0].RefTable)

	type unique struct {
		columns []string
		where   string
	}
	var got []unique
	for _, idx := range migrate.ProviderConnectionsTable.Indexes {
		if !idx.Unique {
			continue
		}
		u := unique{}
		for _, c := range idx.Columns {
			u.columns = append(u.columns, c.Name)
		}
		if idx.Annotation != nil {
			u.where = idx.Annotation.Where
		}
		got = append(got, u)
	}
	require.Equal(t, []unique{
		{columns: []string{"user_id", "builtin"}},
		{columns: []string{"builtin"}, where: "user_id IS NULL"},
	}, got)
}

// The deployment's own connections, with no account, are one per built-in
// provider too; a NULL user_id alone would let them repeat.
func TestDeploymentConnectionsAreOnePerBuiltin(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	add := func(name string) error {
		_, err := f.client.ProviderConnection.Create().
			SetPreset("openai").
			SetBuiltin("openai").
			SetName(name).
			Save(context.Background())
		return err
	}
	require.NoError(t, add("Deployment OpenAI"))
	require.Error(t, add("Second deployment OpenAI"))

	// Accounts are unaffected: each still has its own.
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-alice-key-1111")
	require.NoError(t, err)
	_, err = f.keys.Set(context.Background(), f.bob, models.ModelProviderOpenAI, "sk-bob-key-2222")
	require.NoError(t, err)
}

// Connections that are not a built-in provider's sit beside the built-in ones
// and stay out of the key routes and the resolver.
func TestOtherConnectionsAreNotKeys(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-test-abcd1234")
	require.NoError(t, err)
	for _, name := range []string{"Home GPU box", "Lab server"} {
		_, err := f.client.ProviderConnection.Create().
			SetUserID(f.alice).
			SetPreset("custom").
			SetFlavor("openai").
			SetName(name).
			SetBaseURL("http://localhost:8000/v1").
			Save(context.Background())
		require.NoError(t, err, "any number of connections that are not built-in")
	}

	keys, err := f.keys.List(context.Background(), f.alice)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, models.ModelProviderOpenAI, keys[0].Provider)
}

// --- Resolver

// One account's key is never another's answer.
func TestKey_AnswersWithTheRequestingAccountsKey(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	ctx := context.Background()
	_, err := f.keys.Set(ctx, f.alice, models.ModelProviderOpenAI, "sk-alice-key-1111")
	require.NoError(t, err)
	_, err = f.keys.Set(ctx, f.bob, models.ModelProviderOpenAI, "sk-bob-key-2222")
	require.NoError(t, err)

	require.Equal(t, "sk-alice-key-1111", f.keys.Key(asUser(f.alice), models.ModelProviderOpenAI))
	require.Equal(t, "sk-bob-key-2222", f.keys.Key(asUser(f.bob), models.ModelProviderOpenAI))
	require.Empty(t, f.keys.Key(asUser(f.alice), models.ModelProviderAnthropic))
}

// Background work records its account with apicontext alone where it cannot
// import middleware; the resolver must still find it.
func TestKey_ReadsAnAccountRecordedWithAPIContext(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-alice-key-1111")
	require.NoError(t, err)

	require.Equal(t, "sk-alice-key-1111", f.keys.Key(apicontext.WithUserID(context.Background(), f.alice), models.ModelProviderOpenAI))
}

// A context naming no account gets no key, rather than somebody else's.
func TestKey_NoAccountNoKey(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-alice-key-1111")
	require.NoError(t, err)

	require.Empty(t, f.keys.Key(context.Background(), models.ModelProviderOpenAI))
}

func TestKey_UnreadableKeyIsNoKey(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-alice-key-1111")
	require.NoError(t, err)
	// The same rows, read with a token key that no longer opens them.
	changed := New(f.client, prefixSealer{err: errors.New("token key changed")}, ownerPassword{}, zap.NewNop())

	require.Empty(t, changed.Key(asUser(f.alice), models.ModelProviderOpenAI))
}

// Which accounts have keys is only known per request, so every client is built.
func TestConfigured_EveryProvider(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	for _, p := range models.ModelProviders {
		require.True(t, f.keys.Configured(p), p)
	}
	require.True(t, f.keys.AccountsSupplyKeys())
}

// --- Routes

// testOwnerPassword is what ownerPassword accepts as the owner's confirmation.
const testOwnerPassword = "owner-password"

// ownerPassword confirms when the proof is testOwnerPassword, and fails with
// errConfirmBroken when it is "break".
type ownerPassword struct{}

var errConfirmBroken = errors.New("password store unavailable")

func (ownerPassword) Confirm(_ context.Context, _ uuid.UUID, proof string) error {
	switch proof {
	case testOwnerPassword:
		return nil
	case "break":
		return errConfirmBroken
	}
	return reauth.ErrNotConfirmed
}

// keyBody is a PUT body carrying key, confirmed with the owner's password.
func keyBody(key string) string {
	return keyBodyWith(key, testOwnerPassword)
}

func keyBodyWith(key, password string) string {
	b, _ := json.Marshal(map[string]string{"key": key, "current_password": password})
	return string(b)
}

func serve(t *testing.T, f fixture, userID *uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	f.keys.RegisterRoutes(router)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != nil {
		req = req.WithContext(middleware.ContextWithUser(req.Context(), *userID, ""))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRoutes_SetListDelete(t *testing.T) {
	f := newFixture(t, prefixSealer{})

	rec := serve(t, f, &f.alice, http.MethodPut, "/provider-keys/openai", keyBody("sk-test-abcd1234"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "sk-test", "the key is never sent back")

	rec = serve(t, f, &f.alice, http.MethodGet, "/provider-keys", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list []models.ProviderKeySummary
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.Equal(t, "1234", list[0].Hint)
	require.NotContains(t, rec.Body.String(), "sk-test")

	rec = serve(t, f, &f.alice, http.MethodDelete, "/provider-keys/openai", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = serve(t, f, &f.alice, http.MethodDelete, "/provider-keys/openai", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRoutes_RejectBadInput(t *testing.T) {
	f := newFixture(t, prefixSealer{})

	rec := serve(t, f, &f.alice, http.MethodPut, "/provider-keys/acme", keyBody("sk-test-abcd1234"))
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = serve(t, f, &f.alice, http.MethodPut, "/provider-keys/openai", keyBody("short"))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = serve(t, f, &f.alice, http.MethodPut, "/provider-keys/openai", `not json`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRoutes_RequireAnAccount(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/provider-keys", ""},
		{http.MethodPut, "/provider-keys/openai", keyBody("sk-test-abcd1234")},
		{http.MethodDelete, "/provider-keys/openai", ""},
	} {
		rec := serve(t, f, nil, r.method, r.path, r.body)
		require.Equal(t, http.StatusUnauthorized, rec.Code, r.method)
	}
	var n int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM provider_connections`).Scan(&n))
	require.Zero(t, n)
}

// Setting a key needs the owner's confirmation: a session alone could otherwise
// redirect the account's conversations to someone else's provider account.
func TestRoutes_SetRequiresTheOwnersConfirmation(t *testing.T) {
	f := newFixture(t, prefixSealer{})

	for name, password := range map[string]string{"missing": "", "wrong": "not-the-password"} {
		rec := serve(t, f, &f.alice, http.MethodPut, "/provider-keys/openai", keyBodyWith("sk-test-abcd1234", password))
		require.Equal(t, http.StatusForbidden, rec.Code, name)
		var body models.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, models.ErrCodeReauthenticationRequired, body.Code, name)
	}
	list, err := f.keys.List(context.Background(), f.alice)
	require.NoError(t, err)
	require.Empty(t, list, "an unconfirmed key is not stored")

	rec := serve(t, f, &f.alice, http.MethodPut, "/provider-keys/openai", keyBodyWith("sk-test-abcd1234", "break"))
	require.Equal(t, http.StatusInternalServerError, rec.Code, "a failed check is not a rejection")

	rec = serve(t, f, &f.alice, http.MethodPut, "/provider-keys/openai", keyBody("sk-test-abcd1234"))
	require.Equal(t, http.StatusOK, rec.Code)
}

// Removing a key only breaks things visibly, so it needs no password.
func TestRoutes_DeleteNeedsNoPassword(t *testing.T) {
	f := newFixture(t, prefixSealer{})
	_, err := f.keys.Set(context.Background(), f.alice, models.ModelProviderOpenAI, "sk-test-abcd1234")
	require.NoError(t, err)

	rec := serve(t, f, &f.alice, http.MethodDelete, "/provider-keys/openai", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
}
