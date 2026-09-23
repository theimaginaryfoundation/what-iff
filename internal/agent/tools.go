package agent

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/websearch"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const ImageGenerationToolName = "image_generation"

// ToolConfig holds the parameters needed to configure chat tools
type ToolConfig struct {
	// DisabledTools is the effective set of tool names to exclude. nil/empty = use all defaults.
	DisabledTools map[string]bool
	// NativeWebSearch adds the provider's built-in web search tool (off when first-party web
	// search is configured, ADR 0x021).
	NativeWebSearch bool
}

type turnToolPolicy struct {
	toolsEnabled bool
	// disabledTools filters the function tools offered to the model. It is not consulted for
	// vendor-native web search; use nativeWebSearch for that.
	disabledTools map[string]bool
	showMoodTools bool
	ritualIDs     []uuid.UUID
	// Exactly one of these is true when the user wants web search this turn: first-party
	// web_search/fetch_page when a backend is configured (ADR 0x021), otherwise the vendor's
	// native tool where the provider has one.
	firstPartyWebSearch bool
	nativeWebSearch     bool
}

// ToolMeta describes an available agent tool for use in the tools API.
type ToolMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// humanFacingToolDescription resolves the presentation copy for a tool.
// A non-empty HumanDescription wins after trimming whitespace. External/runtime tools may omit
// HumanDescription, in which case the existing agent description remains a backward-compatible
// fallback rather than rendering a blank entry in the UI.
func humanFacingToolDescription(def agenttools.FunctionToolDefinition) string {
	if description := strings.TrimSpace(def.HumanDescription); description != "" {
		return description
	}
	return def.Spec.Description
}

// GetAvailableTools returns human-facing metadata for tools the user can toggle via disabled_tools.
// Provider/agent prompt descriptions remain on FunctionToolSpec and are intentionally not modified.
func GetAvailableTools(ctx context.Context) []ToolMeta {
	_ = ctx
	definitions := agenttools.FunctionToolCatalog()
	out := make([]ToolMeta, 0, len(definitions)+1)
	out = append(out, ToolMeta{Name: agenttools.ToolNameWebSearch, Description: agenttools.AvailableToolDescriptionWebSearch})
	for _, def := range definitions {
		if !def.UserToggleable {
			continue
		}
		out = append(out, ToolMeta{Name: def.Spec.Name, Description: humanFacingToolDescription(def)})
	}
	return out
}

// disabledToolsSet converts a string slice of tool names into a fast-lookup map.
// Returns nil (not an empty map) when the slice is empty, so callers can use a nil check
// as "use all defaults".
func disabledToolsSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func (a *Agent) buildTurnToolPolicy(ctx context.Context, chatCtx *chatContext, userID uuid.UUID, chatMessage *models.ChatMessage) turnToolPolicy {
	disabledTools := disabledToolsSet(chatCtx.chat.DisabledTools)
	// Mood tools are system-managed; never let user disabled_tools hide them.
	delete(disabledTools, agenttools.ListMoodsToolSpec.Name)
	delete(disabledTools, agenttools.ChangeMoodToolSpec.Name)

	policy := turnToolPolicy{
		toolsEnabled:  chatCtx.chat.ToolsEnabled,
		disabledTools: disabledTools,
		showMoodTools: a.shouldExposeMoodTools(ctx, userID, chatCtx.chat),
		ritualIDs:     mergedRitualIDsForTools(chatMessage, chatCtx.activeMood),
	}
	if !policy.toolsEnabled {
		return policy
	}
	if additionalDisabledToolsForChat != nil {
		for name, disabled := range additionalDisabledToolsForChat(a, chatCtx.chat) {
			if disabled {
				policy.disabledTools[name] = true
			}
		}
	}
	applyWebSearchPolicy(&policy, a.webSearch)

	return policy
}

// applyWebSearchPolicy decides between first-party and vendor-native web search for a turn.
// The user's web_search toggle governs both; fetch_page follows it and also needs a backend
// with an extract API.
func applyWebSearchPolicy(policy *turnToolPolicy, svc *websearch.Service) {
	wantSearch := policy.toolsEnabled && !policy.disabledTools[agenttools.ToolNameWebSearch]
	policy.firstPartyWebSearch = wantSearch && svc != nil
	policy.nativeWebSearch = wantSearch && svc == nil
	if !policy.firstPartyWebSearch {
		policy.disabledTools[agenttools.ToolNameWebSearch] = true
	}
	if !policy.firstPartyWebSearch || svc.Extractor == nil {
		policy.disabledTools[agenttools.ToolNameFetchPage] = true
	}
}

func getChatTools(config ToolConfig) []responses.ToolUnionParam {
	// Always include web search.
	//
	// NOTE: We intentionally do NOT include OpenAI native image_generation here. Many models that
	// otherwise support tool calling do not support image_generation as a native tool, and we
	// implement image generation via an explicit Images API call in a dedicated agent branch.
	tools := []responses.ToolUnionParam{}
	if config.NativeWebSearch {
		tools = append(tools, responses.ToolUnionParam{OfWebSearch: &responses.WebSearchToolParam{
			Type: responses.WebSearchToolTypeWebSearch,
		}})
	}

	return tools
}
