package models

import (
	"errors"

	"github.com/google/uuid"
)

// UtilityRole groups background work that runs on the same model (ADR 0x027
// section 6).
type UtilityRole string

const (
	// UtilityRoleMemory covers memory extraction, the scratchpad, checkpoint
	// summaries, recall and thread rehydration.
	UtilityRoleMemory UtilityRole = "memory"
	// UtilityRoleMemoryMerge covers folding duplicate memories together: the
	// largest memory call, which a deployment may want to run differently from
	// the rest.
	UtilityRoleMemoryMerge UtilityRole = "memory_merge"
	// UtilityRoleSimple covers tasks straightforward and low-risk enough that any
	// model can do them: chat naming, expression and mood selection, schedule
	// parsing and memory queries.
	UtilityRoleSimple UtilityRole = "simple"
	// UtilityRolePersonality covers generating a personality.
	UtilityRolePersonality UtilityRole = "personality"
)

// UtilityRoles lists the roles a model can be chosen for, in the order a
// settings screen shows them.
var UtilityRoles = []UtilityRole{UtilityRoleMemory, UtilityRoleMemoryMerge, UtilityRoleSimple, UtilityRolePersonality}

// Valid reports whether r is a known role.
func (r UtilityRole) Valid() bool {
	for _, known := range UtilityRoles {
		if r == known {
			return true
		}
	}
	return false
}

// UtilityScope is whose choice a utility model setting is.
type UtilityScope string

const (
	UtilityScopeDeployment UtilityScope = "deployment"
	UtilityScopeAccount    UtilityScope = "account"
)

// UtilityServiceTier is how fast, and at what price, a call asks to be processed, where an
// API offers a choice. Only OpenAI's Responses API does; elsewhere it isn't sent.
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

// UtilityModelSetting is what was chosen for a role at a scope: a model, with how it's asked to
// work, or Off.
type UtilityModelSetting struct {
	Scope UtilityScope `json:"scope"`
	Role  UtilityRole  `json:"role"`
	// ModelID is the chosen model, or nil when the role is switched off.
	ModelID *uuid.UUID `json:"model_id"`
	// ReasoningEffort is the effort the role's calls ask for (ADR 0x026), limited to what the
	// model supports; empty leaves the model's own default. Never set when off.
	ReasoningEffort ReasoningEffort `json:"reasoning_effort,omitempty"`
	// ServiceTier is the tier the role's calls ask for, where the model's API offers one; empty
	// leaves the API's default. Never set when off.
	ServiceTier UtilityServiceTier `json:"service_tier,omitempty"`
}

// Off reports whether the setting switches its role off.
func (s UtilityModelSetting) Off() bool { return s.ModelID == nil }

// UtilityChoice is one scope's choice for a role, as an account sees it.
type UtilityChoice struct {
	// Off is whether the role is switched off; the rest is then nil.
	Off bool `json:"off"`
	// ModelID is the chosen model, or nil when the role is off.
	ModelID *uuid.UUID `json:"model_id"`
	// ReasoningEffort is the effort asked for, or nil for the model's own default.
	ReasoningEffort *ReasoningEffort `json:"reasoning_effort"`
	// ServiceTier is the tier asked for, or nil for the API's default.
	ServiceTier *UtilityServiceTier `json:"service_tier"`
}

// UtilityRoleModel is what an account sees of one role: its own choice and the deployment's.
// A role neither chose is not set, and its work doesn't run.
type UtilityRoleModel struct {
	Role UtilityRole `json:"role"`
	// Account is the account's choice, or nil when it made none.
	Account *UtilityChoice `json:"account"`
	// Deployment is the deployment's choice, or nil when it made none.
	Deployment *UtilityChoice `json:"deployment"`
}

// UtilityModels lists every role's models, in UtilityRoles order.
type UtilityModels struct {
	// AccountsChoose is whether an account may choose its own models: only where accounts bring
	// their own keys, and so pay for their background work.
	AccountsChoose bool               `json:"accounts_choose"`
	Roles          []UtilityRoleModel `json:"roles"`
}

// SetUtilityModelRequest chooses a model for a role, or switches it off: exactly one of
// ModelID and Off. ReasoningEffort and ServiceTier go with a model only.
type SetUtilityModelRequest struct {
	ModelID         *uuid.UUID          `json:"model_id"`
	Off             bool                `json:"off"`
	ReasoningEffort *ReasoningEffort    `json:"reasoning_effort"`
	ServiceTier     *UtilityServiceTier `json:"service_tier"`
}

// ErrUtilityRoleOff is utility work whose role is switched off: it doesn't run.
var ErrUtilityRoleOff = errors.New("this utility role is switched off")

// ErrUtilityRoleNotSet is utility work whose role has no model chosen at any scope: it doesn't
// run until one is.
var ErrUtilityRoleNotSet = errors.New("no model is chosen for this utility role")

// ErrUtilityModelUnavailable is utility work whose role's chosen models all can't be used (turned
// off, gone with their connection, unreachable, their key removed, or experimental after the
// account opted out): it doesn't run until one can be. The cause is wrapped with it.
var ErrUtilityModelUnavailable = errors.New("the model chosen for this utility role can't be used")

// UtilityRoleUnavailableError is utility work that didn't run because of its role's choice:
// Err is ErrUtilityRoleOff, ErrUtilityRoleNotSet or ErrUtilityModelUnavailable, and Role says
// which role.
type UtilityRoleUnavailableError struct {
	Role UtilityRole
	Err  error
}

func (e *UtilityRoleUnavailableError) Error() string { return string(e.Role) + ": " + e.Err.Error() }

func (e *UtilityRoleUnavailableError) Unwrap() error { return e.Err }
