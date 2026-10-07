package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

type runSubagentToolArgs struct {
	Message       string   `json:"message"`
	PersonalityID string   `json:"personality_id,omitempty"`
	Model         string   `json:"model,omitempty"`
	RitualIDs     []string `json:"skill_ids,omitempty"`
	// RitualIDsAlias is not advertised in the tool spec. It is decoded only so a sandboxed chat
	// can refuse a model that passes skills under the internal name ("ritual_ids"); a
	// chat that is not sandboxed ignores it, as before.
	RitualIDsAlias []string `json:"ritual_ids,omitempty"`
}

// sandboxedSubagentRefusal returns the refusal for a run_subagent call a sandboxed chat may not
// make, or "". A sub-agent started from a sandboxed chat runs as that chat's own persona and with
// no skills: another personality's system prompt, or a skill's text and the MCP servers linked to
// it, are the owner's account data, and the result returns into a conversation a stranger reads.
// A personality_id equal to the chat's own persona is a no-op and allowed.
func sandboxedSubagentRefusal(chat *models.Chat, args runSubagentToolArgs) string {
	const note = "personality_id and skill_ids are not available in this sandboxed conversation: the sub-agent runs as this conversation's own personality with no skills"
	if p := strings.TrimSpace(args.PersonalityID); p != "" {
		id, err := uuid.Parse(p)
		if err != nil || id != chat.PersonalityID {
			return note
		}
	}
	for _, ids := range [][]string{args.RitualIDs, args.RitualIDsAlias} {
		for _, id := range ids {
			if strings.TrimSpace(id) != "" {
				return note
			}
		}
	}
	return ""
}

type runSubagentToolResult struct {
	Success       bool   `json:"success"`
	Model         string `json:"model,omitempty"`
	ModelID       string `json:"model_id,omitempty"`
	PersonalityID string `json:"personality_id,omitempty"`
	Output        string `json:"output,omitempty"`
	Error         string `json:"error,omitempty"`
}

type subagentCallResult struct {
	Output      string
	InputTokens int64
}

func (a *Agent) runSubagentTool(ctx context.Context, chatCtx *chatContext, args []byte) (string, error) {
	var toolArgs runSubagentToolArgs
	if err := json.Unmarshal(args, &toolArgs); err != nil {
		return marshalSubagentToolResult(runSubagentToolResult{
			Success: false,
			Error:   fmt.Sprintf("invalid arguments: %v", err),
		})
	}

	message := strings.TrimSpace(toolArgs.Message)
	if message == "" {
		return marshalSubagentToolResult(runSubagentToolResult{
			Success: false,
			Error:   "message is required",
		})
	}

	if chatCtx.chat.IsSandboxed() {
		if refusal := sandboxedSubagentRefusal(chatCtx.chat, toolArgs); refusal != "" {
			return marshalSubagentToolResult(runSubagentToolResult{Success: false, Error: refusal})
		}
		// Whatever a sandboxed chat passed, it gets no other persona and no skills.
		toolArgs.PersonalityID, toolArgs.RitualIDs, toolArgs.RitualIDsAlias = "", nil, nil
	}

	modelName := chatCtx.model
	modelID := chatCtx.chat.ModelID
	if strings.TrimSpace(toolArgs.Model) != "" {
		modelInput := strings.TrimSpace(toolArgs.Model)
		var overrideModel *models.Model
		// Try UUID first; fall back to case-insensitive name lookup.
		if overrideModelID, parseErr := uuid.Parse(modelInput); parseErr == nil {
			m, err := a.findModelByID(ctx, overrideModelID)
			if err != nil {
				return marshalSubagentToolResult(runSubagentToolResult{
					Success: false,
					Error:   err.Error(),
				})
			}
			overrideModel = m
		} else {
			m, err := a.findModelByName(ctx, modelInput)
			if err != nil {
				return marshalSubagentToolResult(runSubagentToolResult{
					Success: false,
					Error:   fmt.Sprintf("model %q not found by UUID or name: %v", modelInput, err),
				})
			}
			overrideModel = m
		}
		modelID = overrideModel.ID
		modelName = overrideModel.Name
	}

	personalityID := chatCtx.chat.PersonalityID
	systemPrompt := chatCtx.chat.SystemPrompt
	scratchpad := chatCtx.chat.Scratchpad
	if strings.TrimSpace(toolArgs.PersonalityID) != "" {
		overridePersonalityID, err := uuid.Parse(strings.TrimSpace(toolArgs.PersonalityID))
		if err != nil {
			return marshalSubagentToolResult(runSubagentToolResult{
				Success: false,
				Error:   "personality_id must be a valid UUID",
			})
		}
		personality, err := a.ds.GetPersonality(ctx, chatCtx.chat.UserID, overridePersonalityID)
		if err != nil {
			return marshalSubagentToolResult(runSubagentToolResult{
				Success: false,
				Error:   fmt.Sprintf("failed to get personality: %v", err),
			})
		}
		personalityID = personality.ID
		systemPrompt = personality.SystemPrompt
		scratchpad = personality.Scratchpad
	}
	scratchpad = subagentScratchpad(chatCtx.chat, scratchpad)

	// Enrich message with any requested ritual content and collect ritual IDs for MCP loading.
	ritualUUIDs := parseSkillIDs(toolArgs.RitualIDs)
	if len(ritualUUIDs) > 0 {
		rituals, fetchErr := a.ds.GetRitualsByIDs(ctx, chatCtx.chat.UserID, ritualUUIDs)
		if fetchErr == nil && len(rituals) > 0 {
			message += FormatRituals(rituals)
		}
	}

	subModelTier := ""
	if m, err := a.ds.GetModelByName(ctx, modelName); err == nil && m != nil {
		subModelTier = m.SubscriptionTier
	}
	qd := a.meter.Check(ctx, chatCtx.chat.UserID, subModelTier, models.ActionTypeJobRun)
	if !qd.Allowed {
		return marshalSubagentToolResult(runSubagentToolResult{
			Success: false,
			Error:   "active subscription or quota required for subagent",
		})
	}

	modelContext := buildSubagentModelContext(systemPrompt, scratchpad, message)
	subagentCtx := telemetry.WithCallPath(ctx, telemetry.CallPathSubagent)
	callResult, err := a.callSubagentModel(subagentCtx, chatCtx.chat.UserID, modelName, modelContext, ritualUUIDs, chatCtx.chat.IsSandboxed())
	if err != nil {
		return marshalSubagentToolResult(runSubagentToolResult{
			Success:       false,
			Model:         modelName,
			ModelID:       optionalUUIDString(modelID),
			PersonalityID: optionalUUIDString(personalityID),
			Error:         err.Error(),
		})
	}
	// The meter computes subagent LLM credits and records nothing when the charge
	// rounds to zero (subagent job runs are never free chat).
	a.meter.Record(ctx, qd, metering.Usage{
		UserID:      chatCtx.chat.UserID,
		ActionType:  models.ActionTypeJobRun,
		Model:       modelName,
		ChatID:      chatCtx.chat.ID.String(),
		Tokens:      callResult.InputTokens,
		SubagentRun: true,
	})

	return marshalSubagentToolResult(runSubagentToolResult{
		Success:       true,
		Model:         modelName,
		ModelID:       optionalUUIDString(modelID),
		PersonalityID: optionalUUIDString(personalityID),
		Output:        callResult.Output,
	})
}

func (a *Agent) findModelByID(ctx context.Context, modelID uuid.UUID) (*models.Model, error) {
	return a.resolveModelByReference(ctx, modelID.String(), modelResolverOptions{
		AllowDisplayName: false,
		AllowPrefixMatch: false,
	})
}

// findModelByName looks up an active model by case-insensitive name match.
func (a *Agent) findModelByName(ctx context.Context, name string) (*models.Model, error) {
	return a.resolveModelByReference(ctx, name, modelResolverOptions{
		AllowDisplayName: true,
		AllowPrefixMatch: true,
	})
}

// subagentScratchpad is the scratchpad a sub-agent started from chat receives. A sandboxed chat
// gives it none: the scratchpad is shared across the personality's conversations, and the
// sub-agent's output returns into this one.
func subagentScratchpad(chat *models.Chat, scratchpad string) string {
	if chat.IsSandboxed() {
		return ""
	}
	return scratchpad
}

func buildSubagentModelContext(systemPrompt, scratchpad, message string) *provider.ModelContext {
	modelContext := &provider.ModelContext{}
	modelContext.Append(provider.SegmentKindSystemPrompt, provider.RoleDeveloper, mergePrompts(baseSystemPrompt, systemPrompt), true)
	if strings.TrimSpace(scratchpad) != "" {
		modelContext.Append(provider.SegmentKindScratchpad, provider.RoleDeveloper, scratchpad, true)
	}
	modelContext.AppendUserMessage(provider.RoleUser, message, nil, false)
	return modelContext
}

// subagentModelCapabilities is what the models table (falling back to the seed
// catalog) says the subagent's target model can do. It is the same ToolSupport /
// VisionSupport data a normal chat turn is gated on (chat.ToolsEnabled,
// chatContext.modelVisionSupport), so a subagent never asks a model for more than
// a chat with that model would.
type subagentModelCapabilities struct {
	Provider      string
	ToolSupport   bool
	VisionSupport bool
}

func (a *Agent) subagentModelCapabilities(ctx context.Context, modelName string) subagentModelCapabilities {
	if a.ds != nil {
		if m, err := a.ds.GetModelByName(ctx, modelName); err == nil && m != nil {
			return subagentModelCapabilities{Provider: m.Provider, ToolSupport: m.ToolSupport, VisionSupport: m.VisionSupport}
		}
	}
	// No row: use the seed catalog, and otherwise assume the pre-existing behaviour
	// (tools allowed) since we have no capability data that says otherwise.
	if cfg := models.CatalogModel(modelName); cfg != nil {
		return subagentModelCapabilities{Provider: string(cfg.Provider), ToolSupport: cfg.ToolSupport, VisionSupport: cfg.VisionSupport}
	}
	return subagentModelCapabilities{ToolSupport: true}
}

// errSubagentToolsUnsupported reports that the selected skills would hand the
// subagent MCP tools but the target model is not flagged tool-capable. Failing
// loudly beats silently dropping the tools (the skill would run without the
// capability it exists for) or sending tools the model cannot call.
func errSubagentToolsUnsupported(provider models.ModelProvider, modelName string, toolCount int) error {
	return fmt.Errorf("%s model %q does not support tool calling, but the selected skills provide %d MCP tool(s); "+
		"choose a tool-capable model or run the subagent without skill_ids", provider, modelName, toolCount)
}

// subagentToolContext is the context a sub-agent's tool loop runs under. Its chat is a fresh
// conversation, but it carries the PARENT chat's sandbox, so every tool the sub-agent can
// invoke is gated exactly as it would be in the chat that started it.
func subagentToolContext(userID uuid.UUID, modelName string, sandboxed bool, mcpServers []*models.MCPServer, offered map[string]struct{}) *chatContext {
	c := &chatContext{
		userID:     userID,
		chat:       &models.Chat{ID: uuid.New(), UserID: userID, ContextScope: sandboxScope(sandboxed)},
		mcpServers: mcpServers,
		model:      modelName,
	}
	c.setOfferedTools(offered)
	return c
}

// openAIFunctionToolNames lists the function-tool names in an OpenAI tool list.
func openAIFunctionToolNames(toolParams []responses.ToolUnionParam) map[string]struct{} {
	out := make(map[string]struct{}, len(toolParams))
	for _, t := range toolParams {
		if t.OfFunction != nil {
			out[t.OfFunction.Name] = struct{}{}
		}
	}
	return out
}

// sandboxed is the parent chat's sandbox: the sub-agent's tool loop inherits it.
func (a *Agent) callSubagentModel(ctx context.Context, userID uuid.UUID, modelName string, modelContext *provider.ModelContext, ritualIDs []uuid.UUID, sandboxed bool) (*subagentCallResult, error) {
	caps := a.subagentModelCapabilities(ctx, modelName)
	modelProvider := caps.Provider
	if models.UsesOpenAIChatCompletionsAPI(modelProvider, modelName) {
		return a.callSubagentChatCompletions(ctx, userID, caps, modelName, modelContext, ritualIDs, sandboxed)
	}

	if models.UsesAnthropicMessagesAPI(modelProvider, modelName) {
		// z.ai GLM shares the Messages API but uses a different client.
		claudeProvider := a.ClaudeProvider
		if models.IsZAIModel(modelProvider, modelName) {
			claudeProvider = a.ZAIProvider
		}
		if claudeProvider == nil {
			if !models.IsZAIModel(modelProvider, modelName) {
				return nil, fmt.Errorf("Claude model %q requested but ANTHROPIC_API_KEY is not configured", modelName)
			}
			return nil, fmt.Errorf("z.ai model %q requested but ZAI_API_KEY is not configured", modelName)
		}
		claudeParams := modelContext.BuildClaudeParams(modelName)
		mcpSpecs, mcpServers := a.getSubagentMCPFunctionToolSpecs(ctx, userID, ritualIDs)
		if len(mcpSpecs) > 0 && !caps.ToolSupport {
			return nil, errSubagentToolsUnsupported(models.ProviderForModel(modelProvider, modelName), modelName, len(mcpSpecs))
		}
		if len(mcpSpecs) > 0 {
			adapter := provider.NewClaudeAdapter(claudeProvider, claudeParams, claudeFunctionTools(mcpSpecs), false, nil, nil)
			toolCtx := subagentToolContext(userID, modelName, sandboxed, mcpServers, offeredToolNames(mcpSpecs, nil))
			resp, _, _, err := a.handleAgentLoop(ctx, toolCtx, adapter)
			if err != nil {
				return nil, provider.WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Claude subagent MCP call failed: %w", err))
			}
			if resp != nil {
				return &subagentCallResult{
					Output:      strings.TrimSpace(resp.Text),
					InputTokens: resp.InputTokens,
				}, nil
			}
		}
		msg, err := claudeProvider.Call(ctx, claudeParams)
		if err != nil {
			return nil, provider.WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Claude subagent call failed: %w", err))
		}
		// ToGenerateResponse folds cached prefix tokens into InputTokens.
		resp := claudeProvider.ToGenerateResponse(msg)
		return &subagentCallResult{
			Output:      strings.TrimSpace(resp.Text),
			InputTokens: resp.InputTokens,
		}, nil
	}

	mcpTools := a.getSubagentMCPTools(ctx, userID, ritualIDs, modelName)
	if len(mcpTools) > 0 && !caps.ToolSupport {
		return nil, errSubagentToolsUnsupported(models.ProviderForModel(modelProvider, modelName), modelName, len(mcpTools))
	}
	params := modelContext.BuildOpenAIResponseParams(provider.OpenAIResponseParamsOptions{
		Model:             modelName,
		SafetyUserID:      userID.String(),
		MaxOutputTokens:   provider.DefaultMaxContentLength,
		ParallelToolCalls: false,
		Tools:             mcpTools,
		Instructions:      "",
	})
	adapter := provider.NewOpenAIAdapter(a.OpenAIProvider, params)
	toolCtx := subagentToolContext(userID, modelName, sandboxed, nil, openAIFunctionToolNames(mcpTools))
	if len(ritualIDs) > 0 {
		if servers, err := a.ds.ListRitualMCPServers(ctx, userID, ritualIDs); err == nil {
			toolCtx.mcpServers = servers
		}
	}
	final, _, _, err := a.handleAgentLoop(ctx, toolCtx, adapter)
	if err != nil {
		return nil, provider.WrapSafetyViolationError(models.SafetyViolationProviderOpenAI, fmt.Errorf("OpenAI subagent call failed: %w", err))
	}
	if final == nil {
		return nil, fmt.Errorf("OpenAI subagent call returned no final response")
	}
	return &subagentCallResult{
		Output:      strings.TrimSpace(final.Text),
		InputTokens: final.InputTokens,
	}, nil
}

// callSubagentChatCompletions runs a subagent turn on an OpenAI-compatible Chat
// Completions provider (Gemini, Mistral, DeepSeek, Qwen, Xiaomi MiMo). It reuses the
// chat-turn building blocks (request rendering, function-tool conversion, adapter
// selection and the missing-API-key errors) so these providers behave the same here
// as in a normal turn. Like the other subagent paths it only exposes the MCP tools of
// the requested skills, never the chat's own tools, and it renders no images (the
// subagent message carries none; visionRenderContext still guards the request).
func (a *Agent) callSubagentChatCompletions(ctx context.Context, userID uuid.UUID, caps subagentModelCapabilities, modelName string, modelContext *provider.ModelContext, ritualIDs []uuid.UUID, sandboxed bool) (*subagentCallResult, error) {
	mcpSpecs, mcpServers := a.getSubagentMCPFunctionToolSpecs(ctx, userID, ritualIDs)
	return a.runSubagentChatCompletions(ctx, userID, caps, modelName, modelContext, mcpSpecs, mcpServers, sandboxed)
}

// runSubagentChatCompletions is callSubagentChatCompletions after the skills' MCP
// tools have been discovered; the split keeps the capability gate and provider
// routing testable without a live MCP server.
func (a *Agent) runSubagentChatCompletions(ctx context.Context, userID uuid.UUID, caps subagentModelCapabilities, modelName string, modelContext *provider.ModelContext, mcpSpecs []agenttools.FunctionToolSpec, mcpServers []*models.MCPServer, sandboxed bool) (*subagentCallResult, error) {
	providerName := models.ProviderForModel(caps.Provider, modelName)
	renderCtx := &chatContext{model: modelName, modelProvider: caps.Provider, modelVisionSupport: caps.VisionSupport}
	params := buildOpenAIChatCompletionsParams(renderCtx, modelContext)

	if len(mcpSpecs) > 0 && !caps.ToolSupport {
		return nil, errSubagentToolsUnsupported(providerName, modelName, len(mcpSpecs))
	}
	functionTools := openAIChatCompletionFunctionTools(mcpSpecs)

	var (
		adapter provider.AgentAdapter
		err     error
		sv      models.SafetyViolationProvider
	)
	if models.IsGeminiModel(caps.Provider, modelName) {
		if a.GeminiProvider == nil {
			return nil, fmt.Errorf("Gemini model %q requested but GEMINI_API_KEY is not configured", modelName)
		}
		adapter = provider.NewGeminiAdapter(a.GeminiProvider, params, functionTools, nil, a.logger)
		sv = models.SafetyViolationProviderGoogle
	} else {
		adapter, err = a.openAIChatCompletionsAdapter(renderCtx, params, functionTools, nil)
		if err != nil {
			return nil, err
		}
		sv = models.SafetyViolationProvider(providerName)
	}

	toolCtx := subagentToolContext(userID, modelName, sandboxed, mcpServers, offeredToolNames(mcpSpecs, nil))
	toolCtx.chat.ToolsEnabled = caps.ToolSupport
	toolCtx.modelProvider = caps.Provider
	final, _, _, err := a.handleAgentLoop(ctx, toolCtx, adapter)
	if err != nil {
		return nil, provider.WrapSafetyViolationError(sv, fmt.Errorf("%s subagent call failed: %w", providerName, err))
	}
	if final == nil {
		return nil, fmt.Errorf("%s subagent call returned no final response", providerName)
	}
	return &subagentCallResult{
		Output:      strings.TrimSpace(final.Text),
		InputTokens: final.InputTokens,
	}, nil
}

// parseSkillIDs converts a slice of raw skill-ID strings (UUIDs) into []uuid.UUID,
// silently dropping any entries that are blank or fail to parse.
func parseSkillIDs(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func optionalUUIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func validateNoArgs(args []byte) error {
	if len(strings.TrimSpace(string(args))) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(args, &payload); err != nil {
		return err
	}
	if len(payload) > 0 {
		return fmt.Errorf("this tool does not accept arguments")
	}
	return nil
}

func marshalSubagentToolResult(result any) (string, error) {
	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
