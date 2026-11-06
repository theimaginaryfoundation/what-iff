package models

// UtilityRole groups background work that runs on the same model (ADR 0x027
// section 6).
type UtilityRole string

const (
	// UtilityRoleMemory covers memory extraction and merge, the scratchpad,
	// checkpoint summaries, recall and thread rehydration.
	UtilityRoleMemory UtilityRole = "memory"
	// UtilityRoleSimple covers tasks straightforward and low-risk enough that any
	// model can do them: chat naming, expression and mood selection, schedule
	// parsing and memory queries.
	UtilityRoleSimple UtilityRole = "simple"
	// UtilityRolePersonality covers generating a personality.
	UtilityRolePersonality UtilityRole = "personality"
)

// UtilityRoles lists the roles a model can be chosen for, in the order a
// settings screen shows them.
var UtilityRoles = []UtilityRole{UtilityRoleMemory, UtilityRoleSimple, UtilityRolePersonality}

// Valid reports whether r is a known role.
func (r UtilityRole) Valid() bool {
	for _, known := range UtilityRoles {
		if r == known {
			return true
		}
	}
	return false
}
