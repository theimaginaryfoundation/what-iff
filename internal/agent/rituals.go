package agent

import (
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// FormatRituals renders skills for a prompt that has no skills segment of its own (a sub-agent's
// task): each skill's instructions in a <skill name="slug"> block, preceded by a blank line.
// Empty when no skill has instructions.
func FormatRituals(rituals []*models.Ritual) string {
	blocks := formatSkillBlocks(rituals)
	if blocks == "" {
		return ""
	}
	return "\n\n" + blocks
}

func GetRitualIds(rituals []*models.Ritual) []uuid.UUID {
	ritualIds := make([]uuid.UUID, len(rituals))
	for i, ritual := range rituals {
		ritualIds[i] = ritual.ID
	}
	return ritualIds
}
