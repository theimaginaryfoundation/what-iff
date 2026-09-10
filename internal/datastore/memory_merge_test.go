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
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestMergeLiveExtractedMemory_BatchCreateAndFoldLive(t *testing.T) {
	ds, cleanup := newMemoryMergeTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()

	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))

	liveID := uuid.New()
	require.NoError(t, insertMemoryMergeTestMemory(t, ds, liveID, userID, chatID, entmemory.ScopeChat, "Prefers dark mode", nil))

	collapsed := memoryutil.CollapsedExtractedMemory{
		Content:             "Prefers dark mode",
		Scope:               "Chat",
		Confidence:          models.MemoryConfidenceHigh,
		BatchDuplicateCount: 2,
	}
	folded, err := ds.MergeLiveExtractedMemory(ctx, userID, chatID, collapsed, testEmbeddingVector(), uuid.Nil, []uuid.UUID{liveID})
	require.NoError(t, err)
	require.NotNil(t, folded)
	require.Equal(t, liveID, folded.ID)
	require.NotNil(t, folded.ChainMetadata)
	// duplicate_count is a tally of total observations: the pre-existing survivor stands for one
	// observation (even with no prior chain metadata) plus the 2 folded this batch = 3.
	require.Equal(t, 3, folded.ChainMetadata.DuplicateCount)

	events, err := ds.ListMemoryMergeEvents(ctx, userID, 1, 10, models.MemoryMergeEventFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, events.TotalCount)
	event := events.Results[0].(*models.MemoryMergeEvent)
	require.Equal(t, models.MemoryMergeTypeFoldLive, event.MergeType)
	require.Equal(t, 2, event.DuplicatesFolded)
	require.Len(t, event.SourceMembers, 3)
	require.False(t, event.SourceMembers[0].IsNew)
	require.Equal(t, liveID, *event.SourceMembers[0].MemoryID)
	require.True(t, event.SourceMembers[1].IsNew)

	collapsedNew := memoryutil.CollapsedExtractedMemory{
		Content:             "Drinks tea daily",
		Scope:               "User",
		Confidence:          models.MemoryConfidenceMedium,
		BatchDuplicateCount: 3,
	}
	created, err := ds.MergeLiveExtractedMemory(ctx, userID, chatID, collapsedNew, testEmbeddingVector(), uuid.Nil, nil)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotNil(t, created.ChainMetadata)
	require.Equal(t, 3, created.ChainMetadata.DuplicateCount)

	reverted, err := ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.NoError(t, err)
	require.NotNil(t, reverted.RevertedAt)

	restored, err := ds.dbClient.Memory.Get(ctx, liveID)
	require.NoError(t, err)
	require.Nil(t, restored.ChainMetadata)

	exists, err := ds.dbClient.Memory.Query().Where(entmemory.ID(created.ID)).Exist(ctx)
	require.NoError(t, err)
	require.True(t, exists)
}

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

	_, err := ds.MergeLiveExtractedMemory(ctx, userID, chatID, memoryutil.CollapsedExtractedMemory{
		Content:             "Singleton fact",
		Scope:               "User",
		Confidence:          models.MemoryConfidenceMedium,
		BatchDuplicateCount: 1,
	}, testEmbeddingVector(), uuid.Nil, nil)
	require.NoError(t, err)

	_, err = ds.MergeLiveExtractedMemory(ctx, userID, chatID, memoryutil.CollapsedExtractedMemory{
		Content:             "Batch-collapsed fact",
		Scope:               "User",
		Confidence:          models.MemoryConfidenceMedium,
		BatchDuplicateCount: 3,
	}, testEmbeddingVector(), uuid.Nil, nil)
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
		[]uuid.UUID{techID, emoID}, newMembers, nil, nil)
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
