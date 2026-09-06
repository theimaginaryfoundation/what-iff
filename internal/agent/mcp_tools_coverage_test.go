package agent

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

func TestClaudeFunctionTools_BuildsOneToolPerSpec(t *testing.T) {
	t.Parallel()
	specs := []agenttools.FunctionToolSpec{
		{Name: "web_search", Description: "search the web", Properties: map[string]any{"query": map[string]any{"type": "string"}}, Required: []string{"query"}},
	}
	out := claudeFunctionTools(specs)
	require.Len(t, out, 1)
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
	a.mcpClient = mcpclient.New(nil, nil)
	got := a.getSubagentMCPTools(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}, "gpt-5.4")
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

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
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	a := newTestAgent(ds)
	got := a.getChatMCPServers(context.Background(), uuid.New(), uuid.New(), nil)
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetChatMCPTools_ChatServersLoadFailureYieldsNoTools(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	a.mcpClient = mcpclient.New(nil, nil)
	got := a.getChatMCPTools(context.Background(), uuid.New(), uuid.New(), nil, "gpt-5.4")
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
