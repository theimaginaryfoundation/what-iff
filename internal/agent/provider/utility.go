package provider

// A utility call is background work that is not the assistant talking, such as
// naming a chat or extracting memories (ADR 0x027 section 6). A call names its
// role and says what it needs in provider-neutral terms; the role's model
// decides which API the call goes through, so call sites never build a
// provider's request themselves.

import (
	"context"

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
type UtilityRequest struct {
	Role UtilityRole
	// CallPath labels the call's telemetry; empty keeps the context's.
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

// UtilityCaller makes utility calls. It returns the reply's text, which is JSON when
// the request has a Schema.
type UtilityCaller interface {
	CallUtility(ctx context.Context, req UtilityRequest) (string, error)
}
