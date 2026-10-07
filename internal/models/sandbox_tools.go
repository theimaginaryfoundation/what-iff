package models

import (
	"slices"
	"sync"
)

// Tools a new sandboxed chat starts with switched off. A sandbox is driven by whoever can talk in
// it (a Discord channel, say), so the tools that spend the owner's credits or act beyond the
// conversation are off until the owner turns them on in the thread's settings; the owner can, for
// example, attach a connector to a sandboxed thread on purpose.
//
// The tool catalog (internal/agent/tools) registers its own default-off tools from an init(), and
// a build that adds tools registers its own the same way. The list is read by CreateChat when a
// chat is created sandboxed with no disabled_tools of its own, so every creation path (the API,
// an import, a plugin) starts a sandbox the same way. Tools a sandbox may never use are not
// listed here: those are refused by the agent whatever disabled_tools says.
var (
	sandboxDefaultOffMu    sync.Mutex
	sandboxDefaultOffTools []string
)

// RegisterSandboxDefaultDisabledTool adds a tool name that new sandboxed chats start with in
// their disabled_tools. Call it from an init(); registering a name twice is harmless.
func RegisterSandboxDefaultDisabledTool(name string) {
	if name == "" {
		return
	}
	sandboxDefaultOffMu.Lock()
	defer sandboxDefaultOffMu.Unlock()
	if !slices.Contains(sandboxDefaultOffTools, name) {
		sandboxDefaultOffTools = append(sandboxDefaultOffTools, name)
	}
}

// SandboxDefaultDisabledTools returns the disabled_tools a new sandboxed chat is created with,
// in registration order.
func SandboxDefaultDisabledTools() []string {
	sandboxDefaultOffMu.Lock()
	defer sandboxDefaultOffMu.Unlock()
	return slices.Clone(sandboxDefaultOffTools)
}
