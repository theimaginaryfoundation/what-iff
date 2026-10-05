package discordrelay

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRelayThreadDisabledToolsStartWithTheDefaultsAndTakeRegisteredNames(t *testing.T) {
	got := RelayThreadDisabledTools()
	assert.Equal(t, []string{"create_agent_job", "run_subagent", "update_scratchpad", "web_search", "fetch_page", "generate_image"}, got)

	t.Cleanup(func() {
		relayToolsMu.Lock()
		relayThreadExtraTools = nil
		relayToolsMu.Unlock()
	})
	RegisterRelayThreadDisabledTool("shell_exec")
	RegisterRelayThreadDisabledTool("shell_exec")
	RegisterRelayThreadDisabledTool("web_search")
	RegisterRelayThreadDisabledTool("")
	got = RelayThreadDisabledTools()
	assert.Equal(t, "shell_exec", got[len(got)-1])
	assert.Len(t, got, 7, "duplicates and defaults are not repeated")

	got[0] = "mutated"
	assert.Equal(t, "create_agent_job", RelayThreadDisabledTools()[0], "callers get a copy")
}
