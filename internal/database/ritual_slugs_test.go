package database

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
)

// newRitualSlugsClient opens an in-memory sqlite ent client over the generated schema minus the
// pgvector tables, as the datastore tests do.
func newRitualSlugsClient(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB(dialect.SQLite, db)
	m, err := schema.NewMigrate(drv)
	require.NoError(t, err)
	var tables []*schema.Table
	for _, tb := range migrate.Tables {
		if tb.Name == "embeddings" || tb.Name == "file_chunks" {
			continue
		}
		tables = append(tables, tb)
	}
	require.NoError(t, m.Create(context.Background(), tables...))
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func createSlugTestUser(t *testing.T, client *ent.Client) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, client.User.Create().
		SetID(id).
		SetUsername("u-"+id.String()[:8]).
		SetEmail("u-"+id.String()[:8]+"@example.com").
		SetPasswordHash("hash").
		Exec(context.Background()))
	return id
}

func createLegacyRitual(t *testing.T, client *ent.Client, owner uuid.UUID, name, slug string) uuid.UUID {
	t.Helper()
	r, err := client.Ritual.Create().
		SetOwnerID(owner).
		SetName(name).
		SetSlug(slug).
		SetDescription("d").
		SetContent("c").
		SetHotkeys("").
		Save(context.Background())
	require.NoError(t, err)
	return r.ID
}

func TestBackfillRitualSlugs(t *testing.T) {
	client := newRitualSlugsClient(t)
	ctx := context.Background()
	alice := createSlugTestUser(t, client)
	bob := createSlugTestUser(t, client)

	a1 := createLegacyRitual(t, client, alice, "PDF Forms", "")
	a2 := createLegacyRitual(t, client, alice, "pdf forms", "")       // same derived slug: gets a suffix
	a3 := createLegacyRitual(t, client, alice, "Notes", "notes-kept") // already named: untouched
	a4 := createLegacyRitual(t, client, alice, "Notes kept", "")      // collides with the editor-set slug
	b1 := createLegacyRitual(t, client, bob, "PDF Forms", "")         // other owner: no suffix

	named, err := BackfillRitualSlugs(ctx, client, zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, 4, named)

	slugOf := func(id uuid.UUID) string {
		r, err := client.Ritual.Get(ctx, id)
		require.NoError(t, err)
		return r.Slug
	}
	require.Equal(t, "pdf-forms", slugOf(a1))
	require.Equal(t, "pdf-forms-2", slugOf(a2))
	require.Equal(t, "notes-kept", slugOf(a3))
	require.Equal(t, "notes-kept-2", slugOf(a4))
	require.Equal(t, "pdf-forms", slugOf(b1))

	named, err = BackfillRitualSlugs(ctx, client, nil)
	require.NoError(t, err)
	require.Zero(t, named, "second run finds nothing to do")
}
