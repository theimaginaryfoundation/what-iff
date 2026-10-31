package datastore

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createAgentEntityTestSchema adds agent_entities (columns and partial unique indexes follow
// ent/migrate/schema.go). Needs createMemoryImportTestSchema first for users and personalities.
func createAgentEntityTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE agent_entities (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			key text NOT NULL,
			title text NOT NULL,
			body text NOT NULL DEFAULT '',
			last_personality_id uuid,
			last_chat_id uuid,
			archived_at datetime,
			pinned_personality_id uuid REFERENCES personalities(id) ON DELETE CASCADE,
			user_agent_entities uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX agententity_key_user_agent_entities ON agent_entities(key, user_agent_entities) WHERE pinned_personality_id IS NULL AND archived_at IS NULL`,
		`CREATE UNIQUE INDEX agententity_key_pinned_personality_id_user_agent_entities ON agent_entities(key, pinned_personality_id, user_agent_entities) WHERE pinned_personality_id IS NOT NULL AND archived_at IS NULL`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newAgentEntityTestDatastore(t *testing.T) (*Datastore, *sql.DB, func()) {
	t.Helper()
	var raw *sql.DB
	ds, cleanup := newTestDatastore(t, createMemoryImportTestSchema, createAgentEntityTestSchema, func(_ *testing.T, db *sql.DB) { raw = db })
	return ds, raw, cleanup
}

func insertEntityTestUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now()
	_, err := db.Exec(`INSERT INTO users (id, created_at, updated_at, username, email, password_hash) VALUES (?, ?, ?, ?, ?, '')`,
		id, now, now, "u-"+id.String(), id.String()+"@example.com")
	require.NoError(t, err)
	return id
}

func insertEntityTestPersonality(t *testing.T, db *sql.DB, userID uuid.UUID, name string, autoPin bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now()
	_, err := db.Exec(`INSERT INTO personalities (id, created_at, updated_at, name, system_prompt, auto_pin_memories, user_personalities) VALUES (?, ?, ?, ?, '', ?, ?)`,
		id, now, now, name, autoPin, userID)
	require.NoError(t, err)
	return id
}

func appendTo(text string) func(string) (string, error) {
	return func(body string) (string, error) { return body + text, nil }
}

func TestAgentEntity_GlobalCreateGetAndNormalizedKey(t *testing.T) {
	ds, db, cleanup := newAgentEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := insertEntityTestUser(t, db)
	shared := insertEntityTestPersonality(t, db, userID, "Shared", false)
	other := insertEntityTestPersonality(t, db, userID, "Other", false)
	chatID := uuid.New()

	created, err := ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: shared, ChatID: chatID}, "  Chris   Smith ", "Works on [[Project Y]].")
	require.NoError(t, err)
	require.Equal(t, "chris smith", created.Key)
	require.Equal(t, "Chris Smith", created.Title)
	require.Nil(t, created.PinnedPersonalityID, "auto_pin_memories off: the entity is global")
	require.Equal(t, &shared, created.LastPersonalityID)
	require.Equal(t, &chatID, created.LastChatID)

	got, err := ds.GetAgentEntity(ctx, userID, other, "CHRIS SMITH")
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID, "a global entity is visible to every personality")

	got, err = ds.GetAgentEntity(ctx, userID, uuid.Nil, "chris smith")
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID, "and to conversations with no personality")

	_, err = ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: other}, "chris smith", "dup")
	require.ErrorIs(t, err, ErrAgentEntityExists)

	_, err = ds.GetAgentEntity(ctx, insertEntityTestUser(t, db), uuid.Nil, "chris smith")
	require.ErrorIs(t, err, ErrAgentEntityNotFound, "another user's entity is not visible")
}

func TestAgentEntity_PinnedFollowsAutoPinMemories(t *testing.T) {
	ds, db, cleanup := newAgentEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := insertEntityTestUser(t, db)
	pinner := insertEntityTestPersonality(t, db, userID, "Pinner", true)
	other := insertEntityTestPersonality(t, db, userID, "Other", false)

	created, err := ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: pinner}, "Garden", "Tomatoes.")
	require.NoError(t, err)
	require.Equal(t, &pinner, created.PinnedPersonalityID)

	_, err = ds.GetAgentEntity(ctx, userID, other, "garden")
	require.ErrorIs(t, err, ErrAgentEntityNotFound, "a pinned entity is hidden from other personalities")
	_, err = ds.GetAgentEntity(ctx, userID, uuid.Nil, "garden")
	require.ErrorIs(t, err, ErrAgentEntityNotFound)

	// Another personality can write its own global page with the same key, and the pinned one
	// keeps shadowing it for the pinning personality.
	global, err := ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: other}, "garden", "Shared garden notes.")
	require.NoError(t, err)
	require.Nil(t, global.PinnedPersonalityID)

	got, err := ds.GetAgentEntity(ctx, userID, pinner, "garden")
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID, "the pinned entity wins over the global one")

	// The pinning personality sees a global key as taken rather than shadowing it with a new page.
	_, err = ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: other}, "Lake House", "x")
	require.NoError(t, err)
	_, err = ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: pinner}, "lake house", "y")
	require.ErrorIs(t, err, ErrAgentEntityExists)

	// Deleting the personality takes its pinned entities with it.
	_, err = db.Exec(`DELETE FROM personalities WHERE id = ?`, pinner)
	require.NoError(t, err)
	list, total, err := ds.ListAgentEntities(ctx, userID, models.AgentEntityFilters{})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	for _, e := range list {
		require.Nil(t, e.PinnedPersonalityID)
	}
}

func TestAgentEntity_UpdateAppendAndLimit(t *testing.T) {
	ds, db, cleanup := newAgentEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := insertEntityTestUser(t, db)
	w := models.AgentEntityWriter{ChatID: uuid.New()}

	_, err := ds.UpdateAgentEntity(ctx, userID, w, "nobody", appendTo("x"))
	require.ErrorIs(t, err, ErrAgentEntityNotFound)

	_, err = ds.CreateAgentEntity(ctx, userID, w, "Notes", "one")
	require.NoError(t, err)

	got, err := ds.UpdateAgentEntity(ctx, userID, w, "notes", appendTo("\ntwo"))
	require.NoError(t, err)
	require.Equal(t, "one\ntwo", got.Body)

	got, err = ds.UpdateAgentEntity(ctx, userID, w, "NOTES", func(string) (string, error) { return "replaced", nil })
	require.NoError(t, err)
	require.Equal(t, "replaced", got.Body)

	_, err = ds.UpdateAgentEntity(ctx, userID, w, "notes", appendTo(strings.Repeat("é", models.MaxEntityBodyRunes)))
	require.ErrorIs(t, err, models.ErrEntityBodyTooLong)
	got, err = ds.GetAgentEntity(ctx, userID, uuid.Nil, "notes")
	require.NoError(t, err)
	require.Equal(t, "replaced", got.Body, "a refused write changes nothing")

	_, err = ds.CreateAgentEntity(ctx, userID, w, "big", strings.Repeat("a", models.MaxEntityBodyRunes+1))
	require.ErrorIs(t, err, models.ErrEntityBodyTooLong)
	_, err = ds.CreateAgentEntity(ctx, userID, w, "   ", "x")
	require.ErrorIs(t, err, models.ErrInvalidEntityKey)
}

func TestAgentEntity_UpdateRetriesAfterConcurrentChange(t *testing.T) {
	ds, db, cleanup := newAgentEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := insertEntityTestUser(t, db)
	w := models.AgentEntityWriter{}
	_, err := ds.CreateAgentEntity(ctx, userID, w, "log", "a")
	require.NoError(t, err)

	// The first attempt's read is overtaken by another append; the retry must build on it.
	raced := false
	got, err := ds.UpdateAgentEntity(ctx, userID, w, "log", func(body string) (string, error) {
		if !raced {
			raced = true
			_, err := db.Exec(`UPDATE agent_entities SET body = body || 'b'`)
			require.NoError(t, err)
		}
		return body + "c", nil
	})
	require.NoError(t, err)
	require.Equal(t, "abc", got.Body)
}

func TestAgentEntity_UserEditListAndDelete(t *testing.T) {
	ds, db, cleanup := newAgentEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := insertEntityTestUser(t, db)
	pinner := insertEntityTestPersonality(t, db, userID, "Pinner", true)
	chatID := uuid.New()

	pinned, err := ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: pinner, ChatID: chatID}, "Rex", "A dog.")
	require.NoError(t, err)
	_, err = ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{}, "Oslo", "A city.")
	require.NoError(t, err)

	q := "dog"
	list, total, err := ds.ListAgentEntities(ctx, userID, models.AgentEntityFilters{Query: &q})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "Pinner", list[0].PinnedPersonalityName)

	global := uuid.Nil
	_, total, err = ds.ListAgentEntities(ctx, userID, models.AgentEntityFilters{PinnedPersonalityID: &global})
	require.NoError(t, err)
	require.Equal(t, 1, total)

	edited, err := ds.SetAgentEntityBody(ctx, userID, pinned.ID, "A good dog.")
	require.NoError(t, err)
	require.Equal(t, "A good dog.", edited.Body)
	require.Nil(t, edited.LastChatID, "a user edit clears the agent provenance")
	require.Equal(t, "Pinner", edited.PinnedPersonalityName)

	stranger := insertEntityTestUser(t, db)
	_, err = ds.SetAgentEntityBody(ctx, stranger, pinned.ID, "mine")
	require.ErrorIs(t, err, ErrAgentEntityNotFound)
	require.ErrorIs(t, ds.DeleteAgentEntity(ctx, stranger, pinned.ID), ErrAgentEntityNotFound)

	require.NoError(t, ds.DeleteAgentEntity(ctx, userID, pinned.ID))
	_, err = ds.GetAgentEntity(ctx, userID, pinner, "rex")
	require.ErrorIs(t, err, ErrAgentEntityNotFound)
}

func TestAgentEntity_PrefixListShowsWhatAReadSees(t *testing.T) {
	ds, db, cleanup := newAgentEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := insertEntityTestUser(t, db)
	pinner := insertEntityTestPersonality(t, db, userID, "Pinner", true)
	other := insertEntityTestPersonality(t, db, userID, "Other", false)

	_, err := ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: pinner}, "Project Alpha", "pinned")
	require.NoError(t, err)
	shared, err := ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: other}, "Project Alpha", "shared")
	require.NoError(t, err)
	_, err = ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: other}, "Project Beta", "x")
	require.NoError(t, err)
	_, err = ds.CreateAgentEntity(ctx, userID, models.AgentEntityWriter{ActivePersonalityID: other}, "Chris", "x")
	require.NoError(t, err)

	list, total, err := ds.ListVisibleAgentEntities(ctx, userID, pinner, "  PROJECT ", 10)
	require.NoError(t, err)
	require.Equal(t, 2, total, "the shadowed global page is listed once")
	require.Equal(t, "Project Alpha", list[0].Title)
	require.Equal(t, models.EntityScopePersonality, list[0].Scope(), "as the pinned page a read returns")
	require.Equal(t, "Project Beta", list[1].Title)

	list, total, err = ds.ListVisibleAgentEntities(ctx, userID, other, "", 1)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, list, 1)
	require.Equal(t, "Chris", list[0].Title)

	// The other personality sees only the shared Project Alpha.
	list, _, err = ds.ListVisibleAgentEntities(ctx, userID, other, "project a", 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, shared.ID, list[0].ID)
}

func TestAgentEntity_ArchiveHidesFreesTheKeyAndRestores(t *testing.T) {
	ds, db, cleanup := newAgentEntityTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := insertEntityTestUser(t, db)
	persona := insertEntityTestPersonality(t, db, userID, "Vix", false)
	chatID := uuid.New()
	w := models.AgentEntityWriter{ActivePersonalityID: persona, ChatID: chatID}

	created, err := ds.CreateAgentEntity(ctx, userID, w, "Old Car", "Sold in 2025.")
	require.NoError(t, err)

	archived, err := ds.ArchiveAgentEntity(ctx, userID, w, "old car")
	require.NoError(t, err)
	require.Equal(t, created.ID, archived.ID)
	require.NotNil(t, archived.ArchivedAt)
	require.Equal(t, "Sold in 2025.", archived.Body, "an archived page keeps its text")
	require.Equal(t, &chatID, archived.LastChatID, "and who archived it")

	_, err = ds.GetAgentEntity(ctx, userID, persona, "old car")
	require.ErrorIs(t, err, ErrAgentEntityNotFound)
	_, err = ds.ArchiveAgentEntity(ctx, userID, w, "old car")
	require.ErrorIs(t, err, ErrAgentEntityNotFound)
	_, err = ds.UpdateAgentEntity(ctx, userID, w, "old car", appendTo("x"))
	require.ErrorIs(t, err, ErrAgentEntityNotFound)
	_, total, err := ds.ListAgentEntities(ctx, userID, models.AgentEntityFilters{})
	require.NoError(t, err)
	require.Zero(t, total, "the live list leaves archived pages out")
	list, total, err := ds.ListAgentEntities(ctx, userID, models.AgentEntityFilters{Archived: true})
	require.NoError(t, err)
	require.Equal(t, 1, total, "the archived list has them")
	require.Equal(t, created.ID, list[0].ID)
	_, err = ds.SetAgentEntityBody(ctx, userID, created.ID, "edit")
	require.ErrorIs(t, err, ErrAgentEntityNotFound)

	// While the name is taken again by a new page, the archived one cannot come back.
	again, err := ds.CreateAgentEntity(ctx, userID, w, "Old Car", "A new page.")
	require.NoError(t, err, "the key is free once the old page is archived")
	_, err = ds.RestoreAgentEntity(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrAgentEntityExists)

	require.NoError(t, ds.DeleteAgentEntity(ctx, userID, again.ID))
	restored, err := ds.RestoreAgentEntity(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Nil(t, restored.ArchivedAt)
	require.Nil(t, restored.LastChatID, "the restore is the user's")
	got, err := ds.GetAgentEntity(ctx, userID, persona, "old car")
	require.NoError(t, err)
	require.Equal(t, "Sold in 2025.", got.Body)

	_, err = ds.RestoreAgentEntity(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrAgentEntityNotFound, "only archived entities restore")
}
