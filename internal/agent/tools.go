package agent

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const ImageGenerationToolName = "image_generation"

// ToolConfig holds the parameters needed to configure chat tools
type ToolConfig struct {
	// DisabledTools is the effective set of tool names to exclude. nil/empty = use all defaults.
	DisabledTools map[string]bool
}

type turnToolPolicy struct {
	toolsEnabled  bool
	disabledTools map[string]bool
	showMoodTools bool
	ritualIDs     []uuid.UUID
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
	applyModelCapabilitiesToToolPolicy(&policy, a.modelForToolPolicy(ctx, chatCtx))
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

	return policy
}

func applyModelCapabilitiesToToolPolicy(policy *turnToolPolicy, model *models.Model) {
	if policy == nil || model == nil {
		return
	}
	if policy.disabledTools == nil {
		policy.disabledTools = map[string]bool{}
	}

	caps := models.DeriveModelCapabilities(model, nil)
	if !caps.ToolCalling {
		policy.toolsEnabled = false
		for _, spec := range agenttools.AgentFunctionToolSpecs(true) {
			policy.disabledTools[spec.Name] = true
		}
		policy.disabledTools[agenttools.ToolNameWebSearch] = true
		return
	}

	// Native web search is only wired on the OpenAI Responses and native
	// Anthropic paths. Other providers should not advertise a tool they cannot run.
	provider := models.ProviderForModel(model.Provider, model.Name)
	if provider != models.ModelProviderOpenAI && provider != models.ModelProviderAnthropic {
		policy.disabledTools[agenttools.ToolNameWebSearch] = true
	}
}

func (a *Agent) modelForToolPolicy(ctx context.Context, chatCtx *chatContext) *models.Model {
	if a == nil || a.ds == nil || chatCtx == nil || chatCtx.chat == nil {
		return nil
	}
	if chatCtx.chat.ModelID != uuid.Nil {
		if model, err := a.ds.GetModel(ctx, chatCtx.chat.ModelID); err == nil {
			return model
		}
	}
	if chatCtx.model != "" {
		if model, err := a.ds.GetModelByName(ctx, chatCtx.model); err == nil {
			return model
		}
	}
	return nil
}

func getChatTools(config ToolConfig) []responses.ToolUnionParam {
	// Always include web search when the effective model/policy supports it.
	//
	// NOTE: We intentionally do NOT include OpenAI native image_generation here. Many models that
	// otherwise support tool calling do not support image_generation as a native tool, and we
	// implement image generation via an explicit Images API call in a dedicated agent branch.
	tools := []responses.ToolUnionParam{}
	if !config.DisabledTools[agenttools.ToolNameWebSearch] {
		tools = append(tools, responses.ToolUnionParam{OfWebSearch: &responses.WebSearchToolParam{
			Type: responses.WebSearchToolTypeWebSearch,
		}})
	}

	return tools
}
