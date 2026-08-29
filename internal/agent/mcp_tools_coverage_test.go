package agent

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

// --- claudeFunctionTools / openAIChatCompletionFunctionTools / geminiFunctionTools ---

func TestClaudeFunctionTools_BuildsOneToolPerSpec(t *testing.T) {
	t.Parallel()
	specs := []agenttools.FunctionToolSpec{
		{Name: "web_search", Description: "search the web", Properties: map[string]any{"query": map[string]any{"type": "string"}}, Required: []string{"query"}},
	}
	out := claudeFunctionTools(specs)
	require.Len(t, out, 1)
}

func TestClaudeFunctionTools_EmptyInputReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	out := claudeFunctionTools(nil)
	require.Empty(t, out)
}

func TestOpenAIChatCompletionFunctionTools_BuildsOneToolPerSpec(t *testing.T) {
	t.Parallel()
	specs := []agenttools.FunctionToolSpec{
		{Name: "recall", Description: "recall a memory", Properties: map[string]any{"id": map[string]any{"type": "string"}}, Required: []string{"id"}},
		{Name: "list", Description: "list things"},
	}
	out := openAIChatCompletionFunctionTools(specs)
	require.Len(t, out, 2)
}

func TestGeminiFunctionTools_DelegatesToOpenAIChatCompletionFunctionTools(t *testing.T) {
	t.Parallel()
	specs := []agenttools.FunctionToolSpec{
		{Name: "web_search", Description: "search the web"},
	}
	out := geminiFunctionTools(specs)
	require.Len(t, out, 1)
}

// --- getSubagentMCPTools / getSubagentClaudeMCPConfig ---

func TestGetSubagentMCPTools_NoRitualIDsReturnsNilWithoutDsCall(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.Nil(t, a.getSubagentMCPTools(context.Background(), uuid.New(), nil, "gpt-5.4"))
}

func TestGetSubagentMCPTools_ListRitualMCPServersErrorReturnsNil(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	got := a.getSubagentMCPTools(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}, "gpt-5.4")
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetSubagentMCPTools_EmptyServerListReturnsEmptyTools(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"id"}))

	a := newTestAgent(ds)
	got := a.getSubagentMCPTools(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}, "gpt-5.4")
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetSubagentClaudeMCPConfig_NoRitualIDsReturnsNilWithoutDsCall(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.Nil(t, a.getSubagentClaudeMCPConfig(context.Background(), uuid.New(), nil))
}

func TestGetSubagentClaudeMCPConfig_ListRitualMCPServersErrorReturnsNil(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	got := a.getSubagentClaudeMCPConfig(context.Background(), uuid.New(), []uuid.UUID{uuid.New()})
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- getChatMCPServers ---

func TestGetChatMCPServers_ChatServersLoadFailsReturnsNil(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	got := a.getChatMCPServers(context.Background(), uuid.New(), uuid.New(), nil)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetChatMCPServers_NoRitualIDsReturnsChatServersOnly(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New())) // chat exists
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"id"}))                    // no mcp servers
	mock.ExpectCommit()

	a := newTestAgent(ds)
	got := a.getChatMCPServers(context.Background(), uuid.New(), uuid.New(), nil)
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetChatMCPServers_RitualServersLoadFailureIsLoggedAndIgnored(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New())) // chat exists
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"id"}))                    // no chat mcp servers
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel) // ritual servers query fails

	a := newTestAgent(ds)
	got := a.getChatMCPServers(context.Background(), uuid.New(), uuid.New(), []uuid.UUID{uuid.New()})
	require.Empty(t, got, "ritual server load failure falls back to the (empty) chat server list")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- getChatMCPTools / getChatClaudeMCPConfig ---

func TestGetChatMCPTools_ChatServersLoadFailureYieldsNoTools(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	got := a.getChatMCPTools(context.Background(), uuid.New(), uuid.New(), nil, "gpt-5.4")
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetChatClaudeMCPConfig_ChatServersLoadFailureYieldsNilConfig(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	got := a.getChatClaudeMCPConfig(context.Background(), uuid.New(), uuid.New(), nil)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
