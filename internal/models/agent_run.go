package models

import "github.com/theimaginaryfoundation/what-iff/internal/modeltypes"

// The Advanced run record (ADR 0x026) lives in modeltypes so the Ent schema can store it.
type (
	AgentRun         = modeltypes.AgentRun
	AgentRunStep     = modeltypes.AgentRunStep
	AgentRunNote     = modeltypes.AgentRunNote
	AgentRunNoteKind = modeltypes.AgentRunNoteKind
	AgentRunEnd      = modeltypes.AgentRunEnd
	AgentRunDraft    = modeltypes.AgentRunDraft
	AgentRunToolCall = modeltypes.AgentRunToolCall
)

const (
	AgentRunNoteReasoning = modeltypes.AgentRunNoteReasoning
	AgentRunNoteText      = modeltypes.AgentRunNoteText
	AgentRunEndDone       = modeltypes.AgentRunEndDone
	AgentRunEndStalled    = modeltypes.AgentRunEndStalled
	AgentRunEndDuration   = modeltypes.AgentRunEndDuration
	AgentRunEndQuota      = modeltypes.AgentRunEndQuota
	AgentRunEndStepLimit  = modeltypes.AgentRunEndStepLimit
	AgentRunEndCancelled  = modeltypes.AgentRunEndCancelled
	AgentRunEndFailed     = modeltypes.AgentRunEndFailed
)
