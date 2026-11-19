package agent

import (
	"context"
	"strings"

	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// toolAllowlistForChat, when non-nil, may return the only function tools a chat turn can be
// offered, by name, or nil for no restriction. Nothing in this repository sets it, so by default
// it changes nothing; a build sets it in init() (for example, to give the users of a headless
// deployment a small fixed toolset).
//
// It has the last word over every other policy: a tool it leaves out is never offered, whatever
// the chat's disabled_tools, the catalog defaults or a skill say. It covers every kind of function
// tool: the catalog (including tools a build contributes), the turn-only tools (update_plan,
// ask_user) and MCP tools, whose names start with "mcp__". Vendor-native web search follows
// web_search. When it names no MCP tool, the turn does not contact MCP connectors at all, and when
// it leaves out load_tools the developer context drops the tool-lifecycle note about loading tools.
//
// It is called more than once per turn, so it should be cheap: cache whatever it looks up.
var toolAllowlistForChat func(ctx context.Context, a *Agent, chat *models.Chat) map[string]bool

// applyToolAllowlist resolves the build's allowlist for the turn and disables every tool the
// policy already knows by name (the catalog and the turn-only tools) that it leaves out. MCP tools
// are only known once discovered; restrictToAllowlist covers them where the specs are assembled.
func applyToolAllowlist(ctx context.Context, a *Agent, chat *models.Chat, policy *turnToolPolicy) {
	if toolAllowlistForChat == nil {
		return
	}
	allowed := toolAllowlistForChat(ctx, a, chat)
	if allowed == nil {
		return
	}
	policy.allowedTools = allowed
	for _, def := range agenttools.FunctionToolCatalog() {
		policy.disallowUnlisted(def.Spec.Name)
	}
	for _, spec := range []agenttools.FunctionToolSpec{agenttools.UpdatePlanToolSpec, agenttools.AskUserToolSpec} {
		policy.disallowUnlisted(spec.Name)
	}
}

func (p turnToolPolicy) disallowUnlisted(name string) {
	if !p.allowedTools[name] {
		p.disabledTools[name] = true
	}
}

// toolAllowed reports whether the turn's allowlist, if there is one, admits the tool. It does not
// consult disabledTools.
func (p turnToolPolicy) toolAllowed(name string) bool {
	return p.allowedTools == nil || p.allowedTools[name]
}

// restrictToAllowlist drops the specs the allowlist leaves out and records them in disabledTools,
// so the provider adapters and the offered set (which dispatch enforces) both exclude them. With no
// allowlist it returns specs unchanged.
func (p turnToolPolicy) restrictToAllowlist(specs []agenttools.FunctionToolSpec) []agenttools.FunctionToolSpec {
	if p.allowedTools == nil {
		return specs
	}
	out := make([]agenttools.FunctionToolSpec, 0, len(specs))
	for _, spec := range specs {
		if !p.allowedTools[spec.Name] {
			p.disabledTools[spec.Name] = true
			continue
		}
		out = append(out, spec)
	}
	return out
}

// offersMCP reports whether the turn may use MCP tools: tools are on and the allowlist, if there
// is one, names at least one MCP tool.
func (p turnToolPolicy) offersMCP() bool {
	if !p.toolsEnabled {
		return false
	}
	if p.allowedTools == nil {
		return true
	}
	for name, ok := range p.allowedTools {
		if ok && strings.HasPrefix(name, mcpToolNamePrefix) {
			return true
		}
	}
	return false
}
