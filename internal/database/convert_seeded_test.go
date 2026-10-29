package database

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
	"github.com/theimaginaryfoundation/what-iff/ent/model"
)

// newConversionClient is an ent client over in-memory SQLite with the tables
// the conversion reads and writes, from the ent schema. Their foreign keys
// reach most of the schema, which needs pgvector, so they are left out: the
// conversion clears what it does not convert itself.
func newConversionClient(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB(dialect.SQLite, db)
	m, err := schema.NewMigrate(drv, schema.WithForeignKeys(false))
	require.NoError(t, err)
	var tables []*schema.Table
	for _, table := range []*schema.Table{migrate.UsersTable, migrate.ProviderConnectionsTable, migrate.ModelsTable,
		migrate.UserPreferencesTable, migrate.ChatsTable, migrate.AgentJobsTable} {
		tables = append(tables, &schema.Table{Name: table.Name, Columns: table.Columns, PrimaryKey: table.PrimaryKey, Indexes: table.Indexes})
	}
	require.NoError(t, m.Create(context.Background(), tables...))
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestConvertSeededModels(t *testing.T) {
	ctx := context.Background()
	client := newConversionClient(t)

	seeded := func(name, provider string) *ent.Model {
		m, err := client.Model.Create().SetName(name).SetDisplayName(name + " display").SetDescription("seeded").
			SetProvider(model.Provider(provider)).SetToolSupport(true).SetVisionSupport(true).Save(ctx)
		require.NoError(t, err)
		return m
	}
	gpt := seeded("gpt-5.6-luna", "openai")
	claude := seeded("claude-sonnet-4-6", "anthropic")
	unused := seeded("gpt-5.1", "openai")
	admins, err := client.Model.Create().SetName("house-model").SetDisplayName("House").SetDescription("").Save(ctx)
	require.NoError(t, err, "a model the deployment added itself, not from the seed list")

	account := func(name string, defaultModel *ent.Model, favorites ...string) *ent.User {
		u, err := client.User.Create().SetUsername(name).SetEmail(name + "@example.test").SetPasswordHash("x").Save(ctx)
		require.NoError(t, err)
		_, err = client.UserPreference.Create().SetUserID(u.ID).SetModelID(defaultModel.ID).SetFavoriteModelIds(favorites).Save(ctx)
		require.NoError(t, err)
		return u
	}
	builtin := func(u *ent.User, provider string) *ent.ProviderConnection {
		c, err := client.ProviderConnection.Create().SetUserID(u.ID).SetPreset(provider).SetBuiltin(provider).SetName(provider).Save(ctx)
		require.NoError(t, err)
		return c
	}
	chatOn := func(u *ent.User, m *ent.Model) *ent.Chat {
		c, err := client.Chat.Create().SetName("c").SetOwnerID(u.ID).SetModelID(m.ID).Save(ctx)
		require.NoError(t, err)
		return c
	}
	jobOn := func(u *ent.User, m *ent.Model) *ent.AgentJob {
		j, err := client.AgentJob.Create().SetPrompt("p").SetScheduleInput("daily").SetScheduleType("cron").SetOwnerID(u.ID).SetModelID(m.ID).Save(ctx)
		require.NoError(t, err)
		return j
	}

	// Alice has OpenAI and Anthropic keys, and already enabled gpt-5.6-luna herself (turned off since).
	alice := account("alice", gpt, gpt.ID.String(), claude.ID.String(), admins.ID.String())
	aliceOpenAI, aliceAnthropic := builtin(alice, "openai"), builtin(alice, "anthropic")
	already, err := client.Model.Create().SetName("gpt-5.6-luna").SetDisplayName("mine").SetDescription("").
		SetProvider("openai").SetConnectionID(aliceOpenAI.ID).SetDeleted(true).Save(ctx)
	require.NoError(t, err)
	aliceChat, aliceClaudeChat, aliceJob := chatOn(alice, gpt), chatOn(alice, claude), jobOn(alice, claude)
	// Bob has only an Anthropic key: his default and a chat are on the OpenAI model.
	bob := account("bob", gpt, gpt.ID.String())
	bobAnthropic := builtin(bob, "anthropic")
	bobGPTChat, bobClaudeChat, bobGPTJob := chatOn(bob, gpt), chatOn(bob, claude), jobOn(bob, gpt)

	n, err := ConvertSeededModels(ctx, client, zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// Seeded rows are gone; the deployment's own model stays.
	catalog, err := client.Model.Query().Where(model.ConnectionIDIsNil()).All(ctx)
	require.NoError(t, err)
	require.Len(t, catalog, 1)
	require.Equal(t, admins.ID, catalog[0].ID)
	require.Zero(t, client.Model.Query().Where(model.ID(unused.ID)).CountX(ctx))

	on := func(conn *ent.ProviderConnection, name string) *ent.Model {
		m, err := client.Model.Query().Where(model.ConnectionID(conn.ID), model.Name(name)).Only(ctx)
		require.NoError(t, err)
		return m
	}
	aliceGPT, aliceClaude, bobClaude := on(aliceOpenAI, "gpt-5.6-luna"), on(aliceAnthropic, "claude-sonnet-4-6"), on(bobAnthropic, "claude-sonnet-4-6")
	require.Equal(t, already.ID, aliceGPT.ID, "a model the account had already is reused")
	require.False(t, aliceGPT.Deleted, "and enabled again, since the account uses it")
	require.Equal(t, "claude-sonnet-4-6 display", aliceClaude.DisplayName, "converted models keep the seeded name")
	require.True(t, aliceClaude.ToolSupport)
	require.True(t, *aliceClaude.VisionSupport, "and capabilities")
	require.Equal(t, model.ProviderAnthropic, aliceClaude.Provider)
	require.Zero(t, client.Model.Query().Where(model.ConnectionID(bobAnthropic.ID), model.Name("gpt-5.6-luna")).CountX(ctx),
		"nothing is made on a connection for another provider")

	prefs := func(u *ent.User) *ent.UserPreference {
		return client.User.QueryPreferences(u).OnlyX(ctx)
	}
	chatModel := func(c *ent.Chat) *uuid.UUID {
		m, err := client.Chat.QueryModel(c).Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		require.NoError(t, err)
		return &m.ID
	}
	jobModel := func(j *ent.AgentJob) *uuid.UUID { return client.AgentJob.GetX(ctx, j.ID).ModelID }

	// Alice: everything moved onto her connections; her favourites follow, and
	// the deployment's own model stays among them.
	require.Equal(t, aliceGPT.ID, *prefs(alice).DefaultModel)
	require.Equal(t, []string{aliceGPT.ID.String(), aliceClaude.ID.String(), admins.ID.String()}, prefs(alice).FavoriteModelIds)
	require.Equal(t, aliceGPT.ID, *chatModel(aliceChat))
	require.Equal(t, aliceClaude.ID, *chatModel(aliceClaudeChat))
	require.Equal(t, aliceClaude.ID, *jobModel(aliceJob))

	// Bob: what he has a connection for moves; the rest is cleared, and his job
	// keeps naming a model that no longer exists, so it fails clearly.
	require.Nil(t, prefs(bob).DefaultModel, "no OpenAI connection: no default")
	require.Empty(t, prefs(bob).FavoriteModelIds)
	require.Nil(t, chatModel(bobGPTChat), "the chat falls back to the account's default")
	require.Equal(t, bobClaude.ID, *chatModel(bobClaudeChat))
	require.Equal(t, gpt.ID, *jobModel(bobGPTJob))

	// Running again finds nothing to do.
	n, err = ConvertSeededModels(ctx, client, zap.NewNop())
	require.NoError(t, err)
	require.Zero(t, n)
}
