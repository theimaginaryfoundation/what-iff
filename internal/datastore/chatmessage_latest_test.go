package datastore

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
)

// TestGetLatestMessagesForChats_EmptyInputShortCircuits asserts the helper does not
// touch the database when called with an empty chat ID slice. The cross-resource
// search handler relies on this so a "no chat hits" branch never makes round trips.
func TestGetLatestMessagesForChats_EmptyInputShortCircuits(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	got, err := ds.GetLatestMessagesForChats(context.Background(), uuid.New(), nil)
	require.NoError(t, err)
	require.Empty(t, got)

	got, err = ds.GetLatestMessagesForChats(context.Background(), uuid.New(), []uuid.UUID{})
	require.NoError(t, err)
	require.Empty(t, got)

	require.NoError(t, mock.ExpectationsWereMet())
}
