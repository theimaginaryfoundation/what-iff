package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmerge "github.com/theimaginaryfoundation/what-iff/ent/memorymergeevent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Legacy create-type merge events remain undoable (delete survivor) even though new code no longer
// emits them — keeps undo working for rows written before the created_memories refactor.
func TestUndoMemoryMergeEvent_CreateDeletesSurvivor(t *testing.T) {
	ds, cleanup := newMemoryMergeTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))

	created, err := ds.CreateMemory(ctx, userID, models.Memory{
		ChatID:     chatID,
		Content:    "New fact",
		Scope:      "Chat",
		Confidence: 0.7,
		Status:     models.MemoryStatusActive,
	}, testEmbeddingVector(), uuid.Nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	row, err := ds.dbClient.MemoryMergeEvent.Create().
		SetUserID(userID).
		SetSurvivorMemoryID(created.ID).
		SetMergeType(entmerge.MergeTypeCreate).
		SetContent(created.Content).
		SetDuplicatesFolded(1).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	require.NoError(t, err)

	_, err = ds.UndoMemoryMergeEvent(ctx, userID, row.ID)
	require.NoError(t, err)

	exists, err := ds.dbClient.Memory.Query().Where(entmemory.ID(created.ID)).Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}

// New-only persists (including multi-extraction collapses) must not appear in merge history.
func TestListMemoryMergeEvents_HidesCreates(t *testing.T) {
	ds, cleanup := newMemoryMergeTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))

	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, models.MemoryMergeGroupProposal{
		CanonicalContent: "Singleton fact",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}, 1, nil, nil, testEmbeddingVector(), uuid.Nil, nil, nil)
	require.NoError(t, err)

	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, models.MemoryMergeGroupProposal{
		CanonicalContent: "Batch-collapsed fact",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}, 3, nil, nil, testEmbeddingVector(), uuid.Nil, nil, nil)
	require.NoError(t, err)

	listed, err := ds.ListMemoryMergeEvents(ctx, userID, 1, 10, models.MemoryMergeEventFilters{})
	require.NoError(t, err)
	require.Equal(t, 0, listed.TotalCount)
}

// TestPersistMemoryLinkGroup_LinksAndUndo encodes the user's harder case: the Apple Cobbler
// memories are the same event across different functional registers (technical / emotional /
// narrative). The correct behavior is to LINK, not merge — preserve every surface, cross-reference
// them, and keep them all active + searchable. Reverting withdraws only the cross-reference.
func TestPersistMemoryLinkGroup_LinksAndUndo(t *testing.T) {
	ds, cleanup := newMemoryMergeTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))

	// Two already-stored surfaces of the same event (technical + emotional registers).
	techID := uuid.New()
	emoID := uuid.New()
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, techID, userID, chatID, entmemory.ScopeUser, "Apple Cobbler proved token-space has no canonical English output; fidelity must be evaluated semantically", nil))
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, emoID, userID, chatID, entmemory.ScopeUser, "The cobbler event is a personal myth in the Gori/Vix relationship", nil))

	// One freshly-extracted surface (narrative register) joins as a new member.
	newMembers := []LinkGroupNewMember{{
		Content:    "Gori ran a stealth experiment asking for a cobbler with a specific apple; ice cream felt like a 10x multiplier",
		Confidence: models.MemoryConfidenceMedium,
		Embedding:  testEmbeddingVector(),
	}}

	event, err := ds.PersistMemoryLinkGroup(ctx, userID, chatID, "User",
		"Apple Cobbler event (technical/emotional/narrative registers)",
		[]uuid.UUID{techID, emoID}, newMembers, nil, nil, uuid.Nil)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, models.MemoryMergeTypeLink, event.MergeType)
	require.NotNil(t, event.LinkGroupID)

	// All three surfaces share the link group, stay active, and are NOT folded away.
	linked, err := ds.dbClient.Memory.Query().Where(entmemory.LinkGroupID(*event.LinkGroupID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, linked, 3, "two existing + one created member are cross-referenced")
	for _, m := range linked {
		require.Equal(t, entmemory.StatusActive, m.Status, "linking never de-indexes a surface")
	}

	// The created member is retrievable (has an embedding); linking does not delete embeddings.
	embeddingCount, err := ds.dbClient.Embedding.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, embeddingCount, "the new link member was embedded")

	// Undo withdraws the cross-reference but preserves every memory row.
	_, err = ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.NoError(t, err)

	stillLinked, err := ds.dbClient.Memory.Query().Where(entmemory.LinkGroupIDNotNil()).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, stillLinked, "revert unlinks all members")

	total, err := ds.dbClient.Memory.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, total, "revert keeps all surfaces; nothing is deleted")
}

func newMemoryMergeTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	// compaction_events must exist before memory_merge_events: the merge table holds an FK to it
	// (ON DELETE SET NULL), matching ent/migrate.MemoryMergeEventsTable.
	return newTestDatastore(t, createMemoryImportTestSchema, createCompactionEventTestSchema, createMemoryMergeEventTestSchema)
}

// createMemoryMergeEventTestSchema mirrors ent MemoryMergeEventsTable, including the nullable FK to
// compaction_events (ON DELETE SET NULL) and the compaction_event_id index. Callers must create
// compaction_events first (see createCompactionEventTestSchema).
func createMemoryMergeEventTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE memory_merge_events (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			user_id uuid NOT NULL,
			survivor_memory_id uuid NOT NULL,
			merge_type text NOT NULL,
			content text NOT NULL,
			duplicates_folded integer NOT NULL DEFAULT 1,
			link_group_id uuid,
			source_members json,
			snapshot json,
			reverted_at datetime,
			compaction_event_id uuid,
			FOREIGN KEY (compaction_event_id) REFERENCES compaction_events(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX memorymergeevent_user_id_created_at ON memory_merge_events (user_id, created_at)`,
		`CREATE INDEX memorymergeevent_compaction_event_id ON memory_merge_events (compaction_event_id)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func insertMemoryMergeTestUser(t *testing.T, ds *Datastore, userID uuid.UUID) error {
	t.Helper()
	now := time.Now().UTC()
	_, err := ds.dbClient.User.Create().
		SetID(userID).
		SetUsername("merge-" + userID.String()[:8]).
		SetEmail(userID.String() + "@example.com").
		SetPasswordHash("hash").
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	return err
}

func insertMemoryMergeTestChat(t *testing.T, ds *Datastore, userID, chatID uuid.UUID) error {
	t.Helper()
	_, err := ds.dbClient.Chat.Create().
		SetID(chatID).
		SetName("chat").
		SetOwnerID(userID).
		Save(context.Background())
	return err
}

func insertMemoryMergeTestMemory(t *testing.T, ds *Datastore, memoryID, userID, chatID uuid.UUID, scope entmemory.Scope, content string, chainMeta any) error {
	t.Helper()
	now := time.Now().UTC()
	create := ds.dbClient.Memory.Create().
		SetID(memoryID).
		SetContent(content).
		SetScope(scope).
		SetType(entmemory.TypeContext).
		SetStatus(entmemory.StatusActive).
		SetConfidence(0.6).
		SetOwnerID(userID).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	if scope == entmemory.ScopeChat {
		create = create.SetChatID(chatID)
	}
	_, err := create.Save(context.Background())
	return err
}

func testEmbeddingVector() []float32 {
	vec := make([]float32, 8)
	vec[0] = 1
	return vec
}

// TestAutoPin_AppliesToEveryMemoryCreationPath pins down the auto-pin rule (ent/schema
// Personality.auto_pin_memories) for each path that creates a memory while a personality is
// active: the create_memory tool (CreateMemory), a new-only checkpoint fold
// (PersistMemoryMergeGroup) and a new checkpoint link-group member (PersistMemoryLinkGroup).
// A new User-scoped memory is pinned iff the active personality has auto-pin on; Chat-scoped
// memories are never pinned.
func TestAutoPin_AppliesToEveryMemoryCreationPath(t *testing.T) {
	type createFn func(t *testing.T, ds *Datastore, userID, chatID, personalityID uuid.UUID, scope string) uuid.UUID

	paths := map[string]createFn{
		"create_memory": func(t *testing.T, ds *Datastore, userID, chatID, personalityID uuid.UUID, scope string) uuid.UUID {
			mem, err := ds.CreateMemory(context.Background(), userID, models.Memory{
				ChatID:  chatID,
				Content: "Prefers oolong",
				Scope:   scope,
			}, testEmbeddingVector(), personalityID)
			require.NoError(t, err)
			return mem.ID
		},
		"checkpoint_fold": func(t *testing.T, ds *Datastore, userID, chatID, personalityID uuid.UUID, scope string) uuid.UUID {
			mem, err := ds.PersistMemoryMergeGroup(context.Background(), userID, chatID, models.MemoryMergeGroupProposal{
				CanonicalContent: "Prefers oolong",
				Scope:            scope,
				Confidence:       models.MemoryConfidenceMedium,
			}, 2, nil, nil, testEmbeddingVector(), personalityID, nil, nil)
			require.NoError(t, err)
			return mem.ID
		},
		"checkpoint_link": func(t *testing.T, ds *Datastore, userID, chatID, personalityID uuid.UUID, scope string) uuid.UUID {
			existingID := uuid.New()
			require.NoError(t, insertMemoryMergeTestMemory(t, ds, existingID, userID, chatID, entmemory.Scope(scope), "Tea ritual matters", nil))
			event, err := ds.PersistMemoryLinkGroup(context.Background(), userID, chatID, scope, "Tea",
				[]uuid.UUID{existingID},
				[]LinkGroupNewMember{{Content: "Prefers oolong", Confidence: models.MemoryConfidenceMedium, Embedding: testEmbeddingVector()}},
				nil, nil, personalityID)
			require.NoError(t, err)
			require.NotNil(t, event)
			created, err := ds.dbClient.Memory.Query().
				Where(entmemory.LinkGroupID(*event.LinkGroupID), entmemory.IDNEQ(existingID)).
				Only(context.Background())
			require.NoError(t, err)
			// Linking never repins an existing member.
			existing, err := ds.dbClient.Memory.Get(context.Background(), existingID)
			require.NoError(t, err)
			require.Nil(t, existing.PinnedPersonalityID)
			return created.ID
		},
	}

	cases := []struct {
		name         string
		autoPin      bool
		scope        string
		noPersona    bool
		expectPinned bool
	}{
		{name: "auto-pin on, User scope", autoPin: true, scope: "User", expectPinned: true},
		{name: "auto-pin off, User scope", autoPin: false, scope: "User"},
		{name: "auto-pin on, Chat scope", autoPin: true, scope: "Chat"},
		{name: "no active personality", scope: "User", noPersona: true},
	}

	for pathName, create := range paths {
		for _, tc := range cases {
			t.Run(pathName+"/"+tc.name, func(t *testing.T) {
				ds, cleanup := newMemoryMergeTestDatastore(t)
				defer cleanup()
				ctx := context.Background()
				userID := uuid.New()
				chatID := uuid.New()
				require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
				require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))
				personalityID := uuid.New()
				createTestPersonality(t, ds, personalityID, userID)
				_, err := ds.dbClient.Personality.UpdateOneID(personalityID).SetAutoPinMemories(tc.autoPin).Save(ctx)
				require.NoError(t, err)
				active := personalityID
				if tc.noPersona {
					active = uuid.Nil
				}

				memID := create(t, ds, userID, chatID, active, tc.scope)

				got, err := ds.dbClient.Memory.Get(ctx, memID)
				require.NoError(t, err)
				if tc.expectPinned {
					require.NotNil(t, got.PinnedPersonalityID)
					require.Equal(t, personalityID, *got.PinnedPersonalityID)
				} else {
					require.Nil(t, got.PinnedPersonalityID)
				}
			})
		}
	}
}
