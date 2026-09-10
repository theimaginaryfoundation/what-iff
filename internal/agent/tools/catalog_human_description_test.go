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
