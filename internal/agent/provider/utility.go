package provider

// A utility call is background work that is not the assistant talking, such as
// naming a chat or extracting memories (ADR 0x027 section 6). A call names its
// role and says what it needs in provider-neutral terms; the role's model
// decides which API the call goes through, so call sites never build a
// provider's request themselves.

import (
	"context"
	"errors"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// UtilityRole groups background work that runs on the same model.
type UtilityRole string

const (
	// UtilityRoleMemory covers memory extraction and merge, the scratchpad, checkpoint
	// summaries, recall and thread rehydration.
	UtilityRoleMemory UtilityRole = "memory"
	// UtilityRoleQuick covers short tasks: chat naming, expression and mood selection,
	// schedule parsing and memory queries.
	UtilityRoleQuick UtilityRole = "quick"
	// UtilityRolePersonality covers generating a personality.
	UtilityRolePersonality UtilityRole = "personality"
)

// UtilityMessage is one turn of a call's input.
type UtilityMessage struct {
	// Role is "user" or "developer". APIs without a developer role receive
	// developer turns as system text.
	Role string
	Text string
	// ImageDataURL, when set, attaches one image (a data: URL) after the text.
	ImageDataURL string
}

// UtilitySchema asks for structured output: the reply is JSON matching Schema.
type UtilitySchema struct {
	Name        string
	Description string
	Schema      map[string]any
}

// UtilityRequest is one utility call.
//
// What the call sends is, in order: the Instructions; the conversation it continues, either
// Context or the stored response PreviousResponseID names (never both); then its own turns,
// which are Messages, or Input as a single user turn. A request sets Input or Messages, not
// both, and Input not alongside Context, which takes Messages; Validate enforces this, so no
// field is ever silently left out.
type UtilityRequest struct {
	Role UtilityRole
	// CallPath labels the call's telemetry. Set it unless the context already carries the
	// caller's label, which an empty CallPath keeps.
	CallPath     telemetry.CallPath
	Instructions string
	// Context, when set, is a conversation the call continues; Messages follow it.
	Context *ModelContext
	// PreviousResponseID continues a conversation the Responses API stored, in
	// place of Context. Only a model on that API can use it.
	PreviousResponseID string
	// Input is the call's single user message as plain text. A call with more
	// than one turn, or an image, uses Messages instead.
	Input    string
	Messages []UtilityMessage
	// Schema, when set, asks for structured output.
	Schema *UtilitySchema
	// MaxOutputTokens caps the reply; zero leaves the API's default.
	MaxOutputTokens int64
	// SafetyUserID identifies the account to APIs that take one.
	SafetyUserID string
	// Priority asks for faster, pricier processing where an API offers it.
	Priority bool
	// Flex asks for slower, cheaper processing where an API offers it.
	Flex bool
}

// ErrInvalidUtilityRequest is a utility request that breaks UtilityRequest's rules: a bug at
// the call site, never something the account did.
var ErrInvalidUtilityRequest = errors.New("invalid utility request")

// Validate reports a request that would send something other than what it says. An empty
// Input is allowed: a message can be all attachments, and its call sends an empty turn.
func (r UtilityRequest) Validate() error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidUtilityRequest, fmt.Sprintf(format, args...))
	}
	switch {
	case r.Role != UtilityRoleMemory && r.Role != UtilityRoleQuick && r.Role != UtilityRolePersonality:
		return invalid("unknown role %q", r.Role)
	case r.Context != nil && r.PreviousResponseID != "":
		return invalid("continues both a conversation and a stored response")
	case r.Input != "" && len(r.Messages) > 0:
		return invalid("sets both Input and Messages")
	case r.Input != "" && r.Context != nil:
		return invalid("sets Input alongside a conversation, which takes Messages")
	case r.Priority && r.Flex:
		return invalid("asks for both priority and flex processing")
	case r.MaxOutputTokens < 0:
		return invalid("negative MaxOutputTokens")
	}
	for i, m := range r.Messages {
		if m.Role != RoleUser && m.Role != RoleDeveloper {
			return invalid("message %d has role %q, not user or developer", i, m.Role)
		}
		if m.ImageDataURL != "" {
			if _, _, ok := splitImageDataURL(m.ImageDataURL); !ok {
				return invalid("message %d has an image that is not a base64 data: URL", i)
			}
		}
	}
	return nil
}

// UtilityCaller makes utility calls. It returns the reply's text, which is JSON when
// the request has a Schema.
type UtilityCaller interface {
	CallUtility(ctx context.Context, req UtilityRequest) (string, error)
}
