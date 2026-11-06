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

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// UtilityRole groups background work that runs on the same model.
type UtilityRole = models.UtilityRole

const (
	UtilityRoleMemory      = models.UtilityRoleMemory
	UtilityRoleSimple      = models.UtilityRoleSimple
	UtilityRolePersonality = models.UtilityRolePersonality
)

// UtilityMessageRole is who a turn of a call's input is from.
type UtilityMessageRole string

const (
	UtilityMessageUser UtilityMessageRole = RoleUser
	// UtilityMessageDeveloper is a note from the app, not the person. APIs without a
	// developer role receive developer turns as system text.
	UtilityMessageDeveloper UtilityMessageRole = RoleDeveloper
)

// Valid reports whether r is a role a turn can have.
func (r UtilityMessageRole) Valid() bool {
	return r == UtilityMessageUser || r == UtilityMessageDeveloper
}

// UtilityServiceTier is how fast, and at what price, a call asks to be processed, where an
// API offers a choice.
type UtilityServiceTier string

const (
	// UtilityServiceTierDefault leaves the API's default.
	UtilityServiceTierDefault UtilityServiceTier = ""
	// UtilityServiceTierPriority asks for faster, pricier processing.
	UtilityServiceTierPriority UtilityServiceTier = "priority"
	// UtilityServiceTierFlex asks for slower, cheaper processing.
	UtilityServiceTierFlex UtilityServiceTier = "flex"
)

// Valid reports whether t is a known tier.
func (t UtilityServiceTier) Valid() bool {
	return t == UtilityServiceTierDefault || t == UtilityServiceTierPriority || t == UtilityServiceTierFlex
}

// UtilityMessage is one turn of a call's History.
type UtilityMessage struct {
	Role UtilityMessageRole
	Text string
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
// Context or the stored response PreviousResponseID names (never both); the History; and last,
// Input, the user turn that asks for the work. Validate enforces the rules, so no field is
// ever silently left out.
type UtilityRequest struct {
	Role UtilityRole
	// CallPath labels the call's telemetry. Set it unless the context already carries the
	// caller's label, which an empty CallPath keeps.
	CallPath     telemetry.CallPath
	Instructions string
	// Context, when set, is a conversation the call continues.
	Context *ModelContext
	// PreviousResponseID continues a conversation the Responses API stored, in
	// place of Context. Only a model on that API can use it.
	PreviousResponseID string
	// History is the call's own turns before Input, such as a developer note on how to
	// do the work.
	History []UtilityMessage
	// Input is the call's last user turn, the one that asks for the work.
	Input string
	// InputImageDataURL, when set, attaches one image (a data: URL) to Input, after its text.
	InputImageDataURL string
	// Schema, when set, asks for structured output.
	Schema *UtilitySchema
	// MaxOutputTokens caps the reply; zero leaves the API's default.
	MaxOutputTokens int64
	// SafetyUserID identifies the account to APIs that take one.
	SafetyUserID string
	ServiceTier  UtilityServiceTier
}

// ErrInvalidUtilityRequest is a utility request that breaks UtilityRequest's rules: a bug at
// the call site, never something the account did.
var ErrInvalidUtilityRequest = errors.New("invalid utility request")

// Validate reports a request that would send something other than what it says, or nothing
// to ask: Input needs text or an image, since a call with no ask has nothing to answer and
// Anthropic rejects an empty text block. A caller whose ask can be empty, such as naming a chat
// from an image-only message, skips the call instead.
func (r UtilityRequest) Validate() error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidUtilityRequest, fmt.Sprintf(format, args...))
	}
	switch {
	case !r.Role.Valid():
		return invalid("unknown role %q", r.Role)
	case r.Context != nil && r.PreviousResponseID != "":
		return invalid("continues both a conversation and a stored response")
	case !r.ServiceTier.Valid():
		return invalid("unknown service tier %q", r.ServiceTier)
	case r.Input == "" && r.InputImageDataURL == "":
		return invalid("nothing to ask: Input has no text or image")
	case r.MaxOutputTokens < 0:
		return invalid("negative MaxOutputTokens")
	}
	for i, m := range r.History {
		if !m.Role.Valid() {
			return invalid("history turn %d has role %q, not user or developer", i, m.Role)
		}
	}
	if r.InputImageDataURL != "" {
		if _, _, ok := splitImageDataURL(r.InputImageDataURL); !ok {
			return invalid("input image is not a base64 data: URL")
		}
	}
	return nil
}

// UtilityCaller makes utility calls. It returns the reply's text, which is JSON when
// the request has a Schema.
type UtilityCaller interface {
	CallUtility(ctx context.Context, req UtilityRequest) (string, error)
}
