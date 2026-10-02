package datastore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entembedding "github.com/theimaginaryfoundation/what-iff/ent/embedding"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmerge "github.com/theimaginaryfoundation/what-iff/ent/memorymergeevent"
	entschema "github.com/theimaginaryfoundation/what-iff/ent/schema"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/pgvector/pgvector-go"
)

// Fold rewrite (#249) and exact fold undo (#250) for PersistMemoryMergeGroup / UndoMemoryMergeEvent.

type foldTestMemory struct {
	content    string
	confidence float64
	chain      *entschema.MemoryChainMetadata
	starred    bool
	embedding  []float32 // nil = no embedding row
}

// foldTestMemoryState is the comparable recall-relevant state of one memory row.
type foldTestMemoryState struct {
	Status     entmemory.Status
	Content    string
	Confidence float64
	Chain      string // JSON, so time values compare by their stored form
	Embedding  []float32
}

func insertFoldTestMemory(t *testing.T, ds *Datastore, userID uuid.UUID, m foldTestMemory) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	create := ds.dbClient.Memory.Create().
		SetContent(m.content).
		SetScope(entmemory.ScopeUser).
		SetType(entmemory.TypeContext).
		SetStatus(entmemory.StatusActive).
		SetConfidence(m.confidence).
		SetStarred(m.starred).
		SetOwnerID(userID).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	if m.chain != nil {
		create = create.SetChainMetadata(m.chain)
	}
	row, err := create.Save(ctx)
	require.NoError(t, err)
	if m.embedding != nil {
		_, err := ds.dbClient.Embedding.Create().
			SetEmbedding(pgvector.NewVector(m.embedding)).
			SetMemoryID(row.ID).
			Save(ctx)
		require.NoError(t, err)
	}
	return row.ID
}

func foldTestState(t *testing.T, ds *Datastore, id uuid.UUID) foldTestMemoryState {
	t.Helper()
	ctx := context.Background()
	row, err := ds.dbClient.Memory.Get(ctx, id)
	require.NoError(t, err)
	chain, err := json.Marshal(row.ChainMetadata)
	require.NoError(t, err)
	state := foldTestMemoryState{
		Status:     row.Status,
		Content:    row.Content,
		Confidence: row.Confidence,
		Chain:      string(chain),
	}
	embeddings, err := ds.dbClient.Embedding.Query().
		Where(entembedding.HasMemoryWith(entmemory.ID(id))).
		All(ctx)
	require.NoError(t, err)
	require.LessOrEqual(t, len(embeddings), 1, "a memory has at most one embedding row")
	if len(embeddings) == 1 {
		state.Embedding = embeddings[0].Embedding.Slice()
	}
	return state
}

func foldTestEvent(t *testing.T, ds *Datastore, userID, survivorID uuid.UUID) *ent.MemoryMergeEvent {
	t.Helper()
	row, err := ds.dbClient.MemoryMergeEvent.Query().
		Where(entmerge.UserIDEQ(userID), entmerge.SurvivorMemoryIDEQ(survivorID)).
		Only(context.Background())
	require.NoError(t, err)
	return row
}

func foldTestVector(seed float32) []float32 {
	vec := make([]float32, 8)
	vec[0] = seed
	vec[1] = seed / 2
	return vec
}

func newFoldTestDatastore(t *testing.T) (*Datastore, uuid.UUID, uuid.UUID) {
	t.Helper()
	ds, cleanup := newMemoryMergeTestDatastore(t)
	t.Cleanup(cleanup)
	userID := uuid.New()
	chatID := uuid.New()
	require.NoError(t, insertMemoryMergeTestUser(t, ds, userID))
	require.NoError(t, insertMemoryMergeTestChat(t, ds, userID, chatID))
	return ds, userID, chatID
}

func foldTestGroup(canonical string) models.MemoryMergeGroupProposal {
	return models.MemoryMergeGroupProposal{
		CanonicalContent: canonical,
		Scope:            "User",
		Confidence:       models.MemoryConfidenceHigh,
	}
}

// canonical == survivor (after normalization): the fold keeps the survivor's wording and embedding,
// even when the caller supplied an embedding.
func TestPersistMemoryMergeGroup_FoldSameCanonicalKeepsSurvivor(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	survivorID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Prefers dark mode", confidence: 0.6, embedding: foldTestVector(1)})
	absorbedID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "prefers DARK mode", confidence: 0.6, embedding: foldTestVector(2)})

	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("  prefers   dark MODE "), 2,
		&survivorID, []uuid.UUID{absorbedID}, foldTestVector(9), uuid.Nil, nil, nil)
	require.NoError(t, err)

	got := foldTestState(t, ds, survivorID)
	require.Equal(t, "Prefers dark mode", got.Content)
	require.Equal(t, foldTestVector(1), got.Embedding, "no rewrite, so no re-embed")

	event := foldTestEvent(t, ds, userID, survivorID)
	require.Equal(t, "Prefers dark mode", event.Content)
	require.False(t, event.Snapshot.ContentRewritten)
	require.Empty(t, event.Snapshot.PriorContent)
	require.Equal(t, "prefers   dark MODE", event.Snapshot.CanonicalContent)
}

// canonical != survivor: the fold adopts the canonical phrasing, re-embeds the survivor, records
// the canonical content on the event, and undo restores the prior content and embedding.
func TestPersistMemoryMergeGroup_FoldRewritesSurvivorAndUndoRestores(t *testing.T) {
	cases := []struct {
		name              string
		survivorEmbedding []float32
	}{
		{name: "survivor had an embedding", survivorEmbedding: foldTestVector(1)},
		{name: "survivor had no embedding", survivorEmbedding: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds, userID, chatID := newFoldTestDatastore(t)
			ctx := context.Background()

			survivorID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Likes tea", confidence: 0.6, embedding: tc.survivorEmbedding})
			absorbedID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Drinks oolong most mornings", confidence: 0.6, embedding: foldTestVector(2)})
			before := foldTestState(t, ds, survivorID)

			canonical := "Likes tea, especially oolong in the morning"
			mem, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup(canonical), 2,
				&survivorID, []uuid.UUID{absorbedID}, foldTestVector(9), uuid.Nil, nil, nil)
			require.NoError(t, err)
			require.Equal(t, canonical, mem.Content)

			got := foldTestState(t, ds, survivorID)
			require.Equal(t, canonical, got.Content)
			require.Equal(t, foldTestVector(9), got.Embedding, "survivor re-embedded with the canonical vector")

			event := foldTestEvent(t, ds, userID, survivorID)
			require.Equal(t, canonical, event.Content, "event records the canonical content")
			require.True(t, event.Snapshot.ContentRewritten)
			require.Equal(t, "Likes tea", event.Snapshot.PriorContent)
			require.Equal(t, tc.survivorEmbedding, event.Snapshot.PriorEmbedding)
			require.Equal(t, tc.survivorEmbedding == nil, event.Snapshot.PriorEmbeddingMissing)

			_, err = ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
			require.NoError(t, err)
			require.Equal(t, before, foldTestState(t, ds, survivorID))
		})
	}
}

// Starred memories are never reworded by an automatic fold; the fold still consolidates.
func TestPersistMemoryMergeGroup_FoldDoesNotRewriteStarredSurvivor(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	survivorID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "My cat is called Biscuit", confidence: 0.6, starred: true, embedding: foldTestVector(1)})
	absorbedID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Has a cat", confidence: 0.6, embedding: foldTestVector(2)})

	canonical := "The user has a cat named Biscuit"
	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup(canonical), 2,
		&survivorID, []uuid.UUID{absorbedID}, foldTestVector(9), uuid.Nil, nil, nil)
	require.NoError(t, err)

	got := foldTestState(t, ds, survivorID)
	require.Equal(t, "My cat is called Biscuit", got.Content)
	require.Equal(t, foldTestVector(1), got.Embedding)
	require.Equal(t, entmemory.StatusInactive, foldTestState(t, ds, absorbedID).Status, "the fold still retires the duplicate")

	event := foldTestEvent(t, ds, userID, survivorID)
	require.Equal(t, "My cat is called Biscuit", event.Content)
	require.False(t, event.Snapshot.ContentRewritten)
	require.Equal(t, canonical, event.Snapshot.CanonicalContent, "the declined proposal is kept for audit")
}

// A fold with two absorbed memories, then undo, leaves every row in its pre-fold state: status,
// content, confidence, chain metadata and embedding (recall visibility).
func TestUndoFoldLive_RestoresSurvivorAndAllAbsorbedMemories(t *testing.T) {
	ds, userID, chatID := newFoldTestDatastore(t)
	ctx := context.Background()

	verified := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	survivorID := insertFoldTestMemory(t, ds, userID, foldTestMemory{
		content:    "Runs on weekends",
		confidence: 0.75,
		chain: &entschema.MemoryChainMetadata{
			DuplicateCount:          2,
			VerifiedTimestampsFirst: []time.Time{verified},
			VerifiedTimestampsLast:  []time.Time{verified},
		},
		embedding: foldTestVector(1),
	})
	absorbedA := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Goes running on Saturdays", confidence: 0.6, embedding: foldTestVector(2)})
	absorbedB := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Training for a 10k", confidence: 0.3, embedding: foldTestVector(3)})
	ids := []uuid.UUID{survivorID, absorbedA, absorbedB}

	before := map[uuid.UUID]foldTestMemoryState{}
	for _, id := range ids {
		before[id] = foldTestState(t, ds, id)
	}

	_, err := ds.PersistMemoryMergeGroup(ctx, userID, chatID, foldTestGroup("Runs on weekends (Saturdays) and is training for a 10k"), 3,
		&survivorID, []uuid.UUID{absorbedA, absorbedB}, foldTestVector(9), uuid.Nil, nil, nil)
	require.NoError(t, err)

	for _, id := range []uuid.UUID{absorbedA, absorbedB} {
		folded := foldTestState(t, ds, id)
		require.Equal(t, entmemory.StatusInactive, folded.Status)
		require.Equal(t, before[id].Embedding, folded.Embedding, "absorbed embeddings are kept, not hard-deleted")
	}
	event := foldTestEvent(t, ds, userID, survivorID)
	require.Equal(t, entschema.MemoryMergeUndoSnapshotVersion, event.Snapshot.Version)
	require.Equal(t, []entschema.MemoryMergeAbsorbedMember{
		{MemoryID: absorbedA, PriorStatus: "active"},
		{MemoryID: absorbedB, PriorStatus: "active"},
	}, event.Snapshot.AbsorbedMembers)

	_, err = ds.UndoMemoryMergeEvent(ctx, userID, event.ID)
	require.NoError(t, err)

	for _, id := range ids {
		require.Equal(t, before[id], foldTestState(t, ds, id), "memory %s restored", id)
	}
}

// Events written before the snapshot carried absorbed members / prior content (Version 0) still
// undo: the survivor's confidence and chain metadata come back, and the absorbed rows listed in
// source_members are reactivated even though their embeddings were hard-deleted by the old fold.
func TestUndoFoldLive_LegacyEventWithoutNewSnapshotFields(t *testing.T) {
	ds, userID, _ := newFoldTestDatastore(t)
	ctx := context.Background()

	survivorID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Prefers dark mode", confidence: 0.3, embedding: foldTestVector(1)})
	// What the old fold left behind: absorbed row inactive, embedding deleted.
	absorbedID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Likes dark themes", confidence: 0.6})
	_, err := ds.dbClient.Memory.UpdateOneID(absorbedID).SetStatus(entmemory.StatusInactive).Save(ctx)
	require.NoError(t, err)
	// An unrelated memory the user deactivated themselves is not in the event and stays untouched.
	otherID := insertFoldTestMemory(t, ds, userID, foldTestMemory{content: "Unrelated", confidence: 0.6})
	_, err = ds.dbClient.Memory.UpdateOneID(otherID).SetStatus(entmemory.StatusInactive).Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	legacy, err := ds.dbClient.MemoryMergeEvent.Create().
		SetUserID(userID).
		SetSurvivorMemoryID(survivorID).
		SetMergeType(entmerge.MergeTypeFoldLive).
		SetContent("Prefers dark mode").
		SetDuplicatesFolded(1).
		SetSourceMembers([]entschema.MemoryMergeSourceMember{
			{Content: "Prefers dark mode", Scope: "User", MemoryID: &survivorID},
			{Content: "Likes dark themes", Scope: "User", MemoryID: &absorbedID},
			{Content: "dark mode", Scope: "User", IsNew: true},
		}).
		SetSnapshot(&entschema.MemoryMergeUndoSnapshot{PriorConfidence: 0.6, PriorChainMetadataWasNil: true}).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	require.NoError(t, err)

	reverted, err := ds.UndoMemoryMergeEvent(ctx, userID, legacy.ID)
	require.NoError(t, err)
	require.NotNil(t, reverted.RevertedAt)

	survivor := foldTestState(t, ds, survivorID)
	require.Equal(t, 0.6, survivor.Confidence)
	require.Equal(t, "Prefers dark mode", survivor.Content, "legacy events never rewrote content")
	require.Equal(t, foldTestVector(1), survivor.Embedding, "legacy undo leaves the survivor embedding alone")

	absorbed := foldTestState(t, ds, absorbedID)
	require.Equal(t, entmemory.StatusActive, absorbed.Status, "absorbed row is reactivated")
	require.Nil(t, absorbed.Embedding, "the deleted embedding needs a backfill re-embed (logged)")

	require.Equal(t, entmemory.StatusInactive, foldTestState(t, ds, otherID).Status)
}
