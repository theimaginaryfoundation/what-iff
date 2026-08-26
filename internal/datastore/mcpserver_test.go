package datastore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createMCPServerTestSchema creates the mcp_servers table plus the chat_mcp_servers and
// ritual_mcp_servers join tables (ent/migrate/schema.go's McpServersTable,
// ChatMcpServersTable, RitualMcpServersTable). None of the existing schema-creation funcs
// provide these. Must be composed after createMemoryImportTestSchema (users/chats) and
// createAccountBackupTestSchema (rituals) via
// newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createMCPServerTestSchema).
func createMCPServerTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	statements := []string{
		`CREATE TABLE mcp_servers (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			description text NOT NULL,
			server_url text NOT NULL,
			auth_token text,
			default_enabled bool NOT NULL DEFAULT false,
			user_mcp_servers uuid NOT NULL
		)`,
		`CREATE TABLE chat_mcp_servers (
			chat_id uuid NOT NULL,
			mcp_server_id uuid NOT NULL,
			PRIMARY KEY (chat_id, mcp_server_id)
		)`,
		`CREATE TABLE ritual_mcp_servers (
			ritual_id uuid NOT NULL,
			mcp_server_id uuid NOT NULL,
			PRIMARY KEY (ritual_id, mcp_server_id)
		)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newMCPServerTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, createMCPServerTestSchema, alterChatsTableForAgentJobTests)
}

func createMCPServerTestUser(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(id).
		SetUsername("mcp-" + id.String()[:8]).
		SetEmail("mcp-" + id.String()[:8] + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
	return id
}

func createMCPServerTestModel(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	m, err := ds.dbClient.Model.Create().
		SetName("model-" + uuid.NewString()[:8]).
		SetDisplayName("Test Model").
		SetDescription("test model").
		Save(context.Background())
	require.NoError(t, err)
	return m.ID
}

func createMCPServerTestChat(t *testing.T, ds *Datastore, userID, modelID uuid.UUID) uuid.UUID {
	t.Helper()
	c, err := ds.dbClient.Chat.Create().
		SetName("Chat").
		SetOwnerID(userID).
		SetModelID(modelID).
		Save(context.Background())
	require.NoError(t, err)
	return c.ID
}

func createMCPServerTestRitual(t *testing.T, ds *Datastore, userID uuid.UUID) uuid.UUID {
	t.Helper()
	r, err := ds.dbClient.Ritual.Create().
		SetOwnerID(userID).
		SetName("Morning").
		SetDescription("desc").
		SetContent("content").
		SetHotkeys("").
		Save(context.Background())
	require.NoError(t, err)
	return r.ID
}

func baseMCPServerModel() models.MCPServer {
	return models.MCPServer{
		Name:        "My MCP Server",
		Description: "test server",
		ServerURL:   "https://example.com/mcp",
	}
}

func TestCreateMCPServer_Success(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)

	got, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "My MCP Server", got.Name)
	require.Equal(t, userID, got.UserID)
	require.Empty(t, got.ErrorMessage)
}

func TestCreateMCPServer_WithAuthToken_RoundTrips(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)

	server := baseMCPServerModel()
	server.AuthToken = "super-secret-token"

	got, err := ds.CreateMCPServer(ctx, userID, server)
	require.NoError(t, err)
	require.Equal(t, "super-secret-token", got.AuthToken)
}

func TestCreateMCPServer_UserNotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := ds.CreateMCPServer(ctx, uuid.New(), baseMCPServerModel())
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestGetMCPServer_Success(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	got, err := ds.GetMCPServer(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "My MCP Server", got.Name)
}

func TestGetMCPServer_NotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)

	_, err := ds.GetMCPServer(ctx, userID, uuid.New())
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}

func TestGetMCPServer_WrongOwner(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createMCPServerTestUser(t, ds)
	otherID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, ownerID, baseMCPServerModel())
	require.NoError(t, err)

	_, err = ds.GetMCPServer(ctx, otherID, created.ID)
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}

func TestListMCPServers_FiltersAndPagination(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	otherID := createMCPServerTestUser(t, ds)

	for _, name := range []string{"Alpha Server", "Beta Server", "Gamma Tool"} {
		s := baseMCPServerModel()
		s.Name = name
		s.Description = "a distinct description"
		_, err := ds.CreateMCPServer(ctx, userID, s)
		require.NoError(t, err)
	}
	// A server owned by someone else should never show up.
	_, err := ds.CreateMCPServer(ctx, otherID, baseMCPServerModel())
	require.NoError(t, err)

	resp, err := ds.ListMCPServers(ctx, userID, 1, 10, models.MCPServerFilters{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.TotalCount)
	require.Len(t, resp.Results, 3)

	query := "Server"
	resp, err = ds.ListMCPServers(ctx, userID, 1, 10, models.MCPServerFilters{Query: &query})
	require.NoError(t, err)
	require.Equal(t, 2, resp.TotalCount)

	resp, err = ds.ListMCPServers(ctx, userID, 1, 2, models.MCPServerFilters{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.TotalCount)
	require.Len(t, resp.Results, 2)
	require.Equal(t, 1, resp.Page)

	resp, err = ds.ListMCPServers(ctx, userID, 0, 0, models.MCPServerFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Page)
	require.Len(t, resp.Results, 3)
}

func TestUpdateMCPServer_Success(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	updated := *created
	updated.Name = "Renamed Server"
	updated.DefaultEnabled = true

	got, err := ds.UpdateMCPServer(ctx, userID, updated, models.MCPServerAuthTokenUpdate{}, nil)
	require.NoError(t, err)
	require.Equal(t, "Renamed Server", got.Name)
	require.True(t, got.DefaultEnabled)
}

func TestUpdateMCPServer_NotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	server := baseMCPServerModel()
	server.ID = uuid.New()

	_, err := ds.UpdateMCPServer(ctx, userID, server, models.MCPServerAuthTokenUpdate{}, nil)
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}

func TestUpdateMCPServer_WrongOwner(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createMCPServerTestUser(t, ds)
	otherID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, ownerID, baseMCPServerModel())
	require.NoError(t, err)

	_, err = ds.UpdateMCPServer(ctx, otherID, *created, models.MCPServerAuthTokenUpdate{}, nil)
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}

func TestUpdateMCPServer_AuthTokenSetAndClear(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)
	require.Empty(t, created.AuthToken)

	got, err := ds.UpdateMCPServer(ctx, userID, *created, models.MCPServerAuthTokenUpdate{
		Provided: true,
		Value:    "new-token",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "new-token", got.AuthToken)

	got, err = ds.UpdateMCPServer(ctx, userID, *created, models.MCPServerAuthTokenUpdate{
		Provided: true,
		Clear:    true,
	}, nil)
	require.NoError(t, err)
	require.Empty(t, got.AuthToken)
}

func TestUpdateMCPServer_RitualIDsUpdate(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	ritual1 := createMCPServerTestRitual(t, ds, userID)
	ritual2 := createMCPServerTestRitual(t, ds, userID)

	ritualIDs := []uuid.UUID{ritual1, ritual2}
	got, err := ds.UpdateMCPServer(ctx, userID, *created, models.MCPServerAuthTokenUpdate{}, &ritualIDs)
	require.NoError(t, err)
	require.Len(t, got.RitualIDs, 2)

	// Replacing with an empty slice clears the links.
	empty := []uuid.UUID{}
	got, err = ds.UpdateMCPServer(ctx, userID, *created, models.MCPServerAuthTokenUpdate{}, &empty)
	require.NoError(t, err)
	require.Empty(t, got.RitualIDs)

	// A ritual owned by someone else is rejected.
	otherUserID := createMCPServerTestUser(t, ds)
	otherRitual := createMCPServerTestRitual(t, ds, otherUserID)
	badIDs := []uuid.UUID{otherRitual}
	_, err = ds.UpdateMCPServer(ctx, userID, *created, models.MCPServerAuthTokenUpdate{}, &badIDs)
	require.ErrorIs(t, err, ErrInvalidRequestBody)

	// Nil means don't touch ritual links: re-link, then confirm a nil update preserves them.
	ritualIDs = []uuid.UUID{ritual1}
	got, err = ds.UpdateMCPServer(ctx, userID, *created, models.MCPServerAuthTokenUpdate{}, &ritualIDs)
	require.NoError(t, err)
	require.Len(t, got.RitualIDs, 1)

	got, err = ds.UpdateMCPServer(ctx, userID, *created, models.MCPServerAuthTokenUpdate{}, nil)
	require.NoError(t, err)
	require.Len(t, got.RitualIDs, 1)
}

func TestValidateUserOwnsMCPServerIDs(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	otherUserID := createMCPServerTestUser(t, ds)

	owned, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)
	notOwned, err := ds.CreateMCPServer(ctx, otherUserID, baseMCPServerModel())
	require.NoError(t, err)

	tx, err := ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()

	// Empty input is always valid.
	require.NoError(t, ds.validateUserOwnsMCPServerIDs(ctx, tx, userID, nil))

	// Owned IDs (including duplicates) are valid.
	require.NoError(t, ds.validateUserOwnsMCPServerIDs(ctx, tx, userID, []uuid.UUID{owned.ID, owned.ID}))

	// An ID owned by someone else is rejected.
	err = ds.validateUserOwnsMCPServerIDs(ctx, tx, userID, []uuid.UUID{owned.ID, notOwned.ID})
	require.ErrorIs(t, err, ErrInvalidRequestBody)

	// A nonexistent ID is rejected too.
	err = ds.validateUserOwnsMCPServerIDs(ctx, tx, userID, []uuid.UUID{uuid.New()})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestDeleteMCPServer_Success(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	err = ds.DeleteMCPServer(ctx, userID, created.ID)
	require.NoError(t, err)

	_, err = ds.GetMCPServer(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}

func TestDeleteMCPServer_NotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)

	err := ds.DeleteMCPServer(ctx, userID, uuid.New())
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}

func TestDeleteMCPServer_WrongOwner(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	ownerID := createMCPServerTestUser(t, ds)
	otherID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, ownerID, baseMCPServerModel())
	require.NoError(t, err)

	err = ds.DeleteMCPServer(ctx, otherID, created.ID)
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}

func TestListDefaultEnabledMCPServers(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)

	enabled := baseMCPServerModel()
	enabled.Name = "Enabled"
	enabled.DefaultEnabled = true
	_, err := ds.CreateMCPServer(ctx, userID, enabled)
	require.NoError(t, err)

	disabled := baseMCPServerModel()
	disabled.Name = "Disabled"
	disabled.DefaultEnabled = false
	_, err = ds.CreateMCPServer(ctx, userID, disabled)
	require.NoError(t, err)

	// A default-enabled server owned by someone else must not leak in.
	otherUserID := createMCPServerTestUser(t, ds)
	otherEnabled := baseMCPServerModel()
	otherEnabled.DefaultEnabled = true
	_, err = ds.CreateMCPServer(ctx, otherUserID, otherEnabled)
	require.NoError(t, err)

	servers, err := ds.ListDefaultEnabledMCPServers(ctx, userID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, "Enabled", servers[0].Name)
}

func TestListChatMCPServers(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	modelID := createMCPServerTestModel(t, ds)
	chatID := createMCPServerTestChat(t, ds, userID, modelID)

	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	require.NoError(t, ds.AddMCPServerToChat(ctx, userID, chatID, created.ID))

	servers, err := ds.ListChatMCPServers(ctx, userID, chatID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, created.ID, servers[0].ID)
}

func TestListChatMCPServers_ChatNotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)

	_, err := ds.ListChatMCPServers(ctx, userID, uuid.New())
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestListRitualMCPServers(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	ritual1 := createMCPServerTestRitual(t, ds, userID)
	ritual2 := createMCPServerTestRitual(t, ds, userID)

	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	ritualIDs := []uuid.UUID{ritual1}
	_, err = ds.UpdateMCPServer(ctx, userID, *created, models.MCPServerAuthTokenUpdate{}, &ritualIDs)
	require.NoError(t, err)

	servers, err := ds.ListRitualMCPServers(ctx, userID, []uuid.UUID{ritual1, ritual2})
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, created.ID, servers[0].ID)
}

func TestListRitualMCPServers_EmptyInput(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)

	servers, err := ds.ListRitualMCPServers(ctx, userID, nil)
	require.NoError(t, err)
	require.Empty(t, servers)
}

func TestListAvailableChatMCPServers(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	modelID := createMCPServerTestModel(t, ds)
	chatID := createMCPServerTestChat(t, ds, userID, modelID)

	attached := baseMCPServerModel()
	attached.Name = "Attached"
	attachedSrv, err := ds.CreateMCPServer(ctx, userID, attached)
	require.NoError(t, err)
	require.NoError(t, ds.AddMCPServerToChat(ctx, userID, chatID, attachedSrv.ID))

	available := baseMCPServerModel()
	available.Name = "Available"
	_, err = ds.CreateMCPServer(ctx, userID, available)
	require.NoError(t, err)

	resp, err := ds.ListAvailableChatMCPServers(ctx, userID, chatID, 1, 10, models.MCPServerFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Len(t, resp.Results, 1)
	got, ok := resp.Results[0].(*models.MCPServer)
	require.True(t, ok)
	require.Equal(t, "Available", got.Name)
}

func TestListAvailableChatMCPServers_ChatNotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)

	_, err := ds.ListAvailableChatMCPServers(ctx, userID, uuid.New(), 1, 10, models.MCPServerFilters{})
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestAddMCPServerToChat_Success(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	modelID := createMCPServerTestModel(t, ds)
	chatID := createMCPServerTestChat(t, ds, userID, modelID)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	err = ds.AddMCPServerToChat(ctx, userID, chatID, created.ID)
	require.NoError(t, err)

	servers, err := ds.ListChatMCPServers(ctx, userID, chatID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
}

func TestAddMCPServerToChat_ChatNotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	err = ds.AddMCPServerToChat(ctx, userID, uuid.New(), created.ID)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestAddMCPServerToChat_ServerNotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	modelID := createMCPServerTestModel(t, ds)
	chatID := createMCPServerTestChat(t, ds, userID, modelID)

	err := ds.AddMCPServerToChat(ctx, userID, chatID, uuid.New())
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}

func TestRemoveMCPServerFromChat_Success(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	modelID := createMCPServerTestModel(t, ds)
	chatID := createMCPServerTestChat(t, ds, userID, modelID)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)
	require.NoError(t, ds.AddMCPServerToChat(ctx, userID, chatID, created.ID))

	err = ds.RemoveMCPServerFromChat(ctx, userID, chatID, created.ID)
	require.NoError(t, err)

	servers, err := ds.ListChatMCPServers(ctx, userID, chatID)
	require.NoError(t, err)
	require.Empty(t, servers)
}

func TestRemoveMCPServerFromChat_ChatNotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	created, err := ds.CreateMCPServer(ctx, userID, baseMCPServerModel())
	require.NoError(t, err)

	err = ds.RemoveMCPServerFromChat(ctx, userID, uuid.New(), created.ID)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestRemoveMCPServerFromChat_ServerNotFound(t *testing.T) {
	ds, cleanup := newMCPServerTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createMCPServerTestUser(t, ds)
	modelID := createMCPServerTestModel(t, ds)
	chatID := createMCPServerTestChat(t, ds, userID, modelID)

	err := ds.RemoveMCPServerFromChat(ctx, userID, chatID, uuid.New())
	require.ErrorIs(t, err, ErrMCPServerNotFound)
}
