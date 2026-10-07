package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestMCPLifecycleTools_LoadAndUnloadFlow(t *testing.T) {
	ctx := context.Background()
	srv := newTestMCPRPCServer()
	defer srv.Close()

	agent, chatCtx, mcpServerID, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()

	loadArgs := map[string]any{
		"mcp_server_id": mcpServerID.String(),
		"tools":         []string{"search", "missing"},
	}
	loadRaw, _ := json.Marshal(loadArgs)
	out, err := agent.loadMCPToolsTool(ctx, chatCtx, loadRaw)
	require.NoError(t, err)
	var loadRes mcpToolLifecycleResult
	require.NoError(t, json.Unmarshal([]byte(out), &loadRes))
	require.Equal(t, mcpServerID.String(), loadRes.ServerID)
	require.Len(t, loadRes.Loaded, 1)
	require.Contains(t, loadRes.Loaded[0], "mcp__")
	require.Contains(t, loadRes.Note, "Ignored unknown tool names")

	// Unload specific tool then verify remaining set updates.
	unloadArgs := map[string]any{
		"mcp_server_id": mcpServerID.String(),
		"tools":         []string{loadRes.Loaded[0], "mcp__nope__missing"},
	}
	unloadRaw, _ := json.Marshal(unloadArgs)
	out, err = agent.unloadMCPToolsTool(ctx, chatCtx, unloadRaw)
	require.NoError(t, err)
	var unloadRes mcpToolLifecycleResult
	require.NoError(t, json.Unmarshal([]byte(out), &unloadRes))
	require.Equal(t, []string{loadRes.Loaded[0]}, unloadRes.Unloaded)
	require.Equal(t, []string{"mcp__nope__missing"}, unloadRes.NotLoaded)
	require.Empty(t, unloadRes.Remaining)
}

func TestMCPLifecycleTools_UnloadAllWithoutServerID(t *testing.T) {
	ctx := context.Background()
	srv := newTestMCPRPCServer()
	defer srv.Close()

	agent, chatCtx, mcpServerID, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()

	loadAll, _ := json.Marshal(map[string]any{
		"mcp_server_id": mcpServerID.String(),
		"tools":         []string{"all"},
	})
	_, err := agent.loadMCPToolsTool(ctx, chatCtx, loadAll)
	require.NoError(t, err)

	out, err := agent.unloadMCPToolsTool(ctx, chatCtx, []byte(`{"tools":["*"]}`))
	require.NoError(t, err)
	var res mcpToolLifecycleResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Contains(t, res.Note, "Unloaded all MCP tools")
}

func TestLoadMCPToolsTool_ServerNotConnected(t *testing.T) {
	ctx := context.Background()
	srv := newTestMCPRPCServer()
	defer srv.Close()

	agent, chatCtx, _, cleanup := newMCPLifecycleAgentFixture(t, srv.URL)
	defer cleanup()

	raw, _ := json.Marshal(map[string]any{
		"mcp_server_id": uuid.NewString(),
		"tools":         []string{"search"},
	})
	out, err := agent.loadMCPToolsTool(ctx, chatCtx, raw)
	require.NoError(t, err)
	require.Contains(t, out, "not connected to this chat")
}

func newMCPLifecycleAgentFixture(t *testing.T, serverURL string) (*Agent, *chatContext, uuid.UUID, func()) {
	t.Helper()
	ctx := context.Background()

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&_fk=1", uuid.NewString()))
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	for _, stmt := range []string{
		`CREATE TABLE users (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			username text NOT NULL UNIQUE,
			email text NOT NULL UNIQUE,
			password_hash text NOT NULL,
			cognito_sub text UNIQUE,
			first_name text,
			last_name text,
			timezone text NOT NULL DEFAULT 'America/New_York',
			status text NOT NULL DEFAULT 'active',
			enable_experimental_models bool NOT NULL DEFAULT false,
			last_login datetime,
			last_seen datetime,
			terms_accepted_at datetime,
			refresh_token_id text,
			release_notes_seen_at datetime,
			early_access bool NOT NULL DEFAULT false
		)`,
		`CREATE TABLE chats (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			response_id text,
			checkpoint_summary text,
			checkpoint_user_message_count integer NOT NULL DEFAULT 0,
			last_message_time datetime,
			last_checkpoint_at datetime,
			checkpoint_started_at datetime,
			disabled_tools json,
			tags json,
			is_favorite bool NOT NULL DEFAULT false,
			is_auto_mood bool NOT NULL DEFAULT true,
			archived bool NOT NULL DEFAULT false,
			chat_model uuid,
			chat_personality uuid,
			chat_active_mood uuid,
			source text,
			import_hash text,
			rehydration_state text,
			context_scope text NOT NULL DEFAULT 'account',
			user_chats uuid NOT NULL
		)`,
		`CREATE TABLE mcp_servers (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			description text NOT NULL,
			server_url text NOT NULL,
			auth_mode text NOT NULL DEFAULT 'header',
			auth_token text,
			oauth_auth_url text,
			oauth_token_url text,
			oauth_client_id text,
			oauth_client_secret text,
			oauth_scopes json,
			oauth_pkce_policy text NOT NULL DEFAULT 'supported',
			oauth_access_token text,
			oauth_refresh_token text,
			oauth_access_token_expires_at datetime,
			oauth_refresh_token_expires_at datetime,
			oauth_authenticated_at datetime,
			oauth_last_refresh_at datetime,
			oauth_refresh_fail_count integer NOT NULL DEFAULT 0,
			status text NOT NULL DEFAULT 'active',
			status_reason text NOT NULL DEFAULT '',
			last_checked_at datetime,
			last_healthy_at datetime,
			tool_count integer NOT NULL DEFAULT 0,
			default_enabled bool NOT NULL DEFAULT false,
			user_mcp_servers uuid NOT NULL
		)`,
		`CREATE TABLE chat_mcp_servers (
			chat_id uuid NOT NULL,
			mcp_server_id uuid NOT NULL,
			PRIMARY KEY (chat_id, mcp_server_id)
		)`,
		`CREATE TABLE rituals (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			description text NOT NULL,
			content text NOT NULL,
			hotkeys text NOT NULL,
			agent_job_rituals uuid,
			mood_rituals uuid,
			ritual_personality uuid,
			user_rituals uuid NOT NULL
		)`,
		`CREATE TABLE ritual_mcp_servers (
			ritual_id uuid NOT NULL,
			mcp_server_id uuid NOT NULL,
			PRIMARY KEY (ritual_id, mcp_server_id)
		)`,
		`CREATE TABLE chat_mcp_tool_states (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			chat_id uuid NOT NULL,
			mcp_server_id uuid NOT NULL,
			loaded_tools json,
			UNIQUE(chat_id, mcp_server_id)
		)`,
	} {
		_, err = db.Exec(stmt)
		require.NoError(t, err)
	}

	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	userID := uuid.New()
	chatID := uuid.New()
	now := time.Now().UTC()
	_, err = db.Exec(`INSERT INTO users (id, created_at, updated_at, username, email, password_hash) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, now, now, "mcp-user", "mcp-user@example.com", "hash")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chats (id, created_at, updated_at, name, user_chats) VALUES (?, ?, ?, ?, ?)`,
		chatID, now, now, "test chat", userID)
	require.NoError(t, err)

	created, err := ds.CreateMCPServer(ctx, userID, models.MCPServer{
		Name:        "tracker",
		Description: "test mcp",
		ServerURL:   serverURL,
	})
	require.NoError(t, err)
	require.NoError(t, ds.AddMCPServerToChat(ctx, userID, chatID, created.ID))

	agent := &Agent{
		ds:        ds,
		logger:    zap.NewNop(),
		mcpClient: mcpclient.New(nil, nil),
	}
	chatCtx := &chatContext{
		userID: userID,
		chat:   &models.Chat{ID: chatID, UserID: userID},
	}
	return agent, chatCtx, created.ID, func() {
		_ = client.Close()
		_ = db.Close()
	}
}

func newTestMCPRPCServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		switch method {
		case "initialize", "notifications/initialized":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "search",
							"description": "Search issues",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
						},
						{
							"name":        "get",
							"description": "Get issue",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}
