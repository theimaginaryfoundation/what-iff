package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// This file extends memory2_test.go's ListMemories coverage with filters that
// file doesn't exercise (date range, pinned-personality/global-only,
// type/scope, updated_desc sort, and non-positive pagination inputs). It
// reuses that file's helpers (newMemoryTestDatastore, createTestUser,
// createTestChat, createTestPersonality) since they live in the same package.

// createMemoryAt inserts a memory directly (bypassing CreateMemoryFromInput)
// so the test can control created_at precisely.
func createMemoryAt(t *testing.T, ds *Datastore, userID uuid.UUID, content string, createdAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.Memory.Create().
		SetID(id).
		SetContent(content).
		SetScope(entmemory.ScopeUser).
		SetOwnerID(userID).
		SetCreatedAt(createdAt).
		SetUpdatedAt(createdAt).
		Save(context.Background())
	require.NoError(t, err)
	return id
}

func TestListMemories_DateRangeFilters(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createMemoryAt(t, ds, userID, "january", jan1)
	febID := createMemoryAt(t, ds, userID, "february", feb1)
	createMemoryAt(t, ds, userID, "march", mar1)

	// min_date only: excludes january.
	resp, err := ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{MinDate: &feb1})
	require.NoError(t, err)
	require.Equal(t, 2, resp.TotalCount)

	// max_date only: excludes march.
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{MaxDate: &feb1})
	require.NoError(t, err)
	require.Equal(t, 2, resp.TotalCount)

	// Both bounds narrow to exactly february (inclusive on both ends).
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{MinDate: &feb1, MaxDate: &feb1})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Equal(t, febID, resp.Results[0].(*models.Memory).ID)

	// A range excluding every memory returns zero results, not an error.
	afterMar := mar1.Add(24 * time.Hour)
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{MinDate: &afterMar})
	require.NoError(t, err)
	require.Equal(t, 0, resp.TotalCount)
	require.Empty(t, resp.Results)
}

func TestListMemories_PinnedPersonalityAndGlobalOnlyFilters(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	personalityA := uuid.New()
	createTestPersonality(t, ds, personalityA, userID)
	personalityB := uuid.New()
	createTestPersonality(t, ds, personalityB, userID)
	personalityC := uuid.New()
	createTestPersonality(t, ds, personalityC, userID)

	global, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "global", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	thread, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "thread", Level: models.MemoryLevelThread, ChatID: &chatID})
	require.NoError(t, err)
	pinnedA, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "pinned a", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &personalityA})
	require.NoError(t, err)
	pinnedB, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "pinned b", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &personalityB})
	require.NoError(t, err)
	_, err = ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "pinned c", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &personalityC})
	require.NoError(t, err)

	// PinnedPersonalityID = uuid.Nil means "unpinned only": matches global and
	// thread memories, neither of which carries a pinned_personality_id.
	nilID := uuid.Nil
	resp, err := ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{PinnedPersonalityID: &nilID})
	require.NoError(t, err)
	gotIDs := idsOf(resp.Results)
	require.ElementsMatch(t, []uuid.UUID{global.ID, thread.ID}, gotIDs)

	// PinnedPersonalityID = a specific id matches only that pin.
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{PinnedPersonalityID: &personalityA})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Equal(t, pinnedA.ID, resp.Results[0].(*models.Memory).ID)

	// PinnedPersonalityIDs is an OR-list across multiple pins.
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{
		PinnedPersonalityIDs: []uuid.UUID{personalityA, personalityB},
	})
	require.NoError(t, err)
	gotIDs = idsOf(resp.Results)
	require.ElementsMatch(t, []uuid.UUID{pinnedA.ID, pinnedB.ID}, gotIDs)

	// GlobalOnly behaves like PinnedPersonalityID=nil: it matches "no pin", not
	// "level=global" — a thread memory has no pin either, so it also matches.
	globalOnly := true
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{GlobalOnly: &globalOnly})
	require.NoError(t, err)
	gotIDs = idsOf(resp.Results)
	require.ElementsMatch(t, []uuid.UUID{global.ID, thread.ID}, gotIDs)
}

func TestListMemories_TypeAndScopeFilters(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	global, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "global", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	_, err = ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "thread", Level: models.MemoryLevelThread, ChatID: &chatID})
	require.NoError(t, err)

	// Every creatable memory has Type=Context; filtering for it matches everything.
	contextType := models.MemoryTypeContext
	resp, err := ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Type: &contextType})
	require.NoError(t, err)
	require.Equal(t, 2, resp.TotalCount)

	// Filtering for a type nothing has returns zero results, not an error.
	otherType := models.MemoryType("bogus")
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Type: &otherType})
	require.NoError(t, err)
	require.Equal(t, 0, resp.TotalCount)

	// Scope is the raw ent scope column (internal use, e.g. export); "User"
	// scope covers the global memory only.
	userScope := "User"
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Scope: &userScope})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Equal(t, global.ID, resp.Results[0].(*models.Memory).ID)
}

func TestListMemories_StatusFilterExplicit(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	active, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "active", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	inactiveMem, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "inactive", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	inactiveStatus := models.MemoryStatusInactive
	_, err = ds.UpdateMemory(ctx, userID, inactiveMem.ID, models.MemoryPatch{Status: &inactiveStatus})
	require.NoError(t, err)

	activeStatus := models.MemoryStatusActive
	resp, err := ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Status: &activeStatus})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Equal(t, active.ID, resp.Results[0].(*models.Memory).ID)

	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Status: &inactiveStatus})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Equal(t, inactiveMem.ID, resp.Results[0].(*models.Memory).ID)
}

func TestListMemories_SortUpdatedDesc(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	first, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "first created", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	second, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "second created", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	// Touch the first-created memory last, so updated_desc must resurface it first
	// even though created_desc (the default) would put it last.
	newContent := "first created, updated last"
	_, err = ds.UpdateMemory(ctx, userID, first.ID, models.MemoryPatch{Content: &newContent})
	require.NoError(t, err)

	updatedDesc := models.MemorySortUpdatedDesc
	resp, err := ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Sort: &updatedDesc})
	require.NoError(t, err)
	require.Equal(t, first.ID, resp.Results[0].(*models.Memory).ID)
	require.Equal(t, second.ID, resp.Results[1].(*models.Memory).ID)
}

func TestListMemories_NonPositivePageAndPageSizeDefault(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	for i := 0; i < 3; i++ {
		_, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "mem", Level: models.MemoryLevelGlobal})
		require.NoError(t, err)
	}

	resp, err := ds.ListMemories(ctx, userID, -5, -3, models.MemoryFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Page, "negative page must default to 1, same as zero")
	require.Len(t, resp.Results, 3, "negative page size must default to 10, same as zero")
}

func idsOf(results []any) []uuid.UUID {
	ids := make([]uuid.UUID, len(results))
	for i, r := range results {
		ids[i] = r.(*models.Memory).ID
	}
	return ids
}
