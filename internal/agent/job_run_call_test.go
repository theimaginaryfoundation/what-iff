package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestJobRunToolCall(t *testing.T) {
	t.Parallel()
	require.Nil(t, jobRunToolCall(context.Background(), "hi"), "a turn for no job (webhook, welcome) has none")

	jobID := uuid.New()
	ctx := models.WithJobRunSource(context.Background(), models.JobRunSource{AgentJobID: jobID, Title: " Weekly meal planning "})
	call := jobRunToolCall(ctx, "Plan this week's meals.")
	require.NotNil(t, call)
	assert.Equal(t, jobRunToolCallName, call.ToolName)
	assert.Equal(t, "Plan this week's meals.", call.ToolOutput, "the prompt, which is never saved as a message")

	var input jobRunToolInput
	require.NoError(t, json.Unmarshal([]byte(call.ToolInput), &input))
	assert.Equal(t, jobRunToolInput{AgentJobID: jobID.String(), Title: "Weekly meal planning"}, input)
}

func TestJobRunToolCall_NeverCarriedIntoLaterTurns(t *testing.T) {
	t.Parallel()
	ctx := models.WithJobRunSource(context.Background(), models.JobRunSource{AgentJobID: uuid.New(), Title: "t"})
	call := jobRunToolCall(ctx, "secret-ish prompt")
	_, ok := formatPersistedToolResult(call, 1)
	assert.False(t, ok)
	assert.Empty(t, selectPersistedToolResults([]*models.ChatMessage{{ToolCalls: []*models.ToolCall{call}}}))
}

func TestLeadToolCallsForChatContext(t *testing.T) {
	t.Parallel()
	ctx := models.WithJobRunSource(context.Background(), models.JobRunSource{AgentJobID: uuid.New(), Title: "t"})
	chatCtx := &chatContext{memories: []string{"User likes tea"}, jobRunCall: jobRunToolCall(ctx, "p")}

	got := leadToolCallsForChatContext(chatCtx)
	require.Len(t, got, 2)
	assert.Equal(t, jobRunToolCallName, got[0].ToolName, "the run_job call opens the turn")
	assert.Equal(t, memoryEnrichmentToolCallName, got[1].ToolName)

	chatCtx.jobRunCall = nil
	got = leadToolCallsForChatContext(chatCtx)
	require.Len(t, got, 1)
	assert.Equal(t, memoryEnrichmentToolCallName, got[0].ToolName)
	assert.Nil(t, leadToolCallsForChatContext(nil))
}

func TestMemoryLoadProgress_LeadRowStaysAboveTheMemoryRow(t *testing.T) {
	t.Parallel()
	w := &fakeProgressWriter{}
	p := testMemoryProgress(w)
	ctx := models.WithJobRunSource(context.Background(), models.JobRunSource{AgentJobID: uuid.New(), Title: "t"})
	lead := jobRunProgressEntry(jobRunToolCall(ctx, "p"), time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))

	p.Lead(context.Background(), lead)
	got := writes(t, w)
	require.Len(t, got, 1, "the run_job row shows as soon as the run starts")
	require.Len(t, got[0].ToolCalls, 1)
	assert.Equal(t, jobRunToolCallName, got[0].ToolCalls[0].Name)
	assert.Equal(t, models.ChatTurnToolComplete, got[0].ToolCalls[0].Status)

	p.Started(context.Background())
	p.Finished(context.Background(), []string{"m1"}, false)
	entries := p.Entries()
	require.Len(t, entries, 2)
	assert.Equal(t, jobRunToolCallName, entries[0].Name)
	assert.Equal(t, memoryEnrichmentToolCallName, entries[1].Name)

	// Retrieval that finds nothing drops only the memory row.
	q := testMemoryProgress(&fakeProgressWriter{})
	q.Lead(context.Background(), lead)
	q.Started(context.Background())
	q.Finished(context.Background(), nil, false)
	require.Len(t, q.Entries(), 1)
	assert.Equal(t, jobRunToolCallName, q.Entries()[0].Name)
}
