package tools

import "github.com/theimaginaryfoundation/what-iff/internal/models"

// SandboxPolicy says what a sandboxed chat (models.ContextScopeSandbox) does with a function
// tool. Every catalog entry declares one; the zero value fails closed, so a tool nobody has
// thought about is never offered in a sandbox.
type SandboxPolicy int

const (
	// SandboxNever: never offered in a sandbox, whatever the chat's disabled_tools say, and the
	// handler refuses too. For tools that cannot work inside the boundary at all: they write
	// something shared with the owner's other chats, or run outside the sandbox.
	SandboxNever SandboxPolicy = iota
	// SandboxDefaultOff: a new sandboxed chat starts with the tool in its disabled_tools, and the
	// owner can switch it on in the thread's settings. For tools that spend the owner's credits
	// or act beyond the conversation (the web, image generation, sub-agents).
	SandboxDefaultOff
	// SandboxAllowed: offered as in any chat. The tool's own rules keep it inside the sandbox
	// (memory and recall scoping, the chat's own connectors).
	SandboxAllowed
)

// SandboxNeverTools lists the catalog tools a sandboxed chat is never offered.
func SandboxNeverTools() []string {
	var out []string
	for _, def := range mergedFunctionToolCatalog() {
		if def.SandboxPolicy == SandboxNever {
			out = append(out, def.Spec.Name)
		}
	}
	return out
}

// SandboxDefaultOffTools lists the catalog tools a new sandboxed chat starts with switched off.
func SandboxDefaultOffTools() []string {
	var out []string
	for _, def := range mergedFunctionToolCatalog() {
		if def.SandboxPolicy == SandboxDefaultOff {
			out = append(out, def.Spec.Name)
		}
	}
	return out
}

// The built-in catalog's default-off tools are what CreateChat starts a sandbox with. A build
// that contributes tools through AdditionalFunctionToolCatalog registers its own default-off
// names with models.RegisterSandboxDefaultDisabledTool from its init().
func init() {
	for _, def := range functionToolCatalog {
		if def.SandboxPolicy == SandboxDefaultOff {
			models.RegisterSandboxDefaultDisabledTool(def.Spec.Name)
		}
	}
}
