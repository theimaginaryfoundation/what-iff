package models

import "github.com/google/uuid"

// UtilityRole groups background work that runs on the same model (ADR 0x027
// section 6).
type UtilityRole string

const (
	// UtilityRoleMemory covers memory extraction and merge, the scratchpad,
	// checkpoint summaries, recall and thread rehydration.
	UtilityRoleMemory UtilityRole = "memory"
	// UtilityRoleQuick covers short tasks: chat naming, expression and mood
	// selection, schedule parsing and memory queries.
	UtilityRoleQuick UtilityRole = "quick"
	// UtilityRolePersonality covers generating a personality.
	UtilityRolePersonality UtilityRole = "personality"
)

// UtilityRoles lists the roles a model can be chosen for, in the order a
// settings screen shows them.
var UtilityRoles = []UtilityRole{UtilityRoleMemory, UtilityRoleQuick, UtilityRolePersonality}

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

// UtilityModelSetting is the model chosen for a role at a scope.
type UtilityModelSetting struct {
	Scope   UtilityScope `json:"scope"`
	Role    UtilityRole  `json:"role"`
	ModelID uuid.UUID    `json:"model_id"`
}

// UtilityRoleModel is what an account sees of one role: its own choice, the deployment's,
// and the built-in model the role runs on when neither chose one.
type UtilityRoleModel struct {
	Role UtilityRole `json:"role"`
	// AccountModelID is the account's choice, or nil when it made none.
	AccountModelID *uuid.UUID `json:"account_model_id"`
	// DeploymentModelID is the deployment's choice, or nil when it made none.
	DeploymentModelID *uuid.UUID `json:"deployment_model_id"`
	// BuiltInModel is the name of the model the role runs on when nothing is chosen.
	BuiltInModel string `json:"built_in_model"`
}

// UtilityModels lists every role's models, in UtilityRoles order.
type UtilityModels struct {
	// AccountsChoose is whether an account may choose its own models: only where accounts bring
	// their own keys, and so pay for their background work.
	AccountsChoose bool               `json:"accounts_choose"`
	Roles          []UtilityRoleModel `json:"roles"`
}

// SetUtilityModelRequest chooses a model for a role.
type SetUtilityModelRequest struct {
	ModelID uuid.UUID `json:"model_id"`
}
