package models

import "github.com/google/uuid"

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

// UtilityModelSetting is what was chosen for a role at a scope: a model, or Off.
type UtilityModelSetting struct {
	Scope UtilityScope `json:"scope"`
	Role  UtilityRole  `json:"role"`
	// ModelID is the chosen model, or nil when the role is switched off.
	ModelID *uuid.UUID `json:"model_id"`
}

// Off reports whether the setting switches its role off.
func (s UtilityModelSetting) Off() bool { return s.ModelID == nil }

// UtilityChoice is one scope's choice for a role, as an account sees it.
type UtilityChoice struct {
	// Off is whether the role is switched off; ModelID is then nil.
	Off bool `json:"off"`
	// ModelID is the chosen model, or nil when the role is off.
	ModelID *uuid.UUID `json:"model_id"`
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
// ModelID and Off.
type SetUtilityModelRequest struct {
	ModelID *uuid.UUID `json:"model_id"`
	Off     bool       `json:"off"`
}
