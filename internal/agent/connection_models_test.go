package agent

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// connectionFixture is an Agent over in-memory SQLite holding two accounts,
// each with one provider connection and one model enabled on it, and a catalog
// model. Its connection resolver answers only for each connection's owner,
// with whatever endpoint the test puts in endpoints.
type connectionFixture struct {
	a          *Agent
	ent        *ent.Client
	db         *sql.DB
	alice, bob uuid.UUID
	// aliceModel and bobModel are the models enabled on each account's connection.
	aliceModel, bobModel *ent.Model
	catalog              *ent.Model

	mu        sync.Mutex
	owners    map[uuid.UUID]uuid.UUID
	endpoints map[uuid.UUID]ConnectionEndpoint
}

func newConnectionFixture(t *testing.T) *connectionFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&_fk=1")
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE users (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			username text NOT NULL UNIQUE,
			email text NOT NULL UNIQUE,
			password_hash text NOT NULL,
			first_name text,
			last_name text,
			timezone text NOT NULL DEFAULT 'America/New_York',
			status text NOT NULL DEFAULT 'active',
			enable_experimental_models bool NOT NULL DEFAULT false,
			last_login datetime,
			last_seen datetime,
			terms_accepted_at datetime,
			refresh_token_id text
		)`,
		`CREATE TABLE provider_connections (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			user_id uuid NULL REFERENCES users(id) ON DELETE CASCADE,
			preset text NOT NULL,
			flavor text NOT NULL DEFAULT '',
			name text NOT NULL,
			base_url text NOT NULL DEFAULT '',
			key text NOT NULL DEFAULT '',
			hint text NOT NULL DEFAULT '',
			source text NOT NULL DEFAULT '',
			builtin text NULL
		)`,
		`CREATE TABLE models (
			id uuid PRIMARY KEY,
			name text NOT NULL,
			display_name text NOT NULL,
			description text NOT NULL,
			provider text NOT NULL DEFAULT 'openai',
			tool_support bool NOT NULL DEFAULT false,
			reasoning_support_override text NULL,
			vision_support bool NULL,
			base_credits_per_slab integer NOT NULL DEFAULT 1,
			subscription_tier text NOT NULL DEFAULT 'high',
			deleted bool NOT NULL DEFAULT false,
			is_default bool NOT NULL DEFAULT false,
			connection_id uuid NULL REFERENCES provider_connections(id) ON DELETE CASCADE,
			UNIQUE (connection_id, name)
		)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
	// Preferences from the ent schema, without the foreign keys that reach the rest of it.
	migrator, err := schema.NewMigrate(entsql.OpenDB(dialect.SQLite, db), schema.WithForeignKeys(false))
	require.NoError(t, err)
	prefs := migrate.UserPreferencesTable
	require.NoError(t, migrator.Create(ctx, &schema.Table{Name: prefs.Name, Columns: prefs.Columns, PrimaryKey: prefs.PrimaryKey, Indexes: prefs.Indexes}))
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { _ = client.Close(); _ = db.Close() })
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	f := &connectionFixture{ent: client, db: db, owners: map[uuid.UUID]uuid.UUID{}, endpoints: map[uuid.UUID]ConnectionEndpoint{}}
	now := time.Now().UTC()
	account := func(name string) uuid.UUID {
		id := uuid.New()
		_, err := db.Exec(`INSERT INTO users (id, created_at, updated_at, username, email, password_hash) VALUES (?, ?, ?, ?, ?, ?)`,
			id, now, now, name, name+"@example.com", "hash")
		require.NoError(t, err)
		return id
	}
	model := func(owner uuid.UUID, name string) *ent.Model {
		conn, err := client.ProviderConnection.Create().SetUserID(owner).SetPreset("ollama").SetName("Ollama").Save(ctx)
		require.NoError(t, err)
		f.owners[conn.ID] = owner
		m, err := client.Model.Create().SetName(name).SetDisplayName(name).SetDescription("").
			SetProvider("ollama").SetConnectionID(conn.ID).SetToolSupport(true).Save(ctx)
		require.NoError(t, err)
		return m
	}
	f.alice, f.bob = account("alice"), account("bob")
	f.aliceModel = model(f.alice, "qwen3:0.6b")
	f.bobModel = model(f.bob, "bob-private-model")
	f.catalog, err = client.Model.Create().SetName("gpt-5.1").SetDisplayName("GPT-5.1").SetDescription("").SetToolSupport(true).Save(ctx)
	require.NoError(t, err)

	f.a = newTestAgent(ds)
	f.a.connections = ConnectionResolverFunc(func(_ context.Context, userID, connectionID uuid.UUID) (ConnectionEndpoint, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.owners[connectionID] != userID {
			return ConnectionEndpoint{}, errors.New("no such provider connection")
		}
		return f.endpoints[connectionID], nil
	})
	return f
}

// serve points m's connection at ep.
func (f *connectionFixture) serve(m *ent.Model, ep ConnectionEndpoint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.endpoints[*m.ConnectionID] = ep
}

func TestResolveModelByReference_ConnectionModelsAreTheOwners(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx := context.Background()
	byName := modelResolverOptions{AllowDisplayName: true, AllowPrefixMatch: true, UserID: f.alice}

	got, err := f.a.resolveModelByReference(ctx, "qwen3:0.6b", byName)
	require.NoError(t, err)
	require.Equal(t, f.aliceModel.ID, got.ID, "the account's own connection model, by name")
	got, err = f.a.resolveModelByReference(ctx, f.aliceModel.ID.String(), byName)
	require.NoError(t, err)
	require.Equal(t, f.aliceModel.ID, got.ID, "and by ID")
	got, err = f.a.resolveModelByReference(ctx, "gpt-5.1", byName)
	require.NoError(t, err)
	require.Equal(t, f.catalog.ID, got.ID, "the catalog stays visible")

	_, err = f.a.resolveModelByReference(ctx, "bob-private-model", byName)
	require.Error(t, err, "another account's model is not found by name")
	_, err = f.a.resolveModelByReference(ctx, f.bobModel.ID.String(), byName)
	require.Error(t, err, "nor by ID")
	_, err = f.a.resolveModelByReference(ctx, f.aliceModel.ID.String(), modelResolverOptions{})
	require.Error(t, err, "without an account, no connection model is found")
}

func TestSubagentCapabilitiesFor_ConnectionModels(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx := context.Background()
	ep := ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: "http://localhost:11434/v1"}
	f.serve(f.aliceModel, ep)

	caps, err := f.a.subagentCapabilitiesFor(ctx, f.alice, f.aliceModel.ID, f.aliceModel.Name)
	require.NoError(t, err)
	require.Equal(t, &ep, caps.connection)
	require.Equal(t, "ollama", caps.Provider)
	require.True(t, caps.ToolSupport, "capabilities come from the model's row")

	caps, err = f.a.subagentCapabilitiesFor(ctx, f.alice, f.catalog.ID, f.catalog.Name)
	require.NoError(t, err)
	require.Nil(t, caps.connection, "a catalog model goes through its provider's own path")

	_, err = f.a.subagentCapabilitiesFor(ctx, f.alice, f.bobModel.ID, f.bobModel.Name)
	require.ErrorIs(t, err, ErrConnectionUnavailable, "another account's connection is refused")

	_, err = f.ent.Model.UpdateOneID(f.aliceModel.ID).SetDeleted(true).Save(ctx)
	require.NoError(t, err)
	_, err = f.a.subagentCapabilitiesFor(ctx, f.alice, f.aliceModel.ID, f.aliceModel.Name)
	require.ErrorIs(t, err, ErrModelDisabled, "a model turned off fails clearly")
}

// A subagent on an OpenAI-compatible connection model calls the connection's
// address with its key, and never the deployment's key from the environment.
func TestCallSubagent_ConnectionModelUsesItsConnection(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "deployment-env-key")
	f := newConnectionFixture(t)
	rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("hello from ollama"))
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "ollama", Flavor: ConnectionFlavorOpenAI, BaseURL: rec.srv.URL + "/v1", Key: "connection-key"})
	ctx := context.Background()

	caps, err := f.a.subagentCapabilitiesFor(ctx, f.alice, f.aliceModel.ID, f.aliceModel.Name)
	require.NoError(t, err)
	out, err := f.a.callSubagentModelWithCaps(ctx, f.alice, caps, f.aliceModel.Name, buildSubagentModelContext("", "", "hi"), nil, false)

	require.NoError(t, err)
	require.Equal(t, "hello from ollama", out.Output)
	require.Equal(t, []string{"/v1/chat/completions"}, rec.paths)
	require.Equal(t, "qwen3:0.6b", rec.bodies[0]["model"])
	require.Equal(t, "Bearer connection-key", rec.authHdrs[0])
}

func TestCallSubagent_ConnectionWithoutKeySendsNone(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "deployment-env-key")
	f := newConnectionFixture(t)
	rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("ok"))
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "lmstudio", Flavor: ConnectionFlavorOpenAI, BaseURL: rec.srv.URL + "/v1"})
	ctx := context.Background()

	caps, err := f.a.subagentCapabilitiesFor(ctx, f.alice, f.aliceModel.ID, f.aliceModel.Name)
	require.NoError(t, err)
	_, err = f.a.callSubagentModelWithCaps(ctx, f.alice, caps, f.aliceModel.Name, buildSubagentModelContext("", "", "hi"), nil, false)

	require.NoError(t, err)
	require.Equal(t, "", rec.authHdrs[0])
}

// An Anthropic-compatible connection goes through the Messages API at its own
// address, with its own key.
func TestCallSubagent_AnthropicConnectionUsesMessages(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "deployment-env-key")
	f := newConnectionFixture(t)
	var mu sync.Mutex
	var paths, keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		keys = append(keys, r.Header.Get("X-Api-Key"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(claudeMessageTextJSONBody("msg_c", "hello from custom")))
	}))
	t.Cleanup(srv.Close)
	f.serve(f.aliceModel, ConnectionEndpoint{Preset: "custom", Flavor: ConnectionFlavorAnthropic, BaseURL: srv.URL, Key: "connection-key"})
	ctx := context.Background()

	caps, err := f.a.subagentCapabilitiesFor(ctx, f.alice, f.aliceModel.ID, f.aliceModel.Name)
	require.NoError(t, err)
	out, err := f.a.callSubagentModelWithCaps(ctx, f.alice, caps, f.aliceModel.Name, buildSubagentModelContext("", "", "hi"), nil, false)

	require.NoError(t, err)
	require.Equal(t, "hello from custom", out.Output)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"/v1/messages"}, paths)
	require.Equal(t, []string{"connection-key"}, keys)
}

// A job's model is its own row, so a connection model keeps its connection; a
// model turned off fails the run at connectionFor; a model whose row is gone
// fails it here; a retired catalog model leaves the thread's model in place.
func TestJobModel(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx := context.Background()

	got, err := f.a.jobModel(ctx, f.aliceModel.ID)
	require.NoError(t, err)
	require.Equal(t, f.aliceModel.ID, got.ID)

	_, err = f.ent.Model.UpdateOneID(f.aliceModel.ID).SetDeleted(true).Save(ctx)
	require.NoError(t, err)
	got, err = f.a.jobModel(ctx, f.aliceModel.ID)
	require.NoError(t, err)
	require.True(t, got.Deleted, "a model turned off is returned as it is")
	_, err = f.a.connectionFor(ctx, f.alice, got)
	require.ErrorIs(t, err, ErrModelDisabled)

	_, err = f.a.jobModel(ctx, uuid.New())
	require.ErrorIs(t, err, ErrJobModelGone)

	_, err = f.ent.Model.UpdateOneID(f.catalog.ID).SetDeleted(true).Save(ctx)
	require.NoError(t, err)
	got, err = f.a.jobModel(ctx, f.catalog.ID)
	require.NoError(t, err)
	require.Nil(t, got, "a retired catalog model keeps the old behaviour")
}

// A chat on a connection model the account turned off keeps that model, so the
// turn fails clearly at connectionFor rather than running on something else.
func TestResolveModelForChat_TurnedOffConnectionModel(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx := context.Background()
	_, err := f.ent.Model.UpdateOneID(f.aliceModel.ID).SetDeleted(true).Save(ctx)
	require.NoError(t, err)

	got := f.a.resolveModelForChat(ctx, &models.Chat{ID: uuid.New(), UserID: f.alice, ModelID: f.aliceModel.ID, ModelName: f.aliceModel.Name})
	require.Equal(t, f.aliceModel.Name, got.name)
	require.NotNil(t, got.row)
	require.True(t, got.row.Deleted)
	_, err = f.a.connectionFor(ctx, f.alice, got.row)
	require.ErrorIs(t, err, ErrModelDisabled)
}

// A chat whose connection model went with its connection keeps only the name,
// which no catalog knows: it is not sent to a provider by that name.
func TestResolveModelForChat_ModelGoneWithItsConnection(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx := context.Background()

	got := f.a.resolveModelForChat(ctx, &models.Chat{ID: uuid.New(), UserID: f.alice, ModelName: "qwen3:4b-gone"})
	require.NotEqual(t, "qwen3:4b-gone", got.name, "the name alone is not routed anywhere")
	require.Nil(t, got.row)
}

// A datastore failure is not a missing model: the sub-agent says so instead of
// running on what a missing model would get.
func TestSubagentCapabilitiesFor_DatastoreErrorsAreNotMissingModels(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	_, err := f.db.Exec(`DROP TABLE models`)
	require.NoError(t, err)

	_, err = f.a.subagentCapabilitiesFor(context.Background(), f.alice, f.aliceModel.ID, f.aliceModel.Name)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrConnectionUnavailable)
}

// A chat with no model runs on the account's default; an account with none
// (on an install without a catalog, before it enables a model) has nothing to
// run on, and the turn fails with no_model_selected (ADR 0x027).
func TestResolveModelForChat_AccountDefault(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx := context.Background()
	_, err := f.ent.UserPreference.Create().SetUserID(f.alice).SetDefaultModel(f.aliceModel.ID).Save(ctx)
	require.NoError(t, err)
	_, err = f.ent.UserPreference.Create().SetUserID(f.bob).Save(ctx)
	require.NoError(t, err)

	got := f.a.resolveModelForChat(ctx, &models.Chat{ID: uuid.New(), UserID: f.alice})
	require.Equal(t, f.aliceModel.Name, got.name)
	require.Equal(t, f.aliceModel.ID, got.row.ID, "the row, so the turn goes through the model's connection")

	got = f.a.resolveModelForChat(ctx, &models.Chat{ID: uuid.New(), UserID: f.bob, ModelID: uuid.New()})
	require.Empty(t, got.name, "no default and no deployment default: no model")
	require.Equal(t, models.ErrCodeNoModelSelected, failureCode(ErrNoModelSelected))
}
