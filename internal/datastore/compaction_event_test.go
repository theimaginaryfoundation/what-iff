package datastore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entsnap "github.com/theimaginaryfoundation/what-iff/ent/checkpointsnapshot"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func newCompactionEventTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	// Parent (compaction_events) before child (memory_merge_events) so the FK + ON DELETE SET NULL
	// in createMemoryMergeEventTestSchema can be applied — mirrors production ent schema order.
	return newTestDatastore(t, createMemoryImportTestSchema, createCompactionEventTestSchema, createMemoryMergeEventTestSchema)
}

func createCompactionEventTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE checkpoint_snapshots (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			user_id uuid NOT NULL,
			kind text NOT NULL,
			chat_id uuid,
			personality_id uuid,
			content text NOT NULL,
			content_hash text NOT NULL
		)`,
		`CREATE TABLE compaction_events (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			user_id uuid NOT NULL,
			chat_id uuid NOT NULL,
			personality_id uuid,
			assistant_message_id uuid,
			provider text,
			reason text,
			loaded_memories json,
			created_memories json,
			old_summary_id uuid,
			new_summary_id uuid,
			old_scratchpad_id uuid,
			new_scratchpad_id uuid
		)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func insertCompactionTestPersonality(t *testing.T, ds *Datastore, userID, personalityID uuid.UUID, scratchpad string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := ds.dbClient.Personality.Create().
		SetID(personalityID).
		SetName("p-" + personalityID.String()[:8]).
		SetSystemPrompt("system").
		SetScratchpad(scratchpad).
		SetUserID(userID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	require.NoError(t, err)
}

// TestCompactionEventContentAddressing pins the core dedup guarantee: the snapshot a compaction
// records as its "new" state is the SAME row the next compaction records as its "old" state, so the
// old/new pair never duplicates identical text.
func TestCompactionEventContentAddressing(t *testing.T) {
	ds, cleanup := newCompactionEventTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))
	insertCompactionTestPersonality(t, ds, userID, personalityID, "scratch v1")

	// Compaction 1: summary "" -> (new set later), scratchpad "scratch v0" -> "scratch v1".
	ev1, err := ds.CreateCompactionEvent(ctx, userID, models.CompactionEventInput{
		ChatID:        chatID,
		PersonalityID: &personalityID,
		Provider:      "Claude",
		Reason:        "token_budget",
		OldSummary:    "",
		OldScratchpad: "scratch v0",
		NewScratchpad: "scratch v1",
		HasScratchpad: true,
	})
	require.NoError(t, err)
	require.NotNil(t, ev1.OldSummary)
	require.NotNil(t, ev1.OldScratchpad)
	require.NotNil(t, ev1.NewScratchpad)
	require.Equal(t, "scratch v1", ev1.NewScratchpad.Content)
	require.Equal(t, "chat", ev1.ChatName)
	require.NoError(t, ds.SetCompactionEventNewSummary(ctx, userID, ev1.ID, "summary A"))

	// Compaction 2: old summary is now "summary A" (== ev1 new summary); old scratchpad is
	// "scratch v1" (== ev1 new scratchpad). Both must reuse ev1's snapshot rows.
	ev2, err := ds.CreateCompactionEvent(ctx, userID, models.CompactionEventInput{
		ChatID:        chatID,
		PersonalityID: &personalityID,
		Provider:      "Claude",
		OldSummary:    "summary A",
		OldScratchpad: "scratch v1",
		NewScratchpad: "scratch v2",
		HasScratchpad: true,
	})
	require.NoError(t, err)

	// ev1's new summary row is reused as ev2's old summary row.
	ev1Reloaded, err := ds.GetCompactionEvent(ctx, userID, ev1.ID)
	require.NoError(t, err)
	require.NotNil(t, ev1Reloaded.NewSummary)
	require.Equal(t, "chat", ev1Reloaded.ChatName)
	require.Equal(t, ev1Reloaded.NewSummary.ID, ev2.OldSummary.ID, "new summary of ev1 is old summary of ev2")
	require.Equal(t, ev1.NewScratchpad.ID, ev2.OldScratchpad.ID, "new scratchpad of ev1 is old scratchpad of ev2")

	// Distinct summary states created: "" , "summary A", (scratchpads) "scratch v0","v1","v2".
	summaryCount, err := ds.dbClient.CheckpointSnapshot.Query().Where(entsnap.KindEQ(entsnap.KindSummary)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, summaryCount, `only "" and "summary A" exist; the shared row is not duplicated`)

	scratchCount, err := ds.dbClient.CheckpointSnapshot.Query().Where(entsnap.KindEQ(entsnap.KindScratchpad)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, scratchCount, "scratch v0, v1, v2 — v1 shared between the two events, not duplicated")
}

func TestListCompactionEventsCapsPageSize(t *testing.T) {
	ds, cleanup := newCompactionEventTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))

	for range maxCompactionEventsPageSize + 1 {
		_, err := ds.CreateCompactionEvent(ctx, userID, models.CompactionEventInput{
			ChatID:     chatID,
			Provider:   "OpenAI",
			OldSummary: "before",
		})
		require.NoError(t, err)
	}

	events, err := ds.ListCompactionEvents(ctx, userID, 1, maxCompactionEventsPageSize+1, nil, nil)
	require.NoError(t, err)
	require.Len(t, events.Results, maxCompactionEventsPageSize)
	require.Equal(t, maxCompactionEventsPageSize+1, events.TotalCount)
}

// TestCompactionEventRecordsCreatedMemories verifies a new-only persist under a compaction attaches
// the row to created_memories and does NOT emit a create-type merge event.
func TestCompactionEventRecordsCreatedMemories(t *testing.T) {
	ds, cleanup := newCompactionEventTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))

	ev, err := ds.CreateCompactionEvent(ctx, userID, models.CompactionEventInput{
		ChatID:     chatID,
		Provider:   "OpenAI",
		OldSummary: "before",
	})
	require.NoError(t, err)

	mem, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, models.MemoryMergeGroupProposal{
		MemberIndices:    []int{0},
		Relation:         models.MemoryMergeRelationMerge,
		CanonicalContent: "User prefers dark mode",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}, 1, nil, nil, testEmbeddingVector(), uuid.Nil, nil, &ev.ID)
	require.NoError(t, err)
	require.NotNil(t, mem)

	loaded, err := ds.GetCompactionEvent(ctx, userID, ev.ID)
	require.NoError(t, err)
	require.Empty(t, loaded.MergeEvents, "new-only creates are not merge events")
	require.Len(t, loaded.CreatedMemories, 1)
	require.Equal(t, "User prefers dark mode", loaded.CreatedMemories[0].Content)
	require.NotNil(t, loaded.CreatedMemories[0].MemoryID)
	require.Equal(t, mem.ID, *loaded.CreatedMemories[0].MemoryID)
}

// TestCompactionEventDeleteNullsMergeEventFK pins ON DELETE SET NULL: pruning a compaction event
// must leave merge history intact with compaction_event_id cleared (ent CompactionEvent.merge_events).
func TestCompactionEventDeleteNullsMergeEventFK(t *testing.T) {
	ds, cleanup := newCompactionEventTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))

	survivor, err := ds.CreateMemory(ctx, userID, models.Memory{
		ChatID:     chatID,
		Content:    "User prefers dark mode",
		Scope:      "User",
		Confidence: 0.7,
		Status:     models.MemoryStatusActive,
	}, testEmbeddingVector(), uuid.Nil)
	require.NoError(t, err)

	ev, err := ds.CreateCompactionEvent(ctx, userID, models.CompactionEventInput{
		ChatID:     chatID,
		Provider:   "OpenAI",
		OldSummary: "before",
	})
	require.NoError(t, err)

	survivorID := survivor.ID
	_, err = ds.PersistMemoryMergeGroup(ctx, userID, chatID, models.MemoryMergeGroupProposal{
		MemberIndices:    []int{0, 1},
		Relation:         models.MemoryMergeRelationMerge,
		CanonicalContent: "User prefers dark mode",
		Scope:            "User",
		Confidence:       models.MemoryConfidenceMedium,
	}, 2, &survivorID, nil, nil, uuid.Nil, nil, &ev.ID)
	require.NoError(t, err)

	loaded, err := ds.GetCompactionEvent(ctx, userID, ev.ID)
	require.NoError(t, err)
	require.Len(t, loaded.MergeEvents, 1)
	require.Equal(t, models.MemoryMergeTypeFoldLive, loaded.MergeEvents[0].MergeType)
	mergeEventID := loaded.MergeEvents[0].ID

	require.NoError(t, ds.dbClient.CompactionEvent.DeleteOneID(ev.ID).Exec(ctx))

	row, err := ds.dbClient.MemoryMergeEvent.Get(ctx, mergeEventID)
	require.NoError(t, err, "merge event must survive compaction prune")
	require.Nil(t, row.CompactionEventID, "FK must SET NULL on compaction delete")
}

// TestRevertCheckpointSnapshotScratchpad checks that reverting a scratchpad snapshot rewrites the
// personality's live scratchpad.
func TestRevertCheckpointSnapshotScratchpad(t *testing.T) {
	ds, cleanup := newCompactionEventTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))
	insertCompactionTestPersonality(t, ds, userID, personalityID, "current scratch")

	// Capture a known-good scratchpad state.
	ev, err := ds.CreateCompactionEvent(ctx, userID, models.CompactionEventInput{
		ChatID:        chatID,
		PersonalityID: &personalityID,
		OldSummary:    "",
		OldScratchpad: "known good scratch",
		NewScratchpad: "current scratch",
		HasScratchpad: true,
	})
	require.NoError(t, err)
	require.NotNil(t, ev.OldScratchpad)

	// The live scratchpad drifted; revert to the known-good snapshot.
	reverted, err := ds.RevertCheckpointSnapshot(ctx, userID, ev.OldScratchpad.ID)
	require.NoError(t, err)
	require.Equal(t, models.CheckpointSnapshotKindScratchpad, reverted.Kind)

	p, err := ds.dbClient.Personality.Get(ctx, personalityID)
	require.NoError(t, err)
	require.Equal(t, "known good scratch", p.Scratchpad, "scratchpad reverted to the snapshot content")
}
