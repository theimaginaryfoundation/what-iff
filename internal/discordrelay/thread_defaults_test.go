package discordrelay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestRegisterRelayThreadDisabledToolForwardsToTheSandboxDefaults(t *testing.T) {
	RegisterRelayThreadDisabledTool("shell_exec")
	RegisterRelayThreadDisabledTool("")
	assert.Contains(t, models.SandboxDefaultDisabledTools(), "shell_exec")
	assert.NotContains(t, models.SandboxDefaultDisabledTools(), "")
}
