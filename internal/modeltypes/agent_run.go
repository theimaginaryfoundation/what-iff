package modeltypes

// AgentRun is the durable record of an Advanced-loop turn (ADR 0x026), saved on the assistant
// message it produced (ChatMessage.AgentRun). Each step holds what the model wrote during it,
// in order: its own reasoning and the text it wrote between tool calls. The final reply is the
// message itself and is not repeated here; the step's tool calls are the message's ToolCall rows
// with the same Step.
type AgentRun struct {
	Steps []AgentRunStep `json:"steps,omitempty"`
	// Plan is the run's last update_plan checklist.
	Plan []PlanItem `json:"plan,omitempty"`
	// EndReason is why the run stopped (AgentRunEnd*).
	EndReason AgentRunEnd `json:"end_reason,omitempty"`
}

// AgentRunStep is one step of an Advanced run.
type AgentRunStep struct {
	Step  int            `json:"step"`
	Notes []AgentRunNote `json:"notes,omitempty"`
}

// AgentRunNoteKind says what a note is.
type AgentRunNoteKind string

const (
	// AgentRunNoteReasoning is the model's own reasoning (thinking, reasoning summaries).
	AgentRunNoteReasoning AgentRunNoteKind = "reasoning"
	// AgentRunNoteText is text the model wrote as output before its final reply.
	AgentRunNoteText AgentRunNoteKind = "text"
)

// AgentRunNote is one contiguous piece of a step's notes.
type AgentRunNote struct {
	Kind AgentRunNoteKind `json:"kind"`
	Text string           `json:"text"`
}

// AgentRunEnd is why an Advanced run stopped.
type AgentRunEnd string

const (
	AgentRunEndDone      AgentRunEnd = "done"           // the model answered with no open plan item
	AgentRunEndStalled   AgentRunEnd = "stalled"        // continuation nudges stopped producing work
	AgentRunEndDuration  AgentRunEnd = "duration_limit" // AGENT_ADVANCED_MAX_DURATION
	AgentRunEndQuota     AgentRunEnd = "quota"          // the account's quota ran out between steps
	AgentRunEndStepLimit AgentRunEnd = "step_limit"     // AGENT_ADVANCED_MAX_STEPS
	AgentRunEndCancelled AgentRunEnd = "cancelled"      // the user stopped it
	AgentRunEndFailed    AgentRunEnd = "failed"         // an error, or the server shut down mid-run
)

// Unfinished reports whether the run stopped before its work was done, so its open plan items are
// carried into the thread's next turn.
func (e AgentRunEnd) Unfinished() bool {
	return e != AgentRunEndDone && e != ""
}

// OpenItems returns the plan items still pending or in progress.
func (r *AgentRun) OpenItems() []PlanItem {
	if r == nil {
		return nil
	}
	var open []PlanItem
	for _, item := range r.Plan {
		if item.Status.Open() {
			open = append(open, item)
		}
	}
	return open
}

// AgentRunDraft is an in-progress Advanced run persisted on its job (Job.DraftAgentRun) at every
// step boundary, so a run that fails, is stopped, or dies with the server still leaves its work
// behind: the partial message is built from it.
type AgentRunDraft struct {
	Run       AgentRun           `json:"run"`
	ToolCalls []AgentRunToolCall `json:"tool_calls,omitempty"`
	// The reply's generation metadata, so a draft finalized without the agent (a server shutdown)
	// still records which model, personality and mode produced it.
	GenerationModel       string `json:"generation_model,omitempty"`
	GenerationPersonality string `json:"generation_personality,omitempty"`
	GenerationMoodID      string `json:"generation_mood_id,omitempty"`
}

// AgentRunToolCall is a finished tool call in an AgentRunDraft, saved as a ToolCall row when the
// draft becomes a message.
type AgentRunToolCall struct {
	ToolName   string `json:"tool_name"`
	ToolInput  string `json:"tool_input,omitempty"`
	ToolOutput string `json:"tool_output,omitempty"`
	ToolError  string `json:"tool_error,omitempty"`
	Step       int    `json:"step,omitempty"`
}

// PlanItemStatus is the state of one item in an Advanced run's plan.
type PlanItemStatus string

const (
	PlanItemPending    PlanItemStatus = "pending"
	PlanItemInProgress PlanItemStatus = "in_progress"
	PlanItemCompleted  PlanItemStatus = "completed"
	PlanItemCancelled  PlanItemStatus = "cancelled"
)

// Valid reports whether s is a known status.
func (s PlanItemStatus) Valid() bool {
	switch s {
	case PlanItemPending, PlanItemInProgress, PlanItemCompleted, PlanItemCancelled:
		return true
	}
	return false
}

// Open reports whether the item still has work to do.
func (s PlanItemStatus) Open() bool { return s == PlanItemPending || s == PlanItemInProgress }

// PlanItem is one checklist entry of an Advanced run's plan.
type PlanItem struct {
	Content string         `json:"content"`
	Status  PlanItemStatus `json:"status"`
}
