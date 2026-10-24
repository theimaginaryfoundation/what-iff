package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

// listModels serves GET /model with one catalog model at tier "low" and
// returns the decoded list.
func listModels(t *testing.T, tiersApply bool) []map[string]any {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Only the table this handler reads; the full schema needs pgvector.
	_, err = db.Exec(`CREATE TABLE models (
		id uuid PRIMARY KEY,
		name text NOT NULL,
		display_name text NOT NULL,
		description text NOT NULL,
		provider text NOT NULL DEFAULT 'openai',
		tool_support bool NOT NULL DEFAULT false,
		vision_support bool NULL,
		base_credits_per_slab integer NOT NULL DEFAULT 1,
		subscription_tier text NOT NULL DEFAULT 'high',
		deleted bool NOT NULL DEFAULT false,
		is_default bool NOT NULL DEFAULT false
	)`)
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	require.NoError(t, client.Model.Create().
		SetName("gpt-5.6-luna").
		SetDisplayName("GPT-5.6 Luna").
		SetDescription("").
		SetProvider("openai").
		SetSubscriptionTier("low").
		Exec(context.Background()))
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	h := &Handler{ds: ds, logger: zap.NewNop(), tiersApply: func() bool { return tiersApply }}
	rec := httptest.NewRecorder()
	h.ListModels(rec, httptest.NewRequest(http.MethodGet, "/model", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	return list
}

func TestListModelsOmitsTiersWhereTheyDoNotApply(t *testing.T) {
	_, ok := listModels(t, false)[0]["subscription_tier"]
	require.False(t, ok, "subscription_tier must be absent without a linked meter")
}

func TestListModelsIncludesTiersWhereTheyApply(t *testing.T) {
	require.Equal(t, "low", listModels(t, true)[0]["subscription_tier"])
}
