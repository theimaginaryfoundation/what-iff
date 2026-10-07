package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func testMemoryProgress(w jobProgressWriter) *memoryLoadProgress {
	job := testChatJob()
	clock := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	return &memoryLoadProgress{
		ds: w, logger: zap.NewNop(), userID: job.UserID, jobID: job.ID,
		now: func() time.Time { clock = clock.Add(time.Second); return clock },
	}
}

// writes decodes every progress payload written so far.
func writes(t *testing.T, w *fakeProgressWriter) []models.ChatTurnProgress {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]models.ChatTurnProgress, 0, len(w.payloads))
	for _, raw := range w.payloads {
		var p models.ChatTurnProgress
		require.NoError(t, json.Unmarshal([]byte(raw), &p))
		require.NotNil(t, p.ToolCalls, "tool_calls must encode as [] so clients can read it")
		out = append(out, p)
	}
	return out
}

func memoryHook(mems []string, err error) agentTestHooks {
	return agentTestHooks{GetMemoriesOverride: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) ([]string, error) {
		return mems, err
	}}
}

func TestLoadTurnMemories_TimelineRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		agent *Agent
		// wantStatuses is the memory row (if any) each successive write shows.
		wantStatuses [][]models.ChatTurnToolStatus
		wantOutput   string
		wantSeed     bool
	}{
		{
			name:         "memories found: running, then complete with the payload",
			agent:        &Agent{logger: zap.NewNop(), testHooks: memoryHook([]string{"User likes tea"}, nil)},
			wantStatuses: [][]models.ChatTurnToolStatus{{models.ChatTurnToolRunning}, {models.ChatTurnToolComplete}},
			wantOutput:   "Retrieved memories:\n\n User likes tea",
			wantSeed:     true,
		},
		{
			name:         "retrieval fails: running, then error",
			agent:        &Agent{logger: zap.NewNop(), testHooks: memoryHook(nil, errors.New("embeddings api down"))},
			wantStatuses: [][]models.ChatTurnToolStatus{{models.ChatTurnToolRunning}, {models.ChatTurnToolError}},
			wantOutput:   memoryEnrichmentFailureMessage,
			wantSeed:     true,
		},
		{
			name:         "nothing stored: the row appears, then clears like the saved reply's",
			agent:        &Agent{logger: zap.NewNop(), testHooks: memoryHook([]string{}, nil)},
			wantStatuses: [][]models.ChatTurnToolStatus{{models.ChatTurnToolRunning}, {}},
		},
		{
			name:         "mock backend skips retrieval: no row",
			agent:        &Agent{logger: zap.NewNop(), mockLLM: true},
			wantStatuses: [][]models.ChatTurnToolStatus{},
		},
		{
			name:         "local backend skips retrieval: no row",
			agent:        &Agent{logger: zap.NewNop(), localLLM: true},
			wantStatuses: [][]models.ChatTurnToolStatus{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := &fakeProgressWriter{}
			progress := testMemoryProgress(w)
			tt.agent.loadTurnMemories(context.Background(), progress, uuid.New(), uuid.New(), uuid.New(), "hi", false)

			got := writes(t, w)
			statuses := make([][]models.ChatTurnToolStatus, 0, len(got))
			for _, p := range got {
				row := []models.ChatTurnToolStatus{}
				for _, c := range p.ToolCalls {
					assert.Equal(t, memoryEnrichmentToolCallName, c.Name)
					row = append(row, c.Status)
				}
				statuses = append(statuses, row)
			}
			assert.Equal(t, tt.wantStatuses, statuses)

			if tt.wantSeed {
				seed := progress.Entries()
				require.Len(t, seed, 1)
				assert.Equal(t, tt.wantOutput, seed[0].Output)
				require.NotNil(t, seed[0].FinishedAt)
				assert.True(t, seed[0].FinishedAt.After(seed[0].StartedAt))
			} else {
				assert.Empty(t, progress.Entries())
			}
		})
	}
}

func TestLoadTurnMemories_NilProgressIsSafe(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop(), testHooks: memoryHook([]string{"m1"}, nil)}
	memories, _, failed := a.loadTurnMemories(context.Background(), nil, uuid.New(), uuid.New(), uuid.New(), "hi", false)
	assert.False(t, failed)
	assert.Equal(t, []string{"m1"}, memories)
}

func TestMemoryLoadProgress_EmptyResultAfterFailedWriteWritesNothingMore(t *testing.T) {
	t.Parallel()
	w := &fakeProgressWriter{err: errors.New("db down")}
	p := testMemoryProgress(w)

	p.Started(context.Background())
	p.Finished(context.Background(), nil, false)

	// The running row never reached the job, so there is nothing to clear.
	assert.Equal(t, 1, w.count())
}

func TestMemoryLoadProgress_PreviewIsCapped(t *testing.T) {
	t.Parallel()
	w := &fakeProgressWriter{}
	p := testMemoryProgress(w)
	long := make([]byte, 3*memoryProgressPreviewRunes)
	for i := range long {
		long[i] = 'x'
	}

	p.Started(context.Background())
	p.Finished(context.Background(), []string{string(long)}, false)

	// TruncateRunes marks a cut with one extra rune.
	assert.LessOrEqual(t, len([]rune(p.Entries()[0].Output)), memoryProgressPreviewRunes+1)
}

func TestMemoryLoadProgress_WritesSurviveTurnCancellation(t *testing.T) {
	t.Parallel()
	w := &fakeProgressWriter{}
	p := testMemoryProgress(w)
	ctx, cancel := context.WithCancel(context.Background())
	p.Started(ctx)
	cancel()
	p.Finished(ctx, []string{"m1"}, false)

	got := writes(t, w)
	require.Len(t, got, 2)
	assert.Equal(t, models.ChatTurnToolComplete, got[1].ToolCalls[0].Status)
}

func TestJobToolProgress_SeededRowSurvivesToolSnapshots(t *testing.T) {
	t.Parallel()
	mem := testMemoryProgress(&fakeProgressWriter{})
	mem.Started(context.Background())
	mem.Finished(context.Background(), []string{"User likes tea"}, false)

	w := &fakeProgressWriter{}
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)
	p.Seed(mem.Entries())
	p.Started(0, provider.ToolUse{ID: "call-1", Name: "find_context"})
	p.Close()

	// A tool write replaces the whole payload, so the memory row has to ride along, first.
	calls := w.latest(t).ToolCalls
	require.Len(t, calls, 2)
	assert.Equal(t, memoryEnrichmentToolCallName, calls[0].Name)
	assert.Equal(t, models.ChatTurnToolComplete, calls[0].Status)
	assert.Equal(t, "find_context", calls[1].Name)
}

func TestJobToolProgress_SeedNilAndEmptyAreNoOps(t *testing.T) {
	t.Parallel()
	(*jobToolProgress)(nil).Seed([]models.ChatTurnToolCall{{ID: "x"}})
	w := &fakeProgressWriter{}
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)
	p.Seed(nil)
	p.Close()
	assert.Zero(t, w.count(), "seeding writes nothing itself")
}

func TestMemoryToolCallsPrecedeOtherToolCalls(t *testing.T) {
	t.Parallel()
	chatCtx := &chatContext{memories: []string{"User likes tea"}}
	got := append(memoryToolCallsForChatContext(chatCtx), &models.ToolCall{ToolName: "find_context"})
	require.Len(t, got, 2)
	assert.Equal(t, memoryEnrichmentToolCallName, got[0].ToolName)
}
