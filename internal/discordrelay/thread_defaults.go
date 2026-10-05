package discordrelay

import (
	"slices"
	"sync"

	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

// A relay thread the server creates for a new binding is a public surface: whoever
// the allow list lets tag the bot drives it. Besides starting at a public memory
// limit (which keeps the owner's personal data out, see models.MemorySensitivity),
// it starts with the tools that act beyond the conversation switched off in its
// disabled_tools, so the owner turns each one on deliberately in the thread's
// settings. Binding an existing thread leaves its settings alone.
//
// The defaults here are the tools this repository ships. A build that adds its own
// tools registers the ones that should also start off with
// RegisterRelayThreadDisabledTool, from an init().
var relayThreadDisabledToolDefaults = []string{
	agenttools.CreateAgentJobToolSpec.Name,   // schedules work in a new, unrestricted chat
	agenttools.RunSubagentToolSpec.Name,      // spends the owner's credits on further model calls
	agenttools.UpdateScratchpadToolSpec.Name, // the scratchpad is shared with the persona's other chats
	agenttools.ToolNameWebSearch,             // reaches the open web (and the vendor's native search)
	agenttools.ToolNameFetchPage,             // fetches arbitrary pages
	agenttools.GenerateImageToolSpec.Name,    // costly, and its output is posted to the channel
}

var (
	relayToolsMu          sync.Mutex
	relayThreadExtraTools []string
)

// RegisterRelayThreadDisabledTool adds a tool name that new Discord relay threads
// start with switched off (in their disabled_tools), next to the defaults above.
// Call it from an init() in the package that adds the tool; registering a name
// twice is harmless.
func RegisterRelayThreadDisabledTool(name string) {
	if name == "" {
		return
	}
	relayToolsMu.Lock()
	defer relayToolsMu.Unlock()
	if !slices.Contains(relayThreadExtraTools, name) {
		relayThreadExtraTools = append(relayThreadExtraTools, name)
	}
}

// RelayThreadDisabledTools returns the disabled_tools a new relay thread is
// created with: the defaults, then every registered name, without duplicates.
func RelayThreadDisabledTools() []string {
	relayToolsMu.Lock()
	defer relayToolsMu.Unlock()
	out := slices.Clone(relayThreadDisabledToolDefaults)
	for _, name := range relayThreadExtraTools {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}
