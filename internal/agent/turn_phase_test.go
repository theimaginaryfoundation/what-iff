package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func testTurnPhases(w jobProgressWriter) *chatTurnPhases {
	job := testChatJob()
	return &chatTurnPhases{ds: w, logger: zap.NewNop(), userID: job.UserID, jobID: job.ID}
}

func decodedPhases(t *testing.T, w *fakeProgressWriter) []models.ChatTurnPhase {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]models.ChatTurnPhase, 0, len(w.payloads))
	for _, raw := range w.payloads {
		var p models.ChatTurnProgress
		require.NoError(t, json.Unmarshal([]byte(raw), &p))
		assert.NotNil(t, p.ToolCalls, "tool_calls must encode as [] so clients can read it")
		out = append(out, p.Phase)
	}
	return out
}

func TestLoadTurnMemories_Phases(t *testing.T) {
	t.Parallel()

	found := func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) ([]string, error) {
		return []string{"User likes tea"}, nil
	}
	empty := func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) ([]string, error) {
		return []string{}, nil
	}
	failing := func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) ([]string, error) {
		return nil, errors.New("embeddings api down")
	}
	loadThenInfer := []models.ChatTurnPhase{models.ChatTurnPhaseLoadingMemories, models.ChatTurnPhaseInference}

	tests := []struct {
		name     string
		agent    *Agent
		wantMems []string
		want     []models.ChatTurnPhase
	}{
		{
			name:     "memory enabled with results: loading then inference",
			agent:    &Agent{logger: zap.NewNop(), testHooks: agentTestHooks{GetMemoriesOverride: found}},
			wantMems: []string{"User likes tea"},
			want:     loadThenInfer,
		},
		{
			name:     "memory enabled but nothing stored: still loading then inference",
			agent:    &Agent{logger: zap.NewNop(), testHooks: agentTestHooks{GetMemoriesOverride: empty}},
			wantMems: []string{},
			want:     loadThenInfer,
		},
		{
			name:     "retrieval fails: the phase still clears to inference",
			agent:    &Agent{logger: zap.NewNop(), testHooks: agentTestHooks{GetMemoriesOverride: failing}},
			wantMems: []string{},
			want:     loadThenInfer,
		},
		{
			name:  "memory disabled (mock backend skips retrieval): no phase written",
			agent: &Agent{logger: zap.NewNop(), mockLLM: true},
			want:  []models.ChatTurnPhase{},
		},
		{
			name:  "memory disabled (local backend skips retrieval): no phase written",
			agent: &Agent{logger: zap.NewNop(), localLLM: true},
			want:  []models.ChatTurnPhase{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := &fakeProgressWriter{}
			memories, _, _ := tt.agent.loadTurnMemories(context.Background(), testTurnPhases(w), uuid.New(), uuid.New(), uuid.New(), "hi")
			assert.Equal(t, tt.wantMems, memories)
			assert.Equal(t, tt.want, decodedPhases(t, w))
		})
	}
}

func TestLoadTurnMemories_NilPhasesIsSafe(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop(), testHooks: agentTestHooks{
		GetMemoriesOverride: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) ([]string, error) {
			return []string{"m1"}, nil
		},
	}}
	memories, _, failed := a.loadTurnMemories(context.Background(), nil, uuid.New(), uuid.New(), uuid.New(), "hi")
	assert.False(t, failed)
	assert.Equal(t, []string{"m1"}, memories)
}

func TestChatTurnPhases_FailedLoadingWriteSkipsInferenceWrite(t *testing.T) {
	t.Parallel()
	w := &fakeProgressWriter{err: errors.New("db down")}
	p := testTurnPhases(w)

	p.LoadingMemories(context.Background())
	p.MemoriesLoaded(context.Background())

	// The loading write was attempted and failed, so nothing claims the loading phase and
	// there is nothing to clear.
	assert.Equal(t, 1, w.count())
}

func TestChatTurnPhases_WritesSurviveTurnCancellation(t *testing.T) {
	t.Parallel()
	w := &fakeProgressWriter{}
	p := testTurnPhases(w)
	ctx, cancel := context.WithCancel(context.Background())
	p.LoadingMemories(ctx)
	cancel()
	p.MemoriesLoaded(ctx)
	assert.Equal(t, []models.ChatTurnPhase{models.ChatTurnPhaseLoadingMemories, models.ChatTurnPhaseInference}, decodedPhases(t, w))
}

func TestJobToolProgress_SnapshotsCarryInferencePhase(t *testing.T) {
	t.Parallel()
	w := &fakeProgressWriter{}
	p := newJobToolProgress(context.Background(), w, zap.NewNop(), testChatJob(), nil)
	p.Started(0, provider.ToolUse{ID: "call-1", Name: "find_context"})
	p.Close()
	// A tool write replaces the whole payload, so it must keep the turn out of loading_memories.
	assert.Equal(t, models.ChatTurnPhaseInference, w.latest(t).Phase)
}
