package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const (
	// jobRunToolCallName is the display-only pseudo tool call that opens a scheduled job's reply
	// (ADR 0x040): it names the job and carries its prompt, which is never saved as a message.
	// Like "Load Memory" it is not a real tool, and toolContextPolicyFor keeps it out of later
	// turns' context, so the prompt stays out of the thread's history as before.
	jobRunToolCallName = "run_job"
	// jobRunProgressToolID identifies the run_job row in a turn's live tool timeline.
	jobRunProgressToolID = "job-run"
)

// jobRunToolInput is the run_job call's input: the job it ran for. The web app reads it to link
// the reply to its job.
type jobRunToolInput struct {
	AgentJobID string `json:"agent_job_id"`
	Title      string `json:"title"`
}

// jobRunToolCall is the run_job call for a turn ctx runs for an agent job, or nil when it runs
// for none (a webhook or welcome prompt).
func jobRunToolCall(ctx context.Context, prompt string) *models.ToolCall {
	src, ok := models.JobRunSourceFromContext(ctx)
	if !ok {
		return nil
	}
	input, err := json.Marshal(jobRunToolInput{AgentJobID: src.AgentJobID.String(), Title: strings.TrimSpace(src.Title)})
	if err != nil {
		return nil
	}
	return &models.ToolCall{ToolName: jobRunToolCallName, ToolInput: string(input), ToolOutput: prompt}
}

// jobRunProgressEntry is the run_job call as a finished row of the live tool timeline, shown from
// the start of the run.
func jobRunProgressEntry(call *models.ToolCall, now time.Time) models.ChatTurnToolCall {
	return models.ChatTurnToolCall{
		ID:         jobRunProgressToolID,
		Name:       call.ToolName,
		Input:      call.ToolInput,
		Output:     tools.TruncateRunes(call.ToolOutput, memoryProgressPreviewRunes),
		Status:     models.ChatTurnToolComplete,
		StartedAt:  now,
		FinishedAt: &now,
	}
}

// leadToolCallsForChatContext are the display-only calls saved ahead of the turn's own: the
// run_job call, then the memory load.
func leadToolCallsForChatContext(chatCtx *chatContext) []*models.ToolCall {
	memory := memoryToolCallsForChatContext(chatCtx)
	if chatCtx == nil || chatCtx.jobRunCall == nil {
		return memory
	}
	return append([]*models.ToolCall{chatCtx.jobRunCall}, memory...)
}
