package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every step names the role it runs on, so a client can show a role's notices together; a step
// this server doesn't know has none.
func TestBackgroundStep_Role(t *testing.T) {
	t.Parallel()
	for step, want := range map[BackgroundStep]UtilityRole{
		BackgroundStepScratchpad:                       UtilityRoleMemory,
		BackgroundStepMemory:                           UtilityRoleMemory,
		BackgroundStepSummary:                          UtilityRoleMemory,
		BackgroundStepMemoryQuery:                      UtilityRoleSimple,
		BackgroundStepChatName:                         UtilityRoleSimple,
		BackgroundStepMoodSelect:                       UtilityRoleSimple,
		BackgroundStepExpressionPick:                   UtilityRoleSimple,
		BackgroundFallbackStep(UtilityRoleMemoryMerge): UtilityRoleMemoryMerge,
		"fallback_images":                              "",
		"something_new":                                "",
		"fallback_":                                    "",
	} {
		require.Equal(t, want, step.Role(), step)
	}
}
