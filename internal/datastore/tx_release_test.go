package datastore

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// An error return must not leave its transaction open. A leaked transaction keeps its pooled
// connection checked out until the caller's context ends: on Postgres an idle-in-transaction
// connection the pool can't reuse, on SQLite a write lock every other writer waits behind. Each
// case fails one of the paths that used to return without rolling back; afterwards no connection
// may still be in use.
func TestErrorReturnsReleaseTheirTransaction(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	createGeneratedTestSchema(t, db)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	ctx := context.Background()
	// An account with no preferences row, which is every account until it saves one.
	userID := uuid.New()
	require.NoError(t, client.User.Create().SetID(userID).SetUsername("tx-release").SetEmail("tx@example.com").SetPasswordHash("h").Exec(ctx))
	level := models.MemoryLevel("not-a-level")

	cases := []struct {
		name string
		call func() error
	}{
		{"GetUserPreferences with no preferences", func() error {
			_, err := ds.GetUserPreferences(ctx, userID)
			return err
		}},
		{"CreateChat with no preferences", func() error {
			_, err := ds.CreateChat(ctx, userID, models.Chat{Name: "c"})
			return err
		}},
		{"UpdateChatMessage of a missing message", func() error {
			_, err := ds.UpdateChatMessage(ctx, userID, models.ChatMessage{ID: uuid.New(), Message: "m"})
			return err
		}},
		{"ListMemories with an unknown level", func() error {
			_, err := ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Level: &level})
			return err
		}},
	}
	for _, tc := range cases {
		require.Error(t, tc.call(), tc.name)
		require.Zero(t, db.Stats().InUse, "%s left its transaction open", tc.name)
	}
}
