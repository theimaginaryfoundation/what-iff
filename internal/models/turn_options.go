package models

import "github.com/google/uuid"

// TurnOptions are per-turn settings a caller passes with one user message. They apply to that
// turn's model calls only (every call of an Advanced run included) and are never stored: not on
// the message, not on the chat, not on the job. A nil *TurnOptions, which every ordinary turn has,
// changes nothing.
//
// The app's own send route never sets them; an extension that offers a different send contract
// (a plugin route, say) does, by setting ChatMessage.TurnOptions on the message it starts.
type TurnOptions struct {
	// PreContext is caller-supplied text placed at the end of the system material (after the
	// scratchpad and checkpoint summary, before the history), so a stable value shares the
	// cached prefix. Empty adds nothing.
	PreContext string
	// PostContext is caller-supplied text placed after the history and the turn's own developer
	// context, immediately before the new user message, as a developer message (a user message on
	// providers without a developer role). Empty adds nothing.
	PostContext string
	// ModelID runs this turn on another model than the chat's. The chat row keeps its model.
	// Nil uses the chat's model.
	ModelID *uuid.UUID
	// MaxOutputTokens replaces the output cap the turn's provider requests would otherwise carry,
	// as-is, in the same request field. Zero keeps the usual cap.
	MaxOutputTokens int
}

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
