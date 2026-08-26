package datastore

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/embedding"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// --- Pure/logic helper tests ---

func TestValidateMemoryType(t *testing.T) {
	tests := []struct {
		name    string
		input   models.MemoryType
		wantErr bool
	}{
		{name: "empty type is valid", input: "", wantErr: false},
		{name: "context type is valid", input: models.MemoryTypeContext, wantErr: false},
		{name: "unknown type is invalid", input: models.MemoryType("bogus"), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMemoryType(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidRequestBody)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNormalizeMemoryStatus(t *testing.T) {
	tests := []struct {
		name  string
		input models.MemoryStatus
		want  entmemory.Status
	}{
		{name: "inactive maps to inactive", input: models.MemoryStatusInactive, want: entmemory.StatusInactive},
		{name: "active maps to active", input: models.MemoryStatusActive, want: entmemory.StatusActive},
		{name: "empty defaults to active", input: "", want: entmemory.StatusActive},
		{name: "unknown defaults to active", input: models.MemoryStatus("bogus"), want: entmemory.StatusActive},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, normalizeMemoryStatus(tc.input))
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{name: "shorter than max is unchanged", input: "hello", maxLen: 10, want: "hello"},
		{name: "equal to max is unchanged", input: "hello", maxLen: 5, want: "hello"},
		{name: "longer than max is truncated with ellipsis", input: "hello world", maxLen: 5, want: "hello..."},
		{name: "empty string", input: "", maxLen: 5, want: ""},
		{name: "zero max length truncates everything", input: "hi", maxLen: 0, want: "..."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, truncateString(tc.input, tc.maxLen))
		})
	}
}

func TestChainMetadataToModel(t *testing.T) {
	require.Nil(t, chainMetadataToModel(nil))
}

// --- embeddingCosineRelevance tests ---

func TestEmbeddingCosineRelevance(t *testing.T) {
	tests := []struct {
		name      string
		query     []float32
		stored    []float32
		wantOK    bool
		wantScore float64
	}{
		{
			name:      "identical vectors have similarity 1",
			query:     []float32{1, 0, 0},
			stored:    []float32{1, 0, 0},
			wantOK:    true,
			wantScore: 1,
		},
		{
			name:      "orthogonal vectors have similarity 0",
			query:     []float32{1, 0},
			stored:    []float32{0, 1},
			wantOK:    true,
			wantScore: 0,
		},
		{
			name:      "opposite vectors clamp to 0 rather than negative",
			query:     []float32{1, 0},
			stored:    []float32{-1, 0},
			wantOK:    true,
			wantScore: 0,
		},
		{
			name:   "mismatched lengths return not ok",
			query:  []float32{1, 0, 0},
			stored: []float32{1, 0},
			wantOK: false,
		},
		{
			name:   "empty query returns not ok",
			query:  []float32{},
			stored: []float32{1, 0},
			wantOK: false,
		},
		{
			name:   "zero query vector returns not ok",
			query:  []float32{0, 0},
			stored: []float32{1, 1},
			wantOK: false,
		},
		{
			name:   "zero stored vector returns not ok",
			query:  []float32{1, 1},
			stored: []float32{0, 0},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := embeddingCosineRelevance(tc.query, pgvector.NewVector(tc.stored))
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				require.InDelta(t, tc.wantScore, got, 0.0001)
			}
		})
	}
}

// --- memoryCursorPredicate integration test (needs a real ent query to exercise the builder) ---

func TestMemoryCursorPredicate(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	base := time.Now().UTC().Truncate(time.Second)
	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		id := uuid.New()
		_, err := ds.dbClient.Memory.Create().
			SetID(id).
			SetContent("mem").
			SetScope(entmemory.ScopeUser).
			SetOwnerID(userID).
			SetCreatedAt(base.Add(time.Duration(i) * time.Second)).
			Save(ctx)
		require.NoError(t, err)
		ids = append(ids, id)
	}

	// Zero-value cursor returns everything, in insertion order.
	all, err := memoryCursorPredicate(time.Time{}, uuid.Nil)(
		ds.dbClient.Memory.Query().Order(ent.Asc(entmemory.FieldCreatedAt)),
	).All(ctx)
	require.NoError(t, err)
	require.Len(t, all, 3)

	// Cursor past the first record returns only the remaining two.
	rest, err := memoryCursorPredicate(base, ids[0])(
		ds.dbClient.Memory.Query().Order(ent.Asc(entmemory.FieldCreatedAt)),
	).All(ctx)
	require.NoError(t, err)
	require.Len(t, rest, 2)
	require.Equal(t, ids[1], rest[0].ID)
	require.Equal(t, ids[2], rest[1].ID)
}

// --- test helpers shared by this file ---

// newMemoryTestDatastore builds a sqlite datastore with the shared memory-import schema
// plus the chats-table columns (e.g. "source") that memory.go's WithChat() eager-load
// selects but createMemoryImportTestSchema's fixture omits.
func newMemoryTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, alterChatsTableForAgentJobTests)
}

func createTestUser(t *testing.T, ds *Datastore, userID uuid.UUID) {
	t.Helper()
	_, err := ds.dbClient.User.Create().
		SetID(userID).
		SetUsername("user-" + userID.String()).
		SetEmail(userID.String() + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
}

func createTestChat(t *testing.T, ds *Datastore, chatID, ownerID uuid.UUID) {
	t.Helper()
	_, err := ds.dbClient.Chat.Create().
		SetID(chatID).
		SetName("chat-" + chatID.String()).
		SetOwnerID(ownerID).
		Save(context.Background())
	require.NoError(t, err)
}

func createTestPersonality(t *testing.T, ds *Datastore, personalityID, ownerID uuid.UUID) {
	t.Helper()
	_, err := ds.dbClient.Personality.Create().
		SetID(personalityID).
		SetName("personality-" + personalityID.String()).
		SetSystemPrompt("prompt").
		SetUserID(ownerID).
		Save(context.Background())
	require.NoError(t, err)
}

// --- CreateMemoryFromInput / CreateMemoriesBatch / createMemoryFromLevelInput ---

func TestCreateMemoryFromInput_HappyPaths(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)

	tests := []struct {
		name  string
		input models.CreateMemoryInput
		check func(t *testing.T, mem *models.Memory)
	}{
		{
			name:  "global memory",
			input: models.CreateMemoryInput{Content: "  global fact  ", Level: models.MemoryLevelGlobal},
			check: func(t *testing.T, mem *models.Memory) {
				require.Equal(t, "global fact", mem.Content)
				require.Equal(t, models.MemoryLevelGlobal, mem.Level)
			},
		},
		{
			name:  "thread memory",
			input: models.CreateMemoryInput{Content: "thread fact", Level: models.MemoryLevelThread, ChatID: &chatID},
			check: func(t *testing.T, mem *models.Memory) {
				require.Equal(t, models.MemoryLevelThread, mem.Level)
				require.Equal(t, chatID, mem.ChatID)
			},
		},
		{
			name:  "personality memory",
			input: models.CreateMemoryInput{Content: "personality fact", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &personalityID},
			check: func(t *testing.T, mem *models.Memory) {
				require.Equal(t, models.MemoryLevelPersonality, mem.Level)
				require.NotNil(t, mem.PinnedPersonalityID)
				require.Equal(t, personalityID, *mem.PinnedPersonalityID)
			},
		},
		{
			name:  "summary memory",
			input: models.CreateMemoryInput{Content: "summary fact", Level: models.MemoryLevelSummary, ChatID: &chatID},
			check: func(t *testing.T, mem *models.Memory) {
				require.Equal(t, models.MemoryLevelSummary, mem.Level)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem, err := ds.CreateMemoryFromInput(ctx, userID, tc.input)
			require.NoError(t, err)
			require.NotNil(t, mem)
			tc.check(t, mem)
		})
	}
}

func TestCreateMemoryFromInput_ValidationFailures(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	foreignChatID := uuid.New()
	createTestChat(t, ds, foreignChatID, otherUserID)
	foreignPersonalityID := uuid.New()
	createTestPersonality(t, ds, foreignPersonalityID, otherUserID)
	missingChatID := uuid.New()

	tests := []struct {
		name    string
		input   models.CreateMemoryInput
		wantErr error // nil means just require.Error
	}{
		{
			name:  "empty content",
			input: models.CreateMemoryInput{Content: "   ", Level: models.MemoryLevelGlobal},
		},
		{
			name:  "invalid level",
			input: models.CreateMemoryInput{Content: "x", Level: models.MemoryLevel("bogus")},
		},
		{
			name:  "invalid type",
			input: models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelGlobal, Type: models.MemoryType("bogus")},
		},
		{
			// validateLevelInput's per-level branches return a plain error (not wrapped in
			// ErrInvalidRequestBody) — only the default/unknown-level branch wraps it.
			name:  "thread memory without chat_id",
			input: models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelThread},
		},
		{
			name:    "chat not owned by user",
			input:   models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelThread, ChatID: &foreignChatID},
			wantErr: ErrChatNotFound,
		},
		{
			name:    "chat does not exist",
			input:   models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelThread, ChatID: &missingChatID},
			wantErr: ErrChatNotFound,
		},
		{
			name:    "personality not owned by user",
			input:   models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &foreignPersonalityID},
			wantErr: ErrPersonalityNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem, err := ds.CreateMemoryFromInput(ctx, userID, tc.input)
			require.Error(t, err)
			require.Nil(t, mem)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestCreateMemoriesBatch_EmptyItems(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	out, err := ds.CreateMemoriesBatch(ctx, uuid.New(), models.BatchCreateMemoryInput{})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestCreateMemoriesBatch_AllOrNoneRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	input := models.BatchCreateMemoryInput{
		AllOrNone: true,
		Items: []models.CreateMemoryInput{
			{Content: "good one", Level: models.MemoryLevelGlobal},
			{Content: "bad one", Level: models.MemoryLevelThread}, // missing chat_id -> fails validation
		},
	}

	out, err := ds.CreateMemoriesBatch(ctx, userID, input)
	require.Error(t, err)
	require.Nil(t, out)

	count, err := ds.dbClient.Memory.Query().Where(entmemory.HasOwnerWith()).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "all-or-none batch should not persist any memory when one item fails")
}

func TestCreateMemoriesBatch_PartialSkipsInvalidItems(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	input := models.BatchCreateMemoryInput{
		AllOrNone: false,
		Items: []models.CreateMemoryInput{
			{Content: "good one", Level: models.MemoryLevelGlobal},
			{Content: "bad one", Level: models.MemoryLevelThread}, // skipped
			{Content: "good two", Level: models.MemoryLevelGlobal},
		},
	}

	out, err := ds.CreateMemoriesBatch(ctx, userID, input)
	require.NoError(t, err)
	require.Len(t, out, 2)

	count, err := ds.dbClient.Memory.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

// --- CreateMemory ---

func TestCreateMemory_HappyPath(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	mem, err := ds.CreateMemory(ctx, userID, models.Memory{
		Content: "raw create",
		Scope:   string(entmemory.ScopeChat),
		ChatID:  chatID,
	}, []float32{0.1, 0.2}, uuid.Nil)
	require.NoError(t, err)
	require.NotNil(t, mem)
	require.Equal(t, "raw create", mem.Content)

	emb, err := ds.dbClient.Embedding.Query().Where(embedding.HasMemoryWith(entmemory.ID(mem.ID))).Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, emb)
}

func TestCreateMemory_ChatNotOwnedByUser(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	foreignChatID := uuid.New()
	createTestChat(t, ds, foreignChatID, otherUserID)

	mem, err := ds.CreateMemory(ctx, userID, models.Memory{
		Content: "x",
		Scope:   string(entmemory.ScopeChat),
		ChatID:  foreignChatID,
	}, nil, uuid.Nil)
	require.ErrorIs(t, err, ErrChatNotFound)
	require.Nil(t, mem)
}

func TestCreateMemory_AutoPinsWhenPersonalityHasAutoPinEnabled(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)
	_, err := ds.dbClient.Personality.UpdateOneID(personalityID).SetAutoPinMemories(true).Save(ctx)
	require.NoError(t, err)

	mem, err := ds.CreateMemory(ctx, userID, models.Memory{
		Content: "auto pinned",
		Scope:   "User",
	}, nil, personalityID)
	require.NoError(t, err)
	require.NotNil(t, mem.PinnedPersonalityID)
	require.Equal(t, personalityID, *mem.PinnedPersonalityID)
}

// --- UpdateMemory ---

func TestUpdateMemory_HappyPath(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "original", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	newContent := "updated content"
	updated, err := ds.UpdateMemory(ctx, userID, created.ID, models.MemoryPatch{Content: &newContent})
	require.NoError(t, err)
	require.Equal(t, "updated content", updated.Content)
}

func TestUpdateMemory_NotFound(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	newContent := "x"
	updated, err := ds.UpdateMemory(ctx, userID, uuid.New(), models.MemoryPatch{Content: &newContent})
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Nil(t, updated)
}

func TestUpdateMemory_WrongOwner(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	ownerID := uuid.New()
	createTestUser(t, ds, ownerID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)

	created, err := ds.CreateMemoryFromInput(ctx, ownerID, models.CreateMemoryInput{Content: "mine", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	newContent := "hijacked"
	updated, err := ds.UpdateMemory(ctx, otherUserID, created.ID, models.MemoryPatch{Content: &newContent})
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Nil(t, updated)
}

func TestUpdateMemory_EmptyContentRejected(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "original", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	blank := "   "
	updated, err := ds.UpdateMemory(ctx, userID, created.ID, models.MemoryPatch{Content: &blank})
	require.Error(t, err)
	require.Nil(t, updated)
}

// --- UpdateMemoryPin ---

func TestUpdateMemoryPin_HappyPath(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "pin me", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	updated, err := ds.UpdateMemoryPin(ctx, userID, created.ID, &personalityID)
	require.NoError(t, err)
	require.NotNil(t, updated.PinnedPersonalityID)
	require.Equal(t, personalityID, *updated.PinnedPersonalityID)

	// Unpin.
	updated, err = ds.UpdateMemoryPin(ctx, userID, created.ID, nil)
	require.NoError(t, err)
	require.Nil(t, updated.PinnedPersonalityID)
}

func TestUpdateMemoryPin_NotFound(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	updated, err := ds.UpdateMemoryPin(ctx, userID, uuid.New(), nil)
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Nil(t, updated)
}

func TestUpdateMemoryPin_RejectsChatScopedMemory(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "thread memory", Level: models.MemoryLevelThread, ChatID: &chatID})
	require.NoError(t, err)

	updated, err := ds.UpdateMemoryPin(ctx, userID, created.ID, nil)
	require.Error(t, err)
	require.Nil(t, updated)
}

func TestUpdateMemoryPin_PersonalityNotOwnedByUser(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	foreignPersonalityID := uuid.New()
	createTestPersonality(t, ds, foreignPersonalityID, otherUserID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "pin me", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	updated, err := ds.UpdateMemoryPin(ctx, userID, created.ID, &foreignPersonalityID)
	require.Error(t, err)
	require.Nil(t, updated)
}

// --- DeleteMemory ---

func TestDeleteMemory_HappyPath(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "delete me", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	require.NoError(t, ds.DeleteMemory(ctx, userID, created.ID))

	exists, err := ds.dbClient.Memory.Query().Where(entmemory.ID(created.ID)).Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestDeleteMemory_NotFound(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	err := ds.DeleteMemory(ctx, userID, uuid.New())
	require.ErrorIs(t, err, ErrMemoryNotFound)
}

func TestDeleteMemory_WrongOwner(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	ownerID := uuid.New()
	createTestUser(t, ds, ownerID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)

	created, err := ds.CreateMemoryFromInput(ctx, ownerID, models.CreateMemoryInput{Content: "mine", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	err = ds.DeleteMemory(ctx, otherUserID, created.ID)
	require.ErrorIs(t, err, ErrMemoryNotFound)

	exists, err := ds.dbClient.Memory.Query().Where(entmemory.ID(created.ID)).Exist(ctx)
	require.NoError(t, err)
	require.True(t, exists, "delete by a non-owner must not remove the memory")
}

// --- GetMemory ---

func TestGetMemory_HappyPath(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "find me", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	got, err := ds.GetMemory(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, "find me", got.Content)
}

func TestGetMemory_NotFound(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	got, err := ds.GetMemory(ctx, userID, uuid.New())
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Nil(t, got)
}

func TestGetMemory_WrongOwner(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	ownerID := uuid.New()
	createTestUser(t, ds, ownerID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)

	created, err := ds.CreateMemoryFromInput(ctx, ownerID, models.CreateMemoryInput{Content: "mine", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	got, err := ds.GetMemory(ctx, otherUserID, created.ID)
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Nil(t, got)
}

func TestGetMemory_InactiveNotReturned(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "inactive", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	inactive := models.MemoryStatusInactive
	_, err = ds.UpdateMemory(ctx, userID, created.ID, models.MemoryPatch{Status: &inactive})
	require.NoError(t, err)

	got, err := ds.GetMemory(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrMemoryNotFound)
	require.Nil(t, got)
}

// --- GetMemoryByIDPrefix ---

func TestGetMemoryByIDPrefix(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "prefix me", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	prefix := strings.ReplaceAll(created.ID.String(), "-", "")[:8]

	got, err := ds.GetMemoryByIDPrefix(ctx, userID, prefix)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	// Not found: valid-looking prefix that matches nothing.
	_, err = ds.GetMemoryByIDPrefix(ctx, userID, "ffffffff")
	require.ErrorIs(t, err, ErrMemoryNotFound)

	// Invalid prefix (too short) surfaces memoryIDPrefixBounds' error.
	_, err = ds.GetMemoryByIDPrefix(ctx, userID, "abc")
	require.Error(t, err)
}

func TestGetMemoryByIDPrefix_Ambiguous(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	// Craft two memory IDs sharing an 8-hex-digit prefix.
	base := "aaaaaaaa"
	id1 := uuid.MustParse(base + "-0000-0000-0000-000000000001")
	id2 := uuid.MustParse(base + "-0000-0000-0000-000000000002")
	for _, id := range []uuid.UUID{id1, id2} {
		_, err := ds.dbClient.Memory.Create().
			SetID(id).
			SetContent("ambiguous").
			SetScope(entmemory.ScopeUser).
			SetOwnerID(userID).
			SetCreatedAt(time.Now()).
			Save(ctx)
		require.NoError(t, err)
	}

	_, err := ds.GetMemoryByIDPrefix(ctx, userID, base)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")
}

// --- ListMemories ---

func TestListMemories_FiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)

	globalMem, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "global memory", Level: models.MemoryLevelGlobal, Starred: true})
	require.NoError(t, err)
	_, err = ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "thread memory", Level: models.MemoryLevelThread, ChatID: &chatID})
	require.NoError(t, err)
	_, err = ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "personality memory", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &personalityID})
	require.NoError(t, err)

	// No filters, default status active — all three returned.
	resp, err := ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.TotalCount)
	require.Len(t, resp.Results, 3)

	// Filter by level=global.
	globalLevel := models.MemoryLevelGlobal
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Level: &globalLevel})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Equal(t, globalMem.ID, resp.Results[0].(*models.Memory).ID)

	// Filter by chat_id.
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{ChatID: &chatID})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)

	// Filter by starred.
	starred := true
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Starred: &starred})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)

	// Filter by query substring.
	query := "thread"
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Query: &query})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)

	// Pagination: pageSize=1 across 3 results.
	resp, err = ds.ListMemories(ctx, userID, 1, 1, models.MemoryFilters{})
	require.NoError(t, err)
	require.Equal(t, 3, resp.TotalCount)
	require.Len(t, resp.Results, 1)
	require.Equal(t, 1, resp.Page)

	resp, err = ds.ListMemories(ctx, userID, 2, 1, models.MemoryFilters{})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.Equal(t, 2, resp.Page)

	// Invalid level filter returns an error.
	badLevel := models.MemoryLevel("bogus")
	_, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Level: &badLevel})
	require.Error(t, err)
}

func TestListMemories_DefaultsAndSort(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	first, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "first", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	time.Sleep(time.Millisecond) // ensure distinct created_at ordering on fast filesystems
	second, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "second", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	// Non-positive page/pageSize default to 1/10.
	resp, err := ds.ListMemories(ctx, userID, 0, 0, models.MemoryFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Page)
	require.Len(t, resp.Results, 2)

	// created_asc sort.
	ascSort := models.MemorySortCreatedAsc
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{Sort: &ascSort})
	require.NoError(t, err)
	require.Equal(t, first.ID, resp.Results[0].(*models.Memory).ID)
	require.Equal(t, second.ID, resp.Results[1].(*models.Memory).ID)

	// created_desc (default) sort.
	resp, err = ds.ListMemories(ctx, userID, 1, 10, models.MemoryFilters{})
	require.NoError(t, err)
	require.Equal(t, second.ID, resp.Results[0].(*models.Memory).ID)
	require.Equal(t, first.ID, resp.Results[1].(*models.Memory).ID)
}

// --- UpsertChatSummaryMemory / GetChatSummaryMemory / ListChatsMissingSummaryMemory ---

func TestUpsertChatSummaryMemory_CreatesThenUpdates(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, chatID, "first summary", []float32{0.1, 0.2}))

	got, err := ds.GetChatSummaryMemory(ctx, userID, chatID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "first summary", got.Content)

	// A second upsert updates the singleton in place rather than creating another row.
	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, chatID, "second summary", []float32{0.3, 0.4}))

	got, err = ds.GetChatSummaryMemory(ctx, userID, chatID)
	require.NoError(t, err)
	require.Equal(t, "second summary", got.Content)

	count, err := ds.dbClient.Memory.Query().Where(entmemory.ScopeEQ(entmemory.ScopeSummary)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "upsert must not create a second summary memory")
}

func TestUpsertChatSummaryMemory_EmptySummaryRejected(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	err := ds.UpsertChatSummaryMemory(ctx, userID, chatID, "   ", nil)
	require.Error(t, err)
}

func TestUpsertChatSummaryMemory_ChatNotOwnedByUser(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	foreignChatID := uuid.New()
	createTestChat(t, ds, foreignChatID, otherUserID)

	err := ds.UpsertChatSummaryMemory(ctx, userID, foreignChatID, "summary", nil)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestGetChatSummaryMemory_NotFoundReturnsNilNil(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	got, err := ds.GetChatSummaryMemory(ctx, userID, chatID)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestListChatsMissingSummaryMemory(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	// Chat with a legacy checkpoint summary but no Summary memory yet: a candidate.
	candidateChatID := uuid.New()
	_, err := ds.dbClient.Chat.Create().
		SetID(candidateChatID).
		SetName("needs backfill").
		SetOwnerID(userID).
		SetCheckpointSummary("legacy summary text").
		Save(ctx)
	require.NoError(t, err)

	// Chat with no checkpoint summary at all: not a candidate.
	noSummaryChatID := uuid.New()
	createTestChat(t, ds, noSummaryChatID, userID)

	// Chat with a checkpoint summary AND an existing Summary memory: not a candidate.
	backfilledChatID := uuid.New()
	_, err = ds.dbClient.Chat.Create().
		SetID(backfilledChatID).
		SetName("already backfilled").
		SetOwnerID(userID).
		SetCheckpointSummary("already migrated").
		Save(ctx)
	require.NoError(t, err)
	require.NoError(t, ds.UpsertChatSummaryMemory(ctx, userID, backfilledChatID, "already migrated", nil))

	candidates, err := ds.ListChatsMissingSummaryMemory(ctx, 0)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, candidateChatID, candidates[0].ChatID)
	require.Equal(t, userID, candidates[0].UserID)
	require.Equal(t, "legacy summary text", candidates[0].Summary)
}

// --- chatOwnedByUser / personalityOwnedByUser ---

func TestChatOwnedByUser(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)

	tx, err := ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()

	owned, err := ds.chatOwnedByUser(ctx, tx, userID, chatID)
	require.NoError(t, err)
	require.True(t, owned)

	owned, err = ds.chatOwnedByUser(ctx, tx, otherUserID, chatID)
	require.NoError(t, err)
	require.False(t, owned)

	owned, err = ds.chatOwnedByUser(ctx, tx, userID, uuid.Nil)
	require.NoError(t, err)
	require.False(t, owned, "nil chat ID short-circuits to false")

	owned, err = ds.chatOwnedByUser(ctx, tx, userID, uuid.New())
	require.NoError(t, err)
	require.False(t, owned, "nonexistent chat ID returns false")
}

func TestPersonalityOwnedByUser(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)

	tx, err := ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()

	owned, err := ds.personalityOwnedByUser(ctx, tx, userID, personalityID)
	require.NoError(t, err)
	require.True(t, owned)

	owned, err = ds.personalityOwnedByUser(ctx, tx, otherUserID, personalityID)
	require.NoError(t, err)
	require.False(t, owned)

	owned, err = ds.personalityOwnedByUser(ctx, tx, userID, uuid.Nil)
	require.NoError(t, err)
	require.False(t, owned, "nil personality ID short-circuits to false")
}

// --- existingAnyMemoryIDs / existingChatIDs / existingPersonalityIDs ---

func TestExistingAnyMemoryIDs(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	created, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "exists", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)

	missingID := uuid.New()
	set, err := ds.existingAnyMemoryIDs(ctx, []uuid.UUID{created.ID, missingID})
	require.NoError(t, err)
	require.Len(t, set, 1)
	_, ok := set[created.ID]
	require.True(t, ok)
	_, ok = set[missingID]
	require.False(t, ok)

	// Empty input returns an empty (non-nil) set without querying.
	empty, err := ds.existingAnyMemoryIDs(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestExistingChatIDs(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	ownChatID := uuid.New()
	createTestChat(t, ds, ownChatID, userID)
	foreignChatID := uuid.New()
	createTestChat(t, ds, foreignChatID, otherUserID)

	set, err := ds.existingChatIDs(ctx, userID, []uuid.UUID{ownChatID, foreignChatID, uuid.New()})
	require.NoError(t, err)
	require.Len(t, set, 1)
	_, ok := set[ownChatID]
	require.True(t, ok)

	empty, err := ds.existingChatIDs(ctx, userID, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestExistingPersonalityIDs(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	otherUserID := uuid.New()
	createTestUser(t, ds, otherUserID)
	ownPersonalityID := uuid.New()
	createTestPersonality(t, ds, ownPersonalityID, userID)
	foreignPersonalityID := uuid.New()
	createTestPersonality(t, ds, foreignPersonalityID, otherUserID)

	set, err := ds.existingPersonalityIDs(ctx, userID, []uuid.UUID{ownPersonalityID, foreignPersonalityID, uuid.New()})
	require.NoError(t, err)
	require.Len(t, set, 1)
	_, ok := set[ownPersonalityID]
	require.True(t, ok)

	empty, err := ds.existingPersonalityIDs(ctx, userID, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

// --- ExportMemories ---

func TestExportMemories_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)
	chatID := uuid.New()
	createTestChat(t, ds, chatID, userID)
	personalityID := uuid.New()
	createTestPersonality(t, ds, personalityID, userID)

	_, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "thread memory", Level: models.MemoryLevelThread, ChatID: &chatID})
	require.NoError(t, err)
	_, err = ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "global memory", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	_, err = ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "personality memory", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &personalityID})
	require.NoError(t, err)
	// An inactive memory must be excluded from the export.
	inactive, err := ds.CreateMemoryFromInput(ctx, userID, models.CreateMemoryInput{Content: "inactive memory", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	inactiveStatus := models.MemoryStatusInactive
	_, err = ds.UpdateMemory(ctx, userID, inactive.ID, models.MemoryPatch{Status: &inactiveStatus})
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, ds.ExportMemories(ctx, userID, &buf))

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, f := range zr.File {
		names[f.Name] = true
	}
	require.True(t, names["chat.json"])
	require.True(t, names["user.json"])
	require.True(t, names["personality-"+personalityID.String()+".json"])

	readJSONL := func(name string) []models.MemoryRecord {
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			rc, err := f.Open()
			require.NoError(t, err)
			defer rc.Close()
			dec := json.NewDecoder(rc)
			var recs []models.MemoryRecord
			for {
				var rec models.MemoryRecord
				if err := dec.Decode(&rec); err != nil {
					break
				}
				recs = append(recs, rec)
			}
			return recs
		}
		return nil
	}

	chatRecs := readJSONL("chat.json")
	require.Len(t, chatRecs, 1)
	require.Equal(t, "thread memory", chatRecs[0].Content)
	require.NotNil(t, chatRecs[0].ChatID)
	require.Equal(t, chatID, *chatRecs[0].ChatID)

	userRecs := readJSONL("user.json")
	require.Len(t, userRecs, 1, "only the unpinned global memory belongs in user.json; the inactive one is excluded")
	require.Equal(t, "global memory", userRecs[0].Content)

	personalityRecs := readJSONL("personality-" + personalityID.String() + ".json")
	require.Len(t, personalityRecs, 1)
	require.Equal(t, "personality memory", personalityRecs[0].Content)
}

// --- GetRelatedMemories / GetRelatedSummaryMemories ---
//
// Both use PostgreSQL-only pgvector SQL (the "<->" distance operator embedded via
// sql.ExprP/sql.Expr) to rank by vector distance. SQLite has no such operator, so these
// tests can only exercise the tx/predicate setup and the error-propagation path — the
// actual similarity ranking is covered indirectly by embeddingCosineRelevance's own tests
// and is otherwise only verifiable against real Postgres. This documents that these two
// functions are not fully unit-testable under the current sqlite harness.

func TestGetRelatedMemories_PropagatesQueryError(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	_, err := ds.GetRelatedMemories(ctx, userID, uuid.New(), []float32{0.1, 0.2}, uuid.Nil)
	require.Error(t, err, "sqlite does not support the pgvector '<->' operator memory.go embeds via sql.ExprP")
}

func TestGetRelatedSummaryMemories_PropagatesQueryError(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newMemoryTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createTestUser(t, ds, userID)

	_, err := ds.GetRelatedSummaryMemories(ctx, userID, []float32{0.1, 0.2}, 0)
	require.Error(t, err, "sqlite does not support the pgvector '<->' operator memory.go embeds via sql.ExprP")
}
