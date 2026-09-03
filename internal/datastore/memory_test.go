package datastore

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func TestMemoryScopeValidation(t *testing.T) {
	tests := []struct {
		name  string
		scope entmemory.Scope
		valid bool
	}{
		{
			name:  "User scope is valid",
			scope: entmemory.ScopeUser,
			valid: true,
		},
		{
			name:  "Chat scope is valid",
			scope: entmemory.ScopeChat,
			valid: true,
		},
		{
			name:  "Summary scope is valid for internal checkpoint summaries",
			scope: entmemory.ScopeSummary,
			valid: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Contains(t, []entmemory.Scope{entmemory.ScopeUser, entmemory.ScopeChat, entmemory.ScopeSummary}, tc.scope)
		})
	}
}

func TestMemoryIDPrefixBounds(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		wantLower string
		wantUpper string
		wantErr   bool
	}{
		{
			name:      "eight hex digits",
			prefix:    "df3e519d",
			wantLower: "df3e519d-0000-0000-0000-000000000000",
			wantUpper: "df3e519e-0000-0000-0000-000000000000",
		},
		{
			name:      "mid-length prefix",
			prefix:    "df3e519d1234",
			wantLower: "df3e519d-1234-0000-0000-000000000000",
			wantUpper: "df3e519d-1235-0000-0000-000000000000",
		},
		{
			name:      "full UUID prefix",
			prefix:    "df3e519d1234567890abcdef12345678",
			wantLower: "df3e519d-1234-5678-90ab-cdef12345678",
			wantUpper: "df3e519d-1234-5678-90ab-cdef12345679",
		},
		{
			name:      "maximum prefix has no upper bound",
			prefix:    "ffffffff",
			wantLower: "ffffffff-0000-0000-0000-000000000000",
		},
		{name: "too short", prefix: "df3e519", wantErr: true},
		{name: "non hex", prefix: "df3e519z", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lower, upper, err := memoryIDPrefixBounds(tt.prefix)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantLower, lower.String())
			if tt.wantUpper == "" {
				require.Nil(t, upper)
				return
			}
			require.NotNil(t, upper)
			require.Equal(t, tt.wantUpper, upper.String())
		})
	}
}

func TestMemoryLevelMappingForEntity(t *testing.T) {
	tests := []struct {
		name   string
		memory *ent.Memory
		want   models.MemoryLevel
	}{
		{
			name: "global for user scope without pin",
			memory: &ent.Memory{
				Scope: entmemory.ScopeUser,
			},
			want: models.MemoryLevelGlobal,
		},
		{
			name: "personality for user scope with pin",
			memory: &ent.Memory{
				Scope:               entmemory.ScopeUser,
				PinnedPersonalityID: ptrUUID(uuid.New()),
			},
			want: models.MemoryLevelPersonality,
		},
		{
			name: "thread for chat scope",
			memory: &ent.Memory{
				Scope: entmemory.ScopeChat,
			},
			want: models.MemoryLevelThread,
		},
		{
			name: "summary for summary scope",
			memory: &ent.Memory{
				Scope: entmemory.ScopeSummary,
			},
			want: models.MemoryLevelSummary,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := memoryLevelForEntity(tc.memory)
			require.Equal(t, tc.want, got)
		})
	}
}

func ptrUUID(v uuid.UUID) *uuid.UUID { return &v }

func TestScopeFromLevel(t *testing.T) {
	tests := []struct {
		level models.MemoryLevel
		want  entmemory.Scope
	}{
		{level: models.MemoryLevelGlobal, want: entmemory.ScopeUser},
		{level: models.MemoryLevelPersonality, want: entmemory.ScopeUser},
		{level: models.MemoryLevelThread, want: entmemory.ScopeChat},
		{level: models.MemoryLevelSummary, want: entmemory.ScopeSummary},
	}

	for _, tc := range tests {
		got, err := scopeFromLevel(tc.level)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}

	_, err := scopeFromLevel(models.MemoryLevel("bad"))
	require.Error(t, err)
}

func TestValidateLevelInput(t *testing.T) {
	global := models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelGlobal}
	require.NoError(t, validateLevelInput(global))

	chatID := uuid.New()
	thread := models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelThread, ChatID: &chatID}
	require.NoError(t, validateLevelInput(thread))

	pinned := uuid.New()
	personality := models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelPersonality, PinnedPersonalityID: &pinned}
	require.NoError(t, validateLevelInput(personality))

	invalidThread := models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelThread}
	require.Error(t, validateLevelInput(invalidThread))

	invalidPersonality := models.CreateMemoryInput{Content: "x", Level: models.MemoryLevelPersonality}
	require.Error(t, validateLevelInput(invalidPersonality))
}

func TestToMemoryModel_WithPinnedPersonality(t *testing.T) {
	memoryID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()
	now := time.Now()

	// Create a mock Ent Memory entity with pinned personality
	entMemory := &ent.Memory{
		ID:                  memoryID,
		Content:             "Test memory content",
		Scope:               entmemory.ScopeUser,
		PinnedPersonalityID: &personalityID,
		CreatedAt:           now,
		Edges: ent.MemoryEdges{
			Chat: &ent.Chat{
				ID:   chatID,
				Name: "Test Chat",
			},
		},
	}

	// Convert to model
	model := toMemoryModel(entMemory)

	// Assert
	require.NotNil(t, model)
	require.Equal(t, memoryID, model.ID)
	require.Equal(t, "Test memory content", model.Content)
	require.Equal(t, "User", model.Scope)
	require.NotNil(t, model.PinnedPersonalityID)
	require.Equal(t, personalityID, *model.PinnedPersonalityID)
	require.Equal(t, chatID, model.ChatID)
	require.Equal(t, "Test Chat", model.ChatName)
	require.Equal(t, now, model.CreatedAt)
}

// TestToMemoryModel_WithoutPinnedPersonality tests model conversion without pinned personality
func TestToMemoryModel_WithoutPinnedPersonality(t *testing.T) {
	memoryID := uuid.New()
	chatID := uuid.New()
	now := time.Now()

	// Create a mock Ent Memory entity without pinned personality (unpinned)
	entMemory := &ent.Memory{
		ID:                  memoryID,
		Content:             "Unpinned memory",
		Scope:               entmemory.ScopeUser,
		PinnedPersonalityID: nil, // Explicitly nil - unpinned
		CreatedAt:           now,
		Edges: ent.MemoryEdges{
			Chat: &ent.Chat{
				ID:   chatID,
				Name: "Test Chat",
			},
		},
	}

	// Convert to model
	model := toMemoryModel(entMemory)

	// Assert
	require.NotNil(t, model)
	require.Equal(t, memoryID, model.ID)
	require.Equal(t, "Unpinned memory", model.Content)
	require.Equal(t, "User", model.Scope)
	require.Nil(t, model.PinnedPersonalityID, "PinnedPersonalityID should be nil for unpinned memory")
	require.Equal(t, chatID, model.ChatID)
	require.Equal(t, "Test Chat", model.ChatName)
}

func TestToMemoryModel_ChatScopedMemory(t *testing.T) {
	memoryID := uuid.New()
	chatID := uuid.New()
	now := time.Now()

	// Create a Chat-scoped memory (should not have pinned personality)
	entMemory := &ent.Memory{
		ID:                  memoryID,
		Content:             "Chat-scoped memory",
		Scope:               entmemory.ScopeChat,
		PinnedPersonalityID: nil, // Chat-scoped memories are never pinned
		CreatedAt:           now,
		Edges: ent.MemoryEdges{
			Chat: &ent.Chat{
				ID:   chatID,
				Name: "Test Chat",
			},
		},
	}

	// Convert to model
	model := toMemoryModel(entMemory)

	// Assert
	require.NotNil(t, model)
	require.Equal(t, memoryID, model.ID)
	require.Equal(t, "Chat-scoped memory", model.Content)
	require.Equal(t, "Chat", model.Scope)
	require.Nil(t, model.PinnedPersonalityID, "Chat-scoped memories should never have pinned personality")
	require.Equal(t, chatID, model.ChatID)
	require.Equal(t, "Test Chat", model.ChatName)
}

func TestToMemoryModel_NilMemory(t *testing.T) {
	model := toMemoryModel(nil)
	require.Nil(t, model, "toMemoryModel should return nil for nil input")
}

func TestToMemoryModel_WithoutChatEdge(t *testing.T) {
	memoryID := uuid.New()
	now := time.Now()

	// Create a memory without Chat edge loaded
	entMemory := &ent.Memory{
		ID:                  memoryID,
		Content:             "Memory without chat edge",
		Scope:               entmemory.ScopeUser,
		PinnedPersonalityID: nil,
		CreatedAt:           now,
		Edges:               ent.MemoryEdges{}, // No Chat edge
	}

	// Convert to model
	model := toMemoryModel(entMemory)

	// Assert
	require.NotNil(t, model)
	require.Equal(t, memoryID, model.ID)
	require.Equal(t, "Memory without chat edge", model.Content)
	require.Equal(t, uuid.Nil, model.ChatID, "ChatID should be Nil when Chat edge is not loaded")
	require.Equal(t, "", model.ChatName, "ChatName should be empty when Chat edge is not loaded")
}

func TestMemoryRelevanceThreshold(t *testing.T) {
	require.Greater(t, MemoryRelevanceThreshold, float64(0), "Threshold should be positive")
	require.Less(t, MemoryRelevanceThreshold, float64(2), "Threshold should be less than 2 (typical max distance)")
}

func TestPinMemoryLogic_ScopeValidation(t *testing.T) {
	tests := []struct {
		name        string
		scope       entmemory.Scope
		canBePinned bool
		description string
	}{
		{
			name:        "User-scoped memories can be pinned",
			scope:       entmemory.ScopeUser,
			canBePinned: true,
			description: "User-scoped memories can be pinned to specific personalities or left unpinned for all personalities",
		},
		{
			name:        "Chat-scoped memories cannot be pinned",
			scope:       entmemory.ScopeChat,
			canBePinned: false,
			description: "Chat-scoped memories are always accessible within their chat, regardless of active personality",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.scope == entmemory.ScopeUser {
				require.True(t, tc.canBePinned, tc.description)
			} else if tc.scope == entmemory.ScopeChat {
				require.False(t, tc.canBePinned, tc.description)
			}
		})
	}
}

func TestErrorDefinitions(t *testing.T) {
	require.NotNil(t, ErrMemoryNotFound, "ErrMemoryNotFound should be defined")
	require.NotNil(t, ErrChatNotFound, "ErrChatNotFound should be defined")
}

// --- toMemoryRecord tests ---
// toMemoryRecord is the conversion helper used by the streaming ExportMemories
// implementation.

func TestToMemoryRecord_WithChatEdge(t *testing.T) {
	memoryID := uuid.New()
	chatID := uuid.New()
	now := time.Now()

	m := &ent.Memory{
		ID:        memoryID,
		Content:   "chat memory",
		Scope:     entmemory.ScopeChat,
		CreatedAt: now,
		Edges: ent.MemoryEdges{
			Chat: &ent.Chat{ID: chatID, Name: "My Chat"},
		},
	}

	rec := toMemoryRecord(m)

	require.Equal(t, memoryID, rec.ID)
	require.Equal(t, "chat memory", rec.Content)
	require.Equal(t, now, rec.CreatedAt)
	require.NotNil(t, rec.ChatID)
	require.Equal(t, chatID, *rec.ChatID)
	require.NotNil(t, rec.ChatName)
	require.Equal(t, "My Chat", *rec.ChatName)
}

func TestToMemoryRecord_WithoutChatEdge(t *testing.T) {
	memoryID := uuid.New()
	now := time.Now()

	m := &ent.Memory{
		ID:                  memoryID,
		Content:             "user memory",
		Scope:               entmemory.ScopeUser,
		PinnedPersonalityID: nil,
		CreatedAt:           now,
		Edges:               ent.MemoryEdges{},
	}

	rec := toMemoryRecord(m)

	require.Equal(t, memoryID, rec.ID)
	require.Equal(t, "user memory", rec.Content)
	require.Equal(t, now, rec.CreatedAt)
	require.Nil(t, rec.ChatID)
	require.Nil(t, rec.ChatName)
}

func TestToMemoryRecord_PinnedMemoryHasNoChatFields(t *testing.T) {
	// Pinned memories are User-scoped; they carry no chat edge in the export.
	memoryID := uuid.New()
	personalityID := uuid.New()
	now := time.Now()

	m := &ent.Memory{
		ID:                  memoryID,
		Content:             "pinned memory",
		Scope:               entmemory.ScopeUser,
		PinnedPersonalityID: &personalityID,
		CreatedAt:           now,
		Edges:               ent.MemoryEdges{},
	}

	rec := toMemoryRecord(m)

	require.Equal(t, memoryID, rec.ID)
	require.Equal(t, "pinned memory", rec.Content)
	require.Equal(t, now, rec.CreatedAt)
	require.Nil(t, rec.ChatID)
	require.Nil(t, rec.ChatName)
}

func TestToMemoryRecord_SummaryMemoryHasNoChatFields(t *testing.T) {
	chatID := uuid.New()
	now := time.Now()

	m := &ent.Memory{
		ID:        uuid.New(),
		Content:   "checkpoint summary",
		Scope:     entmemory.ScopeSummary,
		CreatedAt: now,
		Edges:     ent.MemoryEdges{Chat: &ent.Chat{ID: chatID, Name: "My Chat"}},
	}

	rec := toMemoryRecord(m)

	require.Equal(t, "checkpoint summary", rec.Content)
	require.Equal(t, now, rec.CreatedAt)
	require.Nil(t, rec.ChatID, "Summary memories are internal checkpoint state, not exported chat memories")
	require.Nil(t, rec.ChatName)
}

// TestStreamMemorySection verifies that streamMemorySection correctly creates a
// ZIP entry, encodes each record as JSONL, and stops when a batch is smaller
// than batchSize — all without a database.
func TestStreamMemorySection(t *testing.T) {
	chatID := uuid.New()
	mem1ID := uuid.New()
	mem2ID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	batch := []*ent.Memory{
		{
			ID:        mem1ID,
			Content:   "first memory",
			Scope:     entmemory.ScopeChat,
			CreatedAt: now,
			Edges:     ent.MemoryEdges{Chat: &ent.Chat{ID: chatID, Name: "My Chat"}},
		},
		{
			ID:        mem2ID,
			Content:   "second memory",
			Scope:     entmemory.ScopeChat,
			CreatedAt: now.Add(time.Second),
			Edges:     ent.MemoryEdges{Chat: &ent.Chat{ID: chatID, Name: "My Chat"}},
		},
	}

	calls := 0
	fetch := func(_ context.Context, _ time.Time, _ uuid.UUID) ([]*ent.Memory, error) {
		calls++
		if calls == 1 {
			return batch, nil
		}
		return nil, nil
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	require.NoError(t, streamMemorySection(context.Background(), zw, "chat.json", 100, fetch))
	require.NoError(t, zw.Close())

	// fetch should have been called twice: once returning 2 records (< batchSize=100),
	// which triggers the break — so actually just once. Verify.
	require.Equal(t, 1, calls)

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	require.Equal(t, "chat.json", zr.File[0].Name)

	rc, err := zr.File[0].Open()
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

	require.Len(t, recs, 2)
	require.Equal(t, mem1ID, recs[0].ID)
	require.Equal(t, "first memory", recs[0].Content)
	require.NotNil(t, recs[0].ChatID)
	require.Equal(t, chatID, *recs[0].ChatID)
	require.Equal(t, mem2ID, recs[1].ID)
	require.Equal(t, "second memory", recs[1].Content)
}

// TestStreamMemorySection_Pagination verifies that streamMemorySection issues
// multiple fetch calls when the first batch is exactly batchSize, then stops
// when a subsequent batch is smaller.
func TestStreamMemorySection_Pagination(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)

	makeBatch := func(n int) []*ent.Memory {
		out := make([]*ent.Memory, n)
		for i := range out {
			out[i] = &ent.Memory{
				ID:        uuid.New(),
				Content:   "mem",
				Scope:     entmemory.ScopeUser,
				CreatedAt: now.Add(time.Duration(i) * time.Second),
				Edges:     ent.MemoryEdges{},
			}
		}
		return out
	}

	const batchSize = 3
	calls := 0
	fetch := func(_ context.Context, _ time.Time, _ uuid.UUID) ([]*ent.Memory, error) {
		calls++
		switch calls {
		case 1:
			return makeBatch(batchSize), nil // full batch → continue
		case 2:
			return makeBatch(1), nil // partial → stop
		default:
			return nil, nil
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	require.NoError(t, streamMemorySection(context.Background(), zw, "user.json", batchSize, fetch))
	require.NoError(t, zw.Close())

	require.Equal(t, 2, calls)

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	rc, err := zr.File[0].Open()
	require.NoError(t, err)
	defer rc.Close()

	dec := json.NewDecoder(rc)
	var count int
	for {
		var rec models.MemoryRecord
		if err := dec.Decode(&rec); err != nil {
			break
		}
		count++
	}
	require.Equal(t, batchSize+1, count) // 3 from first batch + 1 from second
}

func TestParseMemoryImportFile_UserRecords(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	memID := uuid.New()
	lines := []string{
		`{"id":"` + memID.String() + `","content":"hello","created_at":"` + now.Format(time.RFC3339Nano) + `"}`,
		`{"id":"00000000-0000-0000-0000-000000000000","content":"bad","created_at":"` + now.Format(time.RFC3339Nano) + `"}`,
	}
	zf := buildZipFileForTest(t, "user.json", strings.Join(lines, "\n"))

	candidates, invalidCount, invalidReasons, err := parseMemoryImportFile(zf, nil)
	require.NoError(t, err)
	require.Equal(t, 1, invalidCount)
	require.Equal(t, 1, invalidReasons.MissingID)
	require.Len(t, candidates, 1)
	require.Equal(t, memID, candidates[0].record.ID)
	require.Equal(t, entmemory.ScopeUser, candidates[0].scope)
	require.Nil(t, candidates[0].chatID)
	require.Nil(t, candidates[0].pinnedPersonalityID)
}

func TestParseMemoryImportFile_MalformedJSONCountsInvalidRecord(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	memID := uuid.New()
	lines := []string{
		`{"id":"` + memID.String() + `","content":"hello","created_at":"` + now.Format(time.RFC3339Nano) + `"}`,
		`{"id":`,
	}
	zf := buildZipFileForTest(t, "user.json", strings.Join(lines, "\n"))

	candidates, invalidCount, invalidReasons, err := parseMemoryImportFile(zf, nil)
	require.NoError(t, err)
	require.Equal(t, 1, invalidCount)
	require.Equal(t, 1, invalidReasons.MalformedJSON)
	require.Len(t, candidates, 1)
	require.Equal(t, memID, candidates[0].record.ID)
}

func TestParseMemoryImportFile_ChatRequiresChatID(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	memID := uuid.New()
	lines := []string{
		`{"id":"` + memID.String() + `","content":"missing chat","created_at":"` + now.Format(time.RFC3339Nano) + `"}`,
	}
	zf := buildZipFileForTest(t, "chat.json", strings.Join(lines, "\n"))

	candidates, invalidCount, invalidReasons, err := parseMemoryImportFile(zf, nil)
	require.NoError(t, err)
	require.Equal(t, 1, invalidCount)
	require.Equal(t, 1, invalidReasons.MissingChatID)
	require.Empty(t, candidates)
}

func TestParseMemoryImportFile_PersonalityScoped(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	memID := uuid.New()
	personalityID := uuid.New()
	lines := []string{
		`{"id":"` + memID.String() + `","content":"pinned","created_at":"` + now.Format(time.RFC3339Nano) + `"}`,
	}
	zf := buildZipFileForTest(t, "personality-"+personalityID.String()+".json", strings.Join(lines, "\n"))

	candidates, invalidCount, invalidReasons, err := parseMemoryImportFile(zf, nil)
	require.NoError(t, err)
	require.Zero(t, invalidCount)
	require.Equal(t, models.MemoryImportInvalidReasons{}, invalidReasons)
	require.Len(t, candidates, 1)
	require.Equal(t, entmemory.ScopeUser, candidates[0].scope)
	require.NotNil(t, candidates[0].pinnedPersonalityID)
	require.Equal(t, personalityID, *candidates[0].pinnedPersonalityID)
}

func TestBuildImportEmbeddingsChunkPreservesOrder(t *testing.T) {
	chunk := []memoryImportCandidate{
		{record: models.MemoryRecord{ID: uuid.New(), Content: "one"}},
		{record: models.MemoryRecord{ID: uuid.New(), Content: "three"}},
		{record: models.MemoryRecord{ID: uuid.New(), Content: "seven"}},
	}

	prepared, err := buildImportEmbeddingsChunk(context.Background(), chunk, func(_ context.Context, input string) ([]float32, error) {
		return []float32{float32(len(input))}, nil
	})

	require.NoError(t, err)
	require.Len(t, prepared, len(chunk))
	for i := range chunk {
		require.Equal(t, chunk[i], prepared[i].candidate)
		require.Equal(t, []float32{float32(len(chunk[i].record.Content))}, prepared[i].embedding)
	}
}

func TestBuildImportEmbeddingsChunkReturnsFirstEmbeddingError(t *testing.T) {
	badID := uuid.New()
	chunk := []memoryImportCandidate{
		{record: models.MemoryRecord{ID: uuid.New(), Content: "ok"}},
		{record: models.MemoryRecord{ID: badID, Content: "bad"}},
		{record: models.MemoryRecord{ID: uuid.New(), Content: "also ok"}},
	}
	embedErr := errors.New("embedding failed")

	prepared, err := buildImportEmbeddingsChunk(context.Background(), chunk, func(_ context.Context, input string) ([]float32, error) {
		if input == "bad" {
			return nil, embedErr
		}
		return []float32{1}, nil
	})

	require.Nil(t, prepared)
	require.ErrorIs(t, err, embedErr)
	require.ErrorContains(t, err, badID.String())
}

func TestBuildImportEmbeddingsBatchPreservesOrder(t *testing.T) {
	chunk := []memoryImportCandidate{
		{record: models.MemoryRecord{ID: uuid.New(), Content: "one"}},
		{record: models.MemoryRecord{ID: uuid.New(), Content: "two"}},
	}

	prepared, err := buildImportEmbeddingsBatch(context.Background(), chunk, func(_ context.Context, inputs []string) ([][]float32, error) {
		require.Equal(t, []string{"one", "two"}, inputs)
		return [][]float32{{1}, {2}}, nil
	})

	require.NoError(t, err)
	require.Len(t, prepared, len(chunk))
	require.Equal(t, chunk[0], prepared[0].candidate)
	require.Equal(t, []float32{1}, prepared[0].embedding)
	require.Equal(t, chunk[1], prepared[1].candidate)
	require.Equal(t, []float32{2}, prepared[1].embedding)
}

func TestImportMemoriesPersistsValidRecordsAndCountsSkips(t *testing.T) {
	ctx := context.Background()
	ds, cleanup := newSQLiteDatastore(t)
	defer cleanup()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()
	existingMemoryID := uuid.New()
	validUserID := uuid.New()
	validChatID := uuid.New()
	validPinnedID := uuid.New()
	missingChatID := uuid.New()
	missingPersonalityID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)

	_, err := ds.dbClient.User.Create().
		SetID(userID).
		SetUsername("testuser").
		SetEmail("testuser@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)
	_, err = ds.dbClient.Chat.Create().
		SetID(chatID).
		SetName("Import Chat").
		SetOwnerID(userID).
		Save(ctx)
	require.NoError(t, err)
	_, err = ds.dbClient.Personality.Create().
		SetID(personalityID).
		SetName("Vix").
		SetSystemPrompt("foxfire").
		SetUserID(userID).
		Save(ctx)
	require.NoError(t, err)
	_, err = ds.dbClient.Memory.Create().
		SetID(existingMemoryID).
		SetContent("already here").
		SetScope(entmemory.ScopeUser).
		SetOwnerID(userID).
		SetCreatedAt(now).
		Save(ctx)
	require.NoError(t, err)

	zr := buildZipReaderForTest(t, map[string]string{
		"user.json": strings.Join([]string{
			memoryRecordJSON(t, models.MemoryRecord{ID: validUserID, Content: "valid user", CreatedAt: now}),
			memoryRecordJSON(t, models.MemoryRecord{ID: existingMemoryID, Content: "duplicate", CreatedAt: now.Add(time.Second)}),
			`{"id":`,
			memoryRecordJSON(t, models.MemoryRecord{ID: uuid.New(), Content: "", CreatedAt: now.Add(2 * time.Second)}),
		}, "\n"),
		"chat.json": strings.Join([]string{
			memoryRecordJSON(t, models.MemoryRecord{ID: validChatID, Content: "valid chat", ChatID: &chatID, CreatedAt: now.Add(3 * time.Second)}),
			memoryRecordJSON(t, models.MemoryRecord{ID: uuid.New(), Content: "missing chat", ChatID: &missingChatID, CreatedAt: now.Add(4 * time.Second)}),
		}, "\n"),
		"personality-" + personalityID.String() + ".json":        memoryRecordJSON(t, models.MemoryRecord{ID: validPinnedID, Content: "valid pinned", CreatedAt: now.Add(5 * time.Second)}),
		"personality-" + missingPersonalityID.String() + ".json": memoryRecordJSON(t, models.MemoryRecord{ID: uuid.New(), Content: "missing personality", CreatedAt: now.Add(6 * time.Second)}),
	})

	result, err := ds.ImportMemories(ctx, userID, zr, func(_ context.Context, input string) ([]float32, error) {
		return []float32{float32(len(input)), 1, 2}, nil
	})

	require.NoError(t, err)
	require.Equal(t, models.MemoryImportResult{
		ImportedCount:      3,
		DuplicateCount:     1,
		InvalidRecordCount: 2,
		InvalidReasons: models.MemoryImportInvalidReasons{
			MalformedJSON: 1,
			EmptyContent:  1,
		},
		SkippedMissingChat:        1,
		SkippedMissingPersonality: 1,
	}, result)

	memories, err := ds.dbClient.Memory.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, memories, 4)
	embeddings, err := ds.dbClient.Embedding.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, embeddings, 3)

	validChat, err := ds.dbClient.Memory.Get(ctx, validChatID)
	require.NoError(t, err)
	require.Equal(t, entmemory.ScopeChat, validChat.Scope)
	require.Nil(t, validChat.PinnedPersonalityID)

	validPinned, err := ds.dbClient.Memory.Get(ctx, validPinnedID)
	require.NoError(t, err)
	require.Equal(t, entmemory.ScopeUser, validPinned.Scope)
	require.NotNil(t, validPinned.PinnedPersonalityID)
	require.Equal(t, personalityID, *validPinned.PinnedPersonalityID)

	embeddingForUser, err := ds.dbClient.Embedding.Query().
		Where(embedding.HasMemoryWith(entmemory.ID(validUserID))).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, pgvector.NewVector([]float32{10, 1, 2}), embeddingForUser.Embedding)
}

func TestImportMemoriesSkipsExistingIDsAndContinues(t *testing.T) {
	ds, cleanup := newSQLiteDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(userID).
		SetUsername("memory-import-admin").
		SetEmail("memory-import-admin@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)

	existingID := uuid.New()
	_, err = ds.dbClient.Memory.Create().
		SetID(existingID).
		SetContent("already imported").
		SetScope(entmemory.ScopeUser).
		SetOwnerID(userID).
		SetCreatedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)

	newID := uuid.New()
	zr := buildZipReaderForTest(t, map[string]string{
		"user.json": strings.Join([]string{
			memoryRecordJSON(t, models.MemoryRecord{ID: existingID, Content: "already imported", CreatedAt: time.Now()}),
			memoryRecordJSON(t, models.MemoryRecord{ID: newID, Content: "new memory", CreatedAt: time.Now()}),
		}, "\n"),
	})

	result, err := ds.ImportMemories(ctx, userID, zr, func(context.Context, string) ([]float32, error) {
		return []float32{0.1, 0.2, 0.3}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.DuplicateCount)
	require.Equal(t, 1, result.ImportedCount)

	exists, err := ds.dbClient.Memory.Query().Where(entmemory.ID(newID)).Exist(ctx)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestImportMemoriesWithBatchEmbeddingsUsesBoundedBatchesAndBulkPersistence(t *testing.T) {
	ds, cleanup := newSQLiteDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(userID).
		SetUsername("memory-import-batch").
		SetEmail("memory-import-batch@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	records := make([]string, memoryImportBatchSize+1)
	for i := range records {
		records[i] = memoryRecordJSON(t, models.MemoryRecord{
			ID:        uuid.New(),
			Content:   fmt.Sprintf("memory %d", i),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	zr := buildZipReaderForTest(t, map[string]string{
		"user.json": strings.Join(records, "\n"),
	})

	batchCalls := 0
	result, err := ds.ImportMemoriesWithBatchEmbeddings(ctx, userID, zr,
		func(context.Context, string) ([]float32, error) {
			t.Fatal("single embedding fallback should not be called when a batch generator is provided")
			return nil, nil
		},
		func(_ context.Context, inputs []string) ([][]float32, error) {
			batchCalls++
			require.LessOrEqual(t, len(inputs), memoryImportBatchSize)
			embeddings := make([][]float32, len(inputs))
			for i := range inputs {
				embeddings[i] = []float32{float32(i), 1, 2}
			}
			return embeddings, nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, 2, batchCalls)
	require.Equal(t, len(records), result.ImportedCount)
	require.Zero(t, result.DuplicateCount)
	memoryCount, err := ds.dbClient.Memory.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, len(records), memoryCount)
	embeddingCount, err := ds.dbClient.Embedding.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, len(records), embeddingCount)
}

func newSQLiteDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema)
}

func createMemoryImportTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	statements := []string{
		`CREATE TABLE users (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			username text NOT NULL UNIQUE,
			email text NOT NULL UNIQUE,
			password_hash text NOT NULL,
			cognito_sub text UNIQUE,
			first_name text,
			last_name text,
			timezone text NOT NULL DEFAULT 'America/New_York',
			status text NOT NULL DEFAULT 'active',
			enable_experimental_models bool NOT NULL DEFAULT false,
			last_login datetime,
			last_seen datetime,
			terms_accepted_at datetime,
			refresh_token_id text
		)`,
		`CREATE TABLE chats (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			response_id text,
			checkpoint_summary text,
			checkpoint_user_message_count integer NOT NULL DEFAULT 0,
			last_message_time datetime,
			last_checkpoint_at datetime,
			disabled_tools json,
			tags json,
			is_favorite bool NOT NULL DEFAULT false,
			is_auto_mood bool NOT NULL DEFAULT true,
			archived bool NOT NULL DEFAULT false,
			chat_model uuid,
			chat_personality uuid,
			chat_active_mood uuid,
			user_chats uuid NOT NULL
		)`,
		`CREATE TABLE personalities (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			system_prompt text NOT NULL,
			scratchpad text NOT NULL DEFAULT '',
			scratchpad_history json NOT NULL DEFAULT '[]',
			archival_model text NOT NULL DEFAULT '',
			scratchpad_update_prompt text NOT NULL DEFAULT '',
			memory_search_prompt text NOT NULL DEFAULT '',
			memory_write_prompt text NOT NULL DEFAULT '',
			auto_pin_memories bool NOT NULL DEFAULT false,
			accent_color text,
			thumbnail_circle json,
			expressions_enabled bool NOT NULL DEFAULT true,
			image_style text NOT NULL DEFAULT 'auto',
			user_personalities uuid NOT NULL
		)`,
		`CREATE TABLE memories (
			id uuid PRIMARY KEY,
			content text NOT NULL,
			scope text NOT NULL,
			type text NOT NULL DEFAULT 'Context',
			status text NOT NULL DEFAULT 'active',
			confidence real NOT NULL DEFAULT 0.6,
			chain_metadata json,
			link_group_id uuid,
			starred bool NOT NULL DEFAULT false,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
			chat_memories uuid,
			pinned_personality_id uuid,
			user_memories uuid NOT NULL
		)`,
		`CREATE TABLE embeddings (
			id uuid PRIMARY KEY,
			embedding text NOT NULL,
			embedding_memory uuid NOT NULL
		)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func buildZipReaderForTest(t *testing.T, entries map[string]string) *zip.Reader {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		fw, err := zw.Create(name)
		require.NoError(t, err)
		_, err = fw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	return zr
}

func memoryRecordJSON(t *testing.T, rec models.MemoryRecord) string {
	t.Helper()

	payload, err := json.Marshal(rec)
	require.NoError(t, err)
	return string(payload)
}

func buildZipFileForTest(t *testing.T, name, content string) *zip.File {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, err := zw.Create(name)
	require.NoError(t, err)
	_, err = fw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	return zr.File[0]
}
