package telemetry

import "context"

// CallPath labels which high-level feature issued an inference request. Values are
// fixed strings for low OTel cardinality.
type CallPath string

const (
	CallPathUnknown             CallPath = "unknown"
	CallPathUserChat            CallPath = "user_chat"
	CallPathAgentJob            CallPath = "agent_job"
	CallPathScratchpad          CallPath = "scratchpad"
	CallPathMemory              CallPath = "memory"
	CallPathConversationSummary CallPath = "conversation_summary"
	CallPathImageRitual         CallPath = "image_ritual"
	CallPathChatName            CallPath = "chat_name"
	CallPathGeneratePersonality CallPath = "generate_personality"
	CallPathExpressionGrid      CallPath = "expression_grid"
	CallPathPersonalityPortrait CallPath = "personality_portrait"
	CallPathGateRisk            CallPath = "gate_risk"
	CallPathGateContradiction   CallPath = "gate_contradiction"
	CallPathGateInput           CallPath = "gate_input"
	CallPathScheduleParse       CallPath = "schedule_parse"
	CallPathSubagent            CallPath = "subagent"
)

type ctxKeyCallPath struct{}

// WithCallPath returns a child context that carries callPath for metrics and logging.
func WithCallPath(ctx context.Context, callPath CallPath) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxKeyCallPath{}, callPath)
}

// CallPathFromContext returns the call path from ctx, or CallPathUnknown if unset.
func CallPathFromContext(ctx context.Context) CallPath {
	if ctx == nil {
		return CallPathUnknown
	}
	v, ok := ctx.Value(ctxKeyCallPath{}).(CallPath)
	if !ok || v == "" {
		return CallPathUnknown
	}
	return v
}
