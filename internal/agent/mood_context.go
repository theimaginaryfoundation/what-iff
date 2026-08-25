package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const moodAutoSelectModel = "gpt-4.1-mini"

type moodAutoSelectResult struct {
	MoodID string `json:"mood_id"`
}

var moodAutoSelectSchema = provider.GenerateSchema[moodAutoSelectResult]()

// resolveActiveMood determines the effective mood for this turn.
// chat.is_auto_mood controls policy; chat.active_mood_id stores the effective mood.
//
// Auto policy intentionally re-selects when effective mood is missing/invalid,
// not only on first turn. This lets chats recover if a mood is detached/deleted
// or if clients clear active_mood_id mid-conversation.
func (a *Agent) resolveActiveMood(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, userMessage string, currentMessageID uuid.UUID) *models.Mood {
	chat := chatCtx.chat
	_ = currentMessageID

	// Auto-mood policy can resolve missing/invalid mood; manual policy does not.
	autoPolicy := chat.IsAutoMood

	// ── 1. Existing effective mood ────────────────────────────────────────────
	if chat.ActiveMoodID != nil && *chat.ActiveMoodID != uuid.Nil {
		moodID := *chat.ActiveMoodID
		k, err := a.ds.GetMood(ctx, userID, moodID)
		if err != nil {
			a.logger.Warn("mood_context: failed to load active mood",
				zap.String("mood_id", chat.ActiveMoodID.String()), zap.Error(err))
			// Reconcile stale/invalid mood state.
			if clearErr := a.ds.SetChatActiveMood(ctx, userID, chat.ID, nil, nil, nil); clearErr != nil {
				a.logger.Warn("mood_context: failed to clear stale active mood",
					zap.String("mood_id", moodID.String()), zap.Error(clearErr))
			} else {
				chat.ActiveMoodID = nil
			}
		} else if chat.PersonalityID != uuid.Nil {
			// If pinned mood is no longer attached to this personality, clear and auto-resolve.
			moods, listErr := a.ds.GetMoodsForPersonality(ctx, userID, chat.PersonalityID)
			if listErr != nil {
				a.logger.Warn("mood_context: failed to verify active mood membership", zap.Error(listErr))
				return k
			}
			isAvailable := false
			for _, m := range moods {
				if m.ID == moodID {
					isAvailable = true
					break
				}
			}
			if isAvailable {
				return k
			}
			if clearErr := a.ds.SetChatActiveMood(ctx, userID, chat.ID, nil, nil, nil); clearErr != nil {
				a.logger.Warn("mood_context: failed to clear detached active mood",
					zap.String("mood_id", moodID.String()), zap.Error(clearErr))
				return k
			}
			chat.ActiveMoodID = nil
		} else {
			return k
		}
	}

	if !autoPolicy {
		return nil
	}

	// ── 2. Auto policy mood selection ────────────────────────────────────────
	if chat.PersonalityID == uuid.Nil {
		return nil
	}
	moods, err := a.ds.GetMoodsForPersonality(ctx, userID, chat.PersonalityID)
	if err != nil || len(moods) == 0 {
		return nil
	}

	if len(moods) == 1 {
		// Single mood: select directly and persist as current effective mood.
		a.persistActiveMood(ctx, userID, chat.ID, moods[0])
		return moods[0]
	}

	selected := a.autoSelectMood(ctx, moods, userMessage)
	if selected != nil {
		a.persistActiveMood(ctx, userID, chat.ID, selected)
	}
	return selected
}

func (a *Agent) isFirstUserMessageInChat(ctx context.Context, userID, chatID, currentMessageID uuid.UUID) bool {
	origin := models.MessageOriginUser
	resp, err := a.ds.ListChatMessages(ctx, userID, chatID, 1, 2, models.ChatMessageFilters{Origin: &origin})
	if err != nil {
		a.logger.Warn("mood_context: failed to determine first user message", zap.Error(err))
		return false
	}
	return isFirstUserMessageResult(resp.Results, currentMessageID)
}

func isFirstUserMessageResult(results []any, currentMessageID uuid.UUID) bool {
	userCount := 0
	for _, item := range results {
		msg, ok := item.(*models.ChatMessage)
		if !ok {
			continue
		}
		if msg.ID == currentMessageID {
			userCount++
			continue
		}
		userCount++
	}
	return userCount <= 1
}

// autoSelectMood asks GPT-4.1-mini to pick the best mood from the provided list for the user message.
func (a *Agent) autoSelectMood(ctx context.Context, moods []*models.Mood, userMessage string) *models.Mood {
	if a.OpenAIProvider == nil {
		return nil
	}
	// Mock/local mode: mood selection is a provider call — deliberately keep
	// the current mood instead of hitting the deny transport every auto-mood turn.
	if a.nonVendorLLM() {
		a.logger.Debug("mock/local mode: skipping auto mood selection")
		return nil
	}

	// Build a compact mood summary for the prompt.
	type moodSummary struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	summaries := make([]moodSummary, len(moods))
	for i, k := range moods {
		summaries[i] = moodSummary{ID: k.ID.String(), Name: k.Name, Description: k.Description}
	}
	summaryJSON, _ := json.Marshal(summaries)

	sysPrompt := "You are a routing assistant. Given a user message and a list of agent moods (moods), return ONLY the JSON object {\"mood_id\": \"<uuid>\"} for the best matching mood. Return exactly one mood. No explanation."
	userPrompt := fmt.Sprintf("User message:\n%s\n\nAvailable moods:\n%s", userMessage, string(summaryJSON))

	params := responses.ResponseNewParams{
		Model:        moodAutoSelectModel,
		Instructions: openai.String(sysPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(userPrompt, "user"),
			},
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "MoodAutoSelect",
					Schema:      moodAutoSelectSchema,
					Strict:      openai.Bool(true),
					Description: openai.String("Mood auto-selection JSON"),
					Type:        "json_schema",
				},
			},
		},
	}

	resp, err := a.OpenAIProvider.CallWithRetry(ctx, params)
	if err != nil {
		a.logger.Warn("mood auto-select: inference failed", zap.Error(err))
		return nil
	}

	raw := strings.TrimSpace(provider.ProcessResponseOutput(resp))
	// We rely on OpenAI structured output enforcement here; parser intentionally
	// expects JSON-only text (no prose wrappers or concatenated objects).
	a.logger.Debug("mood auto-select: raw structured output",
		zap.String("model", moodAutoSelectModel),
		zap.String("raw", raw))
	selectedID, err := parseMoodAutoSelectResult(raw)
	if err != nil {
		a.logger.Warn("mood auto-select: failed to parse structured response",
			zap.String("model", moodAutoSelectModel),
			zap.String("raw", raw),
			zap.Error(err))
		return nil
	}
	for _, k := range moods {
		if k.ID == selectedID {
			return k
		}
	}
	return nil
}

func parseMoodAutoSelectResult(raw string) (uuid.UUID, error) {
	var result moodAutoSelectResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
		return uuid.Nil, fmt.Errorf("unmarshal mood auto-select result: %w", err)
	}
	if strings.TrimSpace(result.MoodID) == "" {
		return uuid.Nil, fmt.Errorf("mood_id is required")
	}
	selectedID, err := uuid.Parse(result.MoodID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse mood_id: %w", err)
	}
	return selectedID, nil
}

// persistActiveMood saves the selected mood onto the chat record (best-effort).
func (a *Agent) persistActiveMood(ctx context.Context, userID, chatID uuid.UUID, mood *models.Mood) {
	if err := a.ds.SetChatActiveMood(ctx, userID, chatID, &mood.ID, nil, nil); err != nil {
		a.logger.Warn("mood_context: failed to persist active mood",
			zap.String("mood_id", mood.ID.String()), zap.Error(err))
	}
}

// moodContextOptions controls how the active mode block is worded for the model.
type moodContextOptions struct {
	IsAutoMood         bool
	MoodToolsAvailable bool
}

// formatMoodContextBlock builds the developer-facing mode context segment, including
// the mode name, policy, prompt snippet, attached skills, and optional tool guidance.
func formatMoodContextBlock(mood *models.Mood, rituals []*models.Ritual, opts moodContextOptions) string {
	if mood == nil {
		return ""
	}
	snippet := strings.TrimSpace(mood.PromptSnippet)
	ritualText := formatMoodRitualsForContext(rituals)
	if snippet == "" && ritualText == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("You are currently in %q mode", mood.Name))
	if opts.IsAutoMood {
		b.WriteString(" (auto-selected for this conversation)")
	} else {
		b.WriteString(" (locked for this conversation)")
	}
	b.WriteString(".")
	if desc := strings.TrimSpace(mood.Description); desc != "" {
		b.WriteString(" ")
		b.WriteString(desc)
	}
	if opts.MoodToolsAvailable {
		b.WriteString("\n\nIf this mode's context does not fit the user's message, use list_modes to review options and change_mode to switch.")
	}
	if snippet != "" {
		b.WriteString("\n\nThis mode adds the following context:\n\n")
		b.WriteString(snippet)
	}
	if ritualText != "" {
		b.WriteString("\n\nAttached skills for this mode:\n")
		b.WriteString(ritualText)
	}
	return b.String()
}

// formatMoodRitualsForContext renders mode-attached skills as a bullet list for the mode
// context segment. User-message ritual enrichment uses FormatRituals instead.
func formatMoodRitualsForContext(rituals []*models.Ritual) string {
	if len(rituals) == 0 {
		return ""
	}
	var b strings.Builder
	for _, ritual := range rituals {
		content := strings.TrimSpace(ritual.Content)
		if content == "" {
			continue
		}
		name := strings.TrimSpace(ritual.Name)
		if name == "" {
			b.WriteString(fmt.Sprintf("- %s\n", content))
			continue
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", name, content))
	}
	return strings.TrimRight(b.String(), "\n")
}

// appendMoodContextForChatTurn appends the formatted mode block after merged additional
// context (memories, etc.) and before expression continuity and the final user message,
// so the mood sits in the dynamic tail alongside memories for Claude prompt caching.
func appendMoodContextForChatTurn(modelCtx *provider.ModelContext, mood *models.Mood, rituals []*models.Ritual, opts moodContextOptions) {
	if modelCtx == nil {
		return
	}
	content := formatMoodContextBlock(mood, rituals, opts)
	if content == "" {
		return
	}
	modelCtx.Append(provider.SegmentKindMood, provider.RoleDeveloper, content, false)
}

// moodRitualIDs returns the ritual IDs from the active mood, or nil if no mood / no rituals.
func moodRitualIDs(mood *models.Mood) []uuid.UUID {
	if mood == nil || len(mood.RitualIDs) == 0 {
		return nil
	}
	return mood.RitualIDs
}

// activeMoodID returns a pointer to the mood's ID, or nil if mood is nil.
func activeMoodID(mood *models.Mood) *uuid.UUID {
	if mood == nil {
		return nil
	}
	id := mood.ID
	return &id
}

// ── Agent tool implementations ────────────────────────────────────────────────

type listMoodsResult struct {
	Success bool `json:"success"`
	// Keep legacy wire key for backward compatibility with existing clients.
	Moods []moodSummary `json:"moods,omitempty"`
	// Also expose the new terminology for clients that already switched to "mode".
	Modes []moodSummary `json:"modes,omitempty"`
	Error string        `json:"error,omitempty"`
}

type moodSummary struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	RecommendedModel string `json:"recommended_model,omitempty"`
	Active           bool   `json:"active,omitempty"`
}

func (a *Agent) listMoodsTool(ctx context.Context, chatCtx *chatContext, _ []byte) (string, error) {
	chat := chatCtx.chat
	moods, err := a.ds.GetMoodsForPersonality(ctx, chat.UserID, chat.PersonalityID)
	if err != nil {
		result, _ := json.Marshal(listMoodsResult{Success: false, Error: fmt.Sprintf("failed to list modes: %v", err)})
		return string(result), nil
	}

	items := make([]moodSummary, 0, len(moods))
	for _, k := range moods {
		items = append(items, moodSummary{
			ID:               k.ID.String(),
			Name:             k.Name,
			Description:      k.Description,
			RecommendedModel: k.RecommendedModel,
			Active:           chatCtx.activeMood != nil && chatCtx.activeMood.ID == k.ID,
		})
	}
	result, _ := json.Marshal(listMoodsResult{Success: true, Moods: items, Modes: items})
	return string(result), nil
}

type changeMoodArgs struct {
	ModeID        string `json:"mode_id"`
	MoodID        string `json:"mood_id,omitempty"`
	ModelOverride string `json:"model_override"`
}

type changeMoodResult struct {
	Success bool `json:"success"`
	// Keep legacy key for backward compatibility with existing clients.
	MoodID string `json:"mood_id,omitempty"`
	// Also expose the new terminology for clients that switched to "mode".
	ModeID string `json:"mode_id,omitempty"`
	Model  string `json:"model,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (a *Agent) changeMoodTool(ctx context.Context, chatCtx *chatContext, args []byte) (string, error) {
	chat := chatCtx.chat

	var req changeMoodArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &req); err != nil {
			result, _ := json.Marshal(changeMoodResult{Success: false, Error: "invalid arguments: " + err.Error()})
			return string(result), nil
		}
	}

	modeIDRaw := strings.TrimSpace(req.ModeID)
	// Backward compatibility for legacy callers.
	if modeIDRaw == "" {
		modeIDRaw = strings.TrimSpace(req.MoodID)
	}
	// Agents must always pick a specific mode; empty mode_id is invalid.
	if modeIDRaw == "" {
		result, _ := json.Marshal(changeMoodResult{Success: false, Error: "mode_id is required"})
		return string(result), nil
	}

	moodID, err := uuid.Parse(modeIDRaw)
	if err != nil {
		result, _ := json.Marshal(changeMoodResult{Success: false, Error: "invalid mode_id: " + err.Error()})
		return string(result), nil
	}

	mood, err := a.ds.GetMood(ctx, chat.UserID, moodID)
	if err != nil {
		result, _ := json.Marshal(changeMoodResult{Success: false, Error: fmt.Sprintf("mode not found: %v", err)})
		return string(result), nil
	}

	// Determine the target model.
	var modelID *uuid.UUID
	chosenModel := strings.TrimSpace(req.ModelOverride)
	explicitOverride := chosenModel != ""
	if chosenModel == "" {
		chosenModel = strings.TrimSpace(mood.RecommendedModel)
	}
	if chosenModel != "" {
		m, err := a.resolveModelByReference(ctx, chosenModel, modelResolverOptions{
			AllowDisplayName: true,
			AllowPrefixMatch: true,
		})
		if err == nil {
			id := m.ID
			modelID = &id
			chatCtx.model = m.Name
			chat.ModelID = m.ID
			chat.ModelName = m.Name
		} else if explicitOverride {
			result, _ := json.Marshal(changeMoodResult{Success: false, Error: "model_override not found: " + chosenModel})
			return string(result), nil
		}
	}

	// change_mode updates the effective mode; it should not change auto/manual policy.
	if err := a.ds.SetChatActiveMood(ctx, chat.UserID, chat.ID, &moodID, modelID, nil); err != nil {
		result, _ := json.Marshal(changeMoodResult{Success: false, Error: fmt.Sprintf("failed to set mode: %v", err)})
		return string(result), nil
	}

	chatCtx.activeMood = mood
	chat.ActiveMoodID = &moodID

	resultModel := chatCtx.model
	if modelID != nil {
		resultModel = chat.ModelName
	}
	result, _ := json.Marshal(changeMoodResult{
		Success: true,
		MoodID:  moodID.String(),
		ModeID:  moodID.String(),
		Model:   resultModel,
	})
	return string(result), nil
}
