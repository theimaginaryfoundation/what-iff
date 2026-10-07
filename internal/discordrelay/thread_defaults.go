package discordrelay

import "github.com/theimaginaryfoundation/what-iff/internal/models"

// A relay thread the server creates for a new binding is sandboxed, and the sandbox brings its
// own defaults: the tools that spend the owner's credits or act beyond the conversation start
// switched off (models.SandboxDefaultDisabledTools, applied by datastore.CreateChat), and the
// owner turns each one on deliberately in the thread's settings.

// RegisterRelayThreadDisabledTool adds a tool name that new sandboxed chats (relay threads
// among them) start with switched off. It forwards to models.RegisterSandboxDefaultDisabledTool
// and is kept for builds that already call it from an init(); new code registers with the
// models package directly.
func RegisterRelayThreadDisabledTool(name string) {
	models.RegisterSandboxDefaultDisabledTool(name)
}
