package agent

import (
	"context"
	"strings"

	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// toolAllowlistForChat, when non-nil, may return the only function tools a chat turn can be
// offered, by name, or nil for no restriction. Nothing in this repository sets it, so by default
// it changes nothing; a build sets it in init() to give some chats a small fixed toolset.
//
// It only narrows. A tool it leaves out is never offered, to the turn's model or to a sub-agent
// the turn starts, and dispatch refuses it, whatever the chat's disabled_tools, the catalog
// defaults or a skill say. A tool it names is offered only when those rules would offer it anyway.
// It covers every kind of function tool: the catalog (including tools a build contributes), the
// turn-only tools (update_plan, ask_user) and MCP tools, which it admits only by full name
// ("mcp__<connector>__<tool>"). Vendor-native web search follows web_search.
//
// The resolved list is kept on the chat context (chatContext.toolAllowlist), so code that runs
// later in the turn follows it too:
//   - When it names no MCP tool, the turn does not contact MCP connectors: nothing is discovered,
//     and list(kind="mcp_servers") and load_tools see no connector in scope. When it names some,
//     list and load_tools still see every tool of the connectors in scope, but only the listed
//     ones are offered or callable.
//   - A sub-agent started with run_subagent is offered only the listed MCP tools of its skills'
//     connectors (and contacts none when no MCP tool is listed).
//   - The developer context does not point the model at a tool the list leaves out: the
//     tool-lifecycle note and the not-yet-loaded part of the loadable-tools note need load_tools,
//     the skills index needs load_skill, the mode-switch hint change_mode, and the Advanced loop's
//     checklist paragraph update_plan.
//
// Loadable built-in tools and MCP tools are only sent to the model once loaded in the chat, which
// takes load_tools (and, to find an MCP tool's connector, list). Name those too, or only the tools
// already loaded in the chat can be called.
//
// It is called more than once per turn, so it should be cheap: cache whatever it looks up.
var toolAllowlistForChat func(ctx context.Context, a *Agent, chat *models.Chat) map[string]bool

// applyToolAllowlist resolves the build's allowlist for the turn, keeps it on chatCtx for the code
// that runs later in the turn, and disables every catalog tool it leaves out. Turn-only and MCP
// tools are filtered by restrictToAllowlist where their specs are assembled (turnOnlyToolSpecs is
// their one list), which also records them in disabledTools.
func applyToolAllowlist(ctx context.Context, a *Agent, chatCtx *chatContext, policy *turnToolPolicy) {
	chatCtx.toolAllowlist = nil
	if toolAllowlistForChat == nil {
		return
	}
	allowed := toolAllowlistForChat(ctx, a, chatCtx.chat)
	if allowed == nil {
		return
	}
	chatCtx.toolAllowlist = allowed
	policy.allowedTools = allowed
	for _, def := range agenttools.FunctionToolCatalog() {
		if !allowed[def.Spec.Name] {
			policy.disabledTools[def.Spec.Name] = true
		}
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
	for _, spec := range specs {
		if !p.allowedTools[spec.Name] {
			p.disabledTools[spec.Name] = true
		}
	}
	return keepAllowlisted(specs, p.allowedTools)
}

// keepAllowlisted returns the specs an allowlist admits; a nil allowlist admits every spec.
func keepAllowlisted(specs []agenttools.FunctionToolSpec, allowed map[string]bool) []agenttools.FunctionToolSpec {
	if allowed == nil {
		return specs
	}
	out := make([]agenttools.FunctionToolSpec, 0, len(specs))
	for _, spec := range specs {
		if allowed[spec.Name] {
			out = append(out, spec)
		}
	}
	return out
}

// offersMCP reports whether the turn may use MCP tools: tools are on and the allowlist, if there
// is one, names at least one MCP tool.
func (p turnToolPolicy) offersMCP() bool {
	return p.toolsEnabled && allowlistAdmitsMCP(p.allowedTools)
}

// allowlistAdmitsMCP reports whether an allowlist leaves room for MCP tools: it is nil (no
// restriction) or names at least one.
func allowlistAdmitsMCP(allowed map[string]bool) bool {
	if allowed == nil {
		return true
	}
	for name, ok := range allowed {
		if ok && strings.HasPrefix(name, mcpToolNamePrefix) {
			return true
		}
	}
	return false
}

// toolAllowedThisTurn reports whether the allowlist resolved for the turn, if any, admits the
// tool, for code that runs without the turn's policy. It does not consult disabled tools.
func (c *chatContext) toolAllowedThisTurn(name string) bool {
	return c == nil || c.toolAllowlist == nil || c.toolAllowlist[name]
}

type subagentToolAllowlistKey struct{}

// withSubagentToolAllowlist carries the parent turn's allowlist (nil: none) to a sub-agent call,
// whose MCP tools getSubagentMCPFunctionToolSpecs and getSubagentMCPTools then filter through it.
func withSubagentToolAllowlist(ctx context.Context, allowed map[string]bool) context.Context {
	if allowed == nil {
		return ctx
	}
	return context.WithValue(ctx, subagentToolAllowlistKey{}, allowed)
}

// subagentToolAllowlist is the allowlist a sub-agent inherits (nil for none).
func subagentToolAllowlist(ctx context.Context) map[string]bool {
	v, _ := ctx.Value(subagentToolAllowlistKey{}).(map[string]bool)
	return v
}
