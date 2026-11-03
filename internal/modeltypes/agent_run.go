package modeltypes

// AgentRunVersion is the current AgentRun format. A reader must render an unknown version
// generically (or not at all) rather than misread it.
const AgentRunVersion = 1

// AgentRun is the durable record of an Advanced-loop turn (ADR 0x026), saved on the assistant
// message it produced (ChatMessage.AgentRun). Each step holds what happened during it, in order:
// the model's reasoning, the text it wrote between tool calls, and its tool calls (by ToolCall
// id, the message's ToolCall rows). The final reply is the message itself and is not repeated.
type AgentRun struct {
	// Version is the format (AgentRunVersion).
	Version int            `json:"version"`
	Steps   []AgentRunStep `json:"steps,omitempty"`
	// Plan is the run's last update_plan checklist.
	Plan []PlanItem `json:"plan,omitempty"`
	// EndReason is why the run stopped (AgentRunEnd*).
	EndReason AgentRunEnd `json:"end_reason,omitempty"`
}

// AgentRunStep is one step of an Advanced run: its events in the order they happened.
type AgentRunStep struct {
	Step   int             `json:"step"`
	Events []AgentRunEvent `json:"events,omitempty"`
}

// AgentRunEventKind says what an event is. Readers must skip kinds they do not know.
type AgentRunEventKind string

const (
	// AgentRunEventReasoning is the model's own reasoning (thinking, reasoning summaries).
	AgentRunEventReasoning AgentRunEventKind = "reasoning"
	// AgentRunEventText is text the model wrote as output before its final reply.
	AgentRunEventText AgentRunEventKind = "text"
	// AgentRunEventToolCall is a tool call the model made; ToolCallID is its ToolCall row.
	AgentRunEventToolCall AgentRunEventKind = "tool_call"
)

// AgentRunEvent is one thing that happened in a step: a contiguous note (Text) or a tool call
// (ToolCallID).
type AgentRunEvent struct {
	Kind       AgentRunEventKind `json:"kind"`
	Text       string            `json:"text,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
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
// behind: the partial message is built from it. Its tool calls are not copied here: they are
// ToolCall rows written as each call finishes, linked to the job until a message takes them.
type AgentRunDraft struct {
	Run AgentRun `json:"run"`
	// The reply's generation metadata, so a draft finalized without the agent (a server shutdown)
	// still records which model, personality and mode produced it.
	GenerationModel       string `json:"generation_model,omitempty"`
	GenerationPersonality string `json:"generation_personality,omitempty"`
	GenerationMoodID      string `json:"generation_mood_id,omitempty"`
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
