package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

type runSubagentToolArgs struct {
	Message       string   `json:"message"`
	PersonalityID string   `json:"personality_id,omitempty"`
	Model         string   `json:"model,omitempty"`
	RitualIDs     []string `json:"skill_ids,omitempty"`
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
	callResult, err := a.callSubagentModel(subagentCtx, chatCtx.chat.UserID, modelName, modelContext, ritualUUIDs)
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

func buildSubagentModelContext(systemPrompt, scratchpad, message string) *provider.ModelContext {
	modelContext := &provider.ModelContext{}
	modelContext.Append(provider.SegmentKindSystemPrompt, provider.RoleDeveloper, mergePrompts(baseSystemPrompt, systemPrompt), true)
	if strings.TrimSpace(scratchpad) != "" {
		modelContext.Append(provider.SegmentKindScratchpad, provider.RoleDeveloper, scratchpad, true)
	}
	modelContext.AppendUserMessage(provider.RoleUser, message, nil, false)
	return modelContext
}

func (a *Agent) callSubagentModel(ctx context.Context, userID uuid.UUID, modelName string, modelContext *provider.ModelContext, ritualIDs []uuid.UUID) (*subagentCallResult, error) {
	modelProvider := ""
	if a.ds != nil {
		if m, err := a.ds.GetModelByName(ctx, modelName); err == nil && m != nil {
			modelProvider = m.Provider
		}
	}
	if models.UsesOpenAIChatCompletionsAPI(modelProvider, modelName) {
		return nil, fmt.Errorf("%s subagent calls are not yet supported", models.ProviderForModel(modelProvider, modelName))
	}

	if models.UsesAnthropicMessagesAPI(modelProvider, modelName) {
		// z.ai GLM shares the Messages API but uses a different client and does not
		// support Anthropic beta MCP.
		claudeProvider := a.ClaudeProvider
		nativeAnthropic := true
		if models.IsZAIModel(modelProvider, modelName) {
			claudeProvider = a.ZAIProvider
			nativeAnthropic = false
		}
		if claudeProvider == nil {
			if nativeAnthropic {
				return nil, fmt.Errorf("Claude model %q requested but ANTHROPIC_API_KEY is not configured", modelName)
			}
			return nil, fmt.Errorf("z.ai model %q requested but ZAI_API_KEY is not configured", modelName)
		}
		claudeParams := modelContext.BuildClaudeParams(modelName)
		if nativeAnthropic {
			mcpConfig := a.getSubagentClaudeMCPConfig(ctx, userID, ritualIDs)
			if mcpConfig != nil {
				betaParams, err := provider.BuildClaudeBetaMCPParams(claudeParams, mcpConfig)
				if err == nil {
					msg, betaErr := claudeProvider.CallBeta(ctx, betaParams)
					if betaErr != nil {
						return nil, provider.WrapSafetyViolationError(models.SafetyViolationProviderAnthropic, fmt.Errorf("Claude subagent MCP call failed: %w", betaErr))
					}
					// BetaToGenerateResponse folds cached prefix tokens into InputTokens.
					resp := claudeProvider.BetaToGenerateResponse(msg)
					return &subagentCallResult{
						Output:      strings.TrimSpace(resp.Text),
						InputTokens: resp.InputTokens,
					}, nil
				}
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
	params := modelContext.BuildOpenAIResponseParams(provider.OpenAIResponseParamsOptions{
		Model:             modelName,
		SafetyUserID:      userID.String(),
		MaxOutputTokens:   provider.DefaultMaxContentLength,
		ParallelToolCalls: false,
		Tools:             mcpTools,
		Instructions:      "",
	})
	resp, err := a.OpenAIProvider.CallWithRetry(ctx, params)
	if err != nil {
		return nil, provider.WrapSafetyViolationError(models.SafetyViolationProviderOpenAI, fmt.Errorf("OpenAI subagent call failed: %w", err))
	}
	return &subagentCallResult{
		Output:      strings.TrimSpace(provider.ProcessResponseOutput(resp)),
		InputTokens: resp.Usage.InputTokens,
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
