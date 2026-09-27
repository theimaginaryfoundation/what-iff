package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserToggleableBuiltInToolsHaveHumanDescriptions(t *testing.T) {
	for _, def := range functionToolCatalog {
		if !def.UserToggleable {
			continue
		}

		require.NotEmpty(t, strings.TrimSpace(def.HumanDescription), "%s should expose a human-facing description", def.Spec.Name)
		require.NotEqual(t, strings.TrimSpace(def.Spec.Description), strings.TrimSpace(def.HumanDescription), "%s should not reuse its agent-facing prompt as the human-facing description", def.Spec.Name)
	}
}

// Every toggle in the Tools tab gets a tooltip saying what the tool can do. The guide says more
// than the one-line description and never exposes the agent-facing prompt.
func TestUserToggleableBuiltInToolsHaveUserGuides(t *testing.T) {
	for _, def := range functionToolCatalog {
		if !def.UserToggleable {
			continue
		}

		guide := strings.TrimSpace(def.UserGuide)
		require.NotEmpty(t, guide, "%s should explain what it can do in a user guide", def.Spec.Name)
		require.NotEqual(t, strings.TrimSpace(def.HumanDescription), guide, "%s guide should add to its description", def.Spec.Name)
		require.NotContains(t, def.Spec.Description, guide, "%s guide should not reuse the agent-facing prompt", def.Spec.Name)
		require.LessOrEqual(t, len(guide), 260, "%s guide should stay tooltip-sized", def.Spec.Name)
	}
}
