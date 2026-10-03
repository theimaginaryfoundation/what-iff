package tools

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// A successful write records the new scratchpad and revision on the turn's chat, so the turn's own
// checkpoint (a write conditional on that revision) does not mistake it for a concurrent change.
func TestUpdateScratchpadTool_SuccessAdvancesTheTurnsRevision(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { _ = client.Close() })
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New(), Scratchpad: "old", ScratchpadRevision: 4}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `personalities`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "scratchpad", "scratchpad_revision"}).AddRow(chat.PersonalityID, "old", 5))
	mock.ExpectExec("UPDATE `personalities`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .* FROM `personalities`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "scratchpad", "scratchpad_revision"}).AddRow(chat.PersonalityID, "noted", 6))
	mock.ExpectCommit()

	out, err := NewScratchpadTool(ds, zap.NewNop()).UpdateScratchpadTool(context.Background(), chat, []byte(`{"content":"noted"}`))
	require.NoError(t, err)
	require.Contains(t, out, `"success":true`)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Equal(t, "noted", chat.Scratchpad)
	require.Equal(t, 6, chat.ScratchpadRevision)
}

// UpdateScratchpadTool's cheapest branch is the invalid-JSON-args guard,
// which fires before any datastore access, so datastore can stay nil.
func TestUpdateScratchpadTool_InvalidArgsReturnsErrorResult(t *testing.T) {
	t.Parallel()

	tool := &ScratchpadTool{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}

	out, err := tool.UpdateScratchpadTool(context.Background(), chat, []byte(`not json`))
	require.NoError(t, err)
	require.Contains(t, out, "invalid arguments")
	require.Contains(t, out, `"success":false`)
}
