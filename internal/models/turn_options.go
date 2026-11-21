package models

import (
	"strings"

	"github.com/google/uuid"
)

// TurnOptions are per-turn settings a caller passes with one user message. They apply to that
// turn's model calls only (every call of an Advanced run included) and are never stored: not on
// the message, not on the chat, not on the job. A nil *TurnOptions, which every ordinary turn has,
// changes nothing.
//
// The app's own send route never sets them; an extension that offers a different send contract
// (a plugin route, say) does, by setting ChatMessage.TurnOptions on the message it starts.
//
// The context fields carry authority: PreContext is read as system instruction (a system block on
// Claude, a system message on Chat Completions, a developer message on Responses) and PostContext
// as developer or user text. An extension must pass only text it vouches for, never raw end-user
// input, which belongs in the message itself.
type TurnOptions struct {
	// PreContext is caller-supplied text placed at the end of the system material (after the
	// scratchpad and checkpoint summary, before the history), so a stable value shares the
	// cached prefix. It is trusted as system instruction. Empty adds nothing.
	PreContext string
	// PostContext is caller-supplied text placed after the history and the turn's own developer
	// context, immediately before the new user message, as a developer message (a user message on
	// providers without a developer role). Empty adds nothing.
	PostContext string
	// ModelID runs this turn on another model than the chat's. The chat row keeps its model.
	// Nil uses the chat's model. The model must be one the sender could pick for a chat (not
	// retired or deleted, their own if it is on a provider connection, experimental only if they
	// have experimental models on); otherwise the send fails (agent.ErrTurnModelUnavailable) and
	// no turn starts. It never falls back to the chat's model.
	ModelID *uuid.UUID
	// MaxOutputTokens replaces the output cap of the requests the turn's own model makes (every
	// call of an Advanced run, and z.ai's truncation retry), in the same request field. It is not
	// applied to other models the turn calls (sub-agents, checkpoint work). A value below
	// MinTurnMaxOutputTokens is raised to it. A low cap still makes a reasoning model likely to
	// spend it all on thinking and reply with nothing; on a Claude model that takes a thinking
	// budget, the budget is lowered under the cap, or thinking turned off if the cap leaves no
	// room for it. Zero keeps the usual cap.
	MaxOutputTokens int
	// MemoryRecall false turns memory recall off for this turn: no memories are retrieved for it
	// (and the memory-query model call is skipped), and memories earlier turns recalled are not
	// replayed into its context. The first message's user-name line stays: it is identity, not
	// memory. Nil (or true) recalls as usual.
	//
	// It is not stored: a retry of the message runs without options and recalls as usual (only
	// MemorySave false survives a retry, through the flag stored on the message). A caller that
	// regenerates a recall-off turn has to pass the options again.
	MemoryRecall *bool
	// MemorySave false means no memories are extracted while the turn is in a checkpoint's window,
	// and create_memory is not offered on the turn. Its messages are flagged
	// (ChatMessage.ExcludeFromMemory) so a later checkpoint knows. The turn can still reach the
	// personality's scratchpad (shared across its chats) and the chat's summary (stored as a
	// Summary memory that find_context searches), and a later checkpoint's extraction reads both.
	//
	// Extraction runs over a window of turns, not per turn, so the flag skips the whole window: a
	// flagged turn suppresses extraction for as long as it is carried over into later windows, and
	// the save-on turns in a skipped window are never extracted. Nil (or true) saves as usual.
	MemorySave *bool
}

// MinTurnMaxOutputTokens is the lowest TurnOptions.MaxOutputTokens a request is sent with: the
// Responses API refuses a cap under 16.
const MinTurnMaxOutputTokens = 16

// TurnPreContext returns o.PreContext, or "" for nil options.
func (o *TurnOptions) TurnPreContext() string {
	if o == nil {
		return ""
	}
	return o.PreContext
}

// TurnPostContext returns o.PostContext, or "" for nil options.
func (o *TurnOptions) TurnPostContext() string {
	if o == nil {
		return ""
	}
	return o.PostContext
}

// HasContext reports whether the turn carries pre or post context (blank text adds none).
func (o *TurnOptions) HasContext() bool {
	return o != nil && (strings.TrimSpace(o.PreContext) != "" || strings.TrimSpace(o.PostContext) != "")
}

// TurnModelID returns o.ModelID, or nil for nil options.
func (o *TurnOptions) TurnModelID() *uuid.UUID {
	if o == nil {
		return nil
	}
	return o.ModelID
}

// TurnMaxOutputTokens returns o.MaxOutputTokens when positive, else 0.
func (o *TurnOptions) TurnMaxOutputTokens() int {
	if o == nil || o.MaxOutputTokens <= 0 {
		return 0
	}
	return o.MaxOutputTokens
}

// RecallsMemory reports whether the turn recalls memories: true unless MemoryRecall is set to false.
func (o *TurnOptions) RecallsMemory() bool {
	return o == nil || o.MemoryRecall == nil || *o.MemoryRecall
}

// SavesMemory reports whether the turn may become memories: true unless MemorySave is set to false.
func (o *TurnOptions) SavesMemory() bool {
	return o == nil || o.MemorySave == nil || *o.MemorySave
}
