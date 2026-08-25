package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const expressionPickModel = chatNameModel

// expressionPickStructuredOut is the OpenAI structured-output shape for the portrait classifier.
type expressionPickStructuredOut struct {
	ExpressionKey string `json:"expression_key"`
	Reasoning     string `json:"reasoning"`
}

var expressionPickStructuredSchema = provider.GenerateSchema[expressionPickStructuredOut]()

var expressionPickInstructions = strings.TrimSpace(`
Classify the latest assistant reply: pick exactly one expression portrait that best matches the assistant's tone, stance, and intent in context.

Your reply must match the configured JSON schema (expression_key + reasoning).

CRITICAL — valid keys only:
- The expression_key MUST be copied exactly from the "Available expressions" list in this message (character-for-character match to the key after each "- ").
- You MUST NOT invent, paraphrase, or substitute keys (e.g. do not output "focused", "neutral", "calm", "serious", or any word that is not listed).
- If no listed key feels perfect, choose the closest listed key anyway; never output an unlisted key.

Expression list format: each line is "- key" or "- key — label".

If a label exists (after the em dash), treat it as ground truth for what that expression means.
If no label, infer meaning only from the key name and conversation context — still pick a listed key.

Selection guidance:

- Bias toward the assistant reply's emotional posture, not the user's emotion by itself.
- Use the user's message as context for what the assistant is responding to, but do not simply mirror the user's tone or mood.
- Map the assistant's reaction (consoling, amused, surprised, concerned, playful, skeptical, delighted, attentive, etc.) to the best matching listed key — not to a new key name.
- Avoid defaulting to baseline expressions like "content" or "thinking" just because the topic is normal, technical, or complex. Use them only when they are the most specific listed fit for the reply.
- When more than one listed expression is appropriate, prefer variety and avoid repeating the previous assistant expression if another listed key fits this reply well.
- Choose exactly one expression_key from the list; do not hedge or describe multiple options in the final answer.

Include a non-empty reasoning string whenever possible (one or two sentences).
`)

func expressionPickStructuredOutputFormat() responses.ResponseTextConfigParam {
	return responses.ResponseTextConfigParam{
		Format: responses.ResponseFormatTextConfigUnionParam{
			OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
				Name:        "ExpressionPortraitPick",
				Schema:      expressionPickStructuredSchema,
				Strict:      openai.Bool(true),
				Description: openai.String("Portrait expression classifier output"),
				Type:        "json_schema",
			},
		},
	}
}

// buildExpressionPickUserMessageBody is the final user turn for the expression picker when using a forked
// inference ModelContext (task instructions + catalog live in the user message, like scratchpad follow-ups).
func buildExpressionPickUserMessageBody(catalogBlock string) string {
	return strings.TrimSpace(expressionPickInstructions + "\n\nAvailable expressions:\n" + catalogBlock)
}

// PickGenerationExpression uses a small model to choose a personality expression for the assistant turn.
//
// When inferenceModelCtx is non-nil, the OpenAI request forks that context—the same segments as the main
// inference call (system prompt, scratchpad, checkpoint summary, memories, mood, history, user turn)—then
// appends this assistant reply as a history turn and the expression task as an additional user message,
// so picking runs in full agent context. Instructions reuse the merged system prompt from the fork (via
// BuildOpenAIResponseParams). When inferenceModelCtx is nil, uses a minimal standalone prompt (defensive).
//
// On classifier failure or invalid output, returns nil id (no portrait); callers should not assume a default slot.
func (a *Agent) PickGenerationExpression(ctx context.Context, userID, personalityID uuid.UUID, inferenceModelCtx *provider.ModelContext, userTurn, assistantReply string) (*uuid.UUID, string, error) {
	slots, err := a.ds.ListPersonalityExpressions(ctx, userID, personalityID)
	if err != nil {
		return nil, "", fmt.Errorf("list expressions: %w", err)
	}
	if len(slots) == 0 {
		return nil, "", nil
	}

	catalog := buildExpressionPickCatalog(slots)

	var params responses.ResponseNewParams
	if inferenceModelCtx != nil {
		fork := inferenceModelCtx.Clone()
		// Structured JSON output on the Responses API rejects multimodal input_image on
		// the same request; continuity may attach expression portrait thumbnails.
		fork.StripUserMessageImages()
		if reply := strings.TrimSpace(assistantReply); reply != "" {
			fork.Append(provider.SegmentKindHistoryTurn, provider.RoleAssistant, reply, false)
		}
		fork.AppendUserMessage(provider.RoleUser, buildExpressionPickUserMessageBody(catalog), nil, false)

		params = fork.BuildOpenAIResponseParams(provider.OpenAIResponseParamsOptions{
			Model:           expressionPickModel,
			SafetyUserID:    userID.String(),
			MaxOutputTokens: 384,
		})
		params.Temperature = openai.Float(0.2)
		params.Text = expressionPickStructuredOutputFormat()
	} else {
		userPayload := strings.TrimSpace(fmt.Sprintf(`
Available expressions:
%s

Latest user message:
%s

Assistant reply to evaluate:
%s
`, catalog, strings.TrimSpace(userTurn), strings.TrimSpace(assistantReply)))
		params = responses.ResponseNewParams{
			Model:           expressionPickModel,
			Temperature:     openai.Float(0.2),
			MaxOutputTokens: openai.Int(384),
			Instructions:    openai.String(expressionPickInstructions),
			Input: responses.ResponseNewParamsInputUnion{
				OfString: openai.String(userPayload),
			},
			Text: expressionPickStructuredOutputFormat(),
		}
	}

	resp, err := a.OpenAIProvider.CallWithRetry(ctx, params)
	if err != nil {
		a.logger.Warn("expression picker model call failed", zap.Error(err))
		return nil, "", nil
	}

	raw := strings.TrimSpace(provider.ProcessResponseOutput(resp))
	if raw == "" {
		return nil, "", nil
	}

	key, reasoning := parseExpressionPickPayload(raw)
	if key == "" {
		return nil, "", nil
	}

	for i := range slots {
		if slots[i].ExpressionKey == key {
			return &slots[i].ID, truncateExpressionReasoning(reasoning), nil
		}
	}

	a.logger.Debug("expression picker returned unknown key",
		zap.String("expression_key", key))
	return nil, "", nil
}

// buildExpressionPickCatalog formats one line per expression: always "- key" or "- key — label" when a label exists.
func buildExpressionPickCatalog(slots []models.PersonalityExpression) string {
	var lines []string
	for _, s := range slots {
		label := ""
		if s.Label != nil {
			label = strings.TrimSpace(*s.Label)
		}
		line := fmt.Sprintf("- %s", s.ExpressionKey)
		if label != "" {
			line += " — " + label
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// stripMarkdownJSONFence trims optional GitHub-style ``` / ```json wrappers so JSON decoding can succeed.
func stripMarkdownJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSpace(s)
		if strings.HasPrefix(strings.ToLower(s), "json") {
			s = strings.TrimSpace(s[len("json"):])
		}
		s = strings.TrimSpace(s)
	}
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}

// decodeExpressionPickStructured extracts strict classifier JSON from raw model text (plain JSON,
// markdown-fenced JSON, or prose containing an embedded JSON object).
func decodeExpressionPickStructured(text string) (expressionPickStructuredOut, bool) {
	text = stripMarkdownJSONFence(text)
	text = strings.TrimSpace(text)
	if text == "" {
		return expressionPickStructuredOut{}, false
	}

	tryParse := func(blob string) (expressionPickStructuredOut, bool) {
		blob = strings.TrimSpace(blob)
		if blob == "" {
			return expressionPickStructuredOut{}, false
		}
		var p expressionPickStructuredOut
		if err := json.Unmarshal([]byte(blob), &p); err != nil {
			return expressionPickStructuredOut{}, false
		}
		if strings.TrimSpace(p.ExpressionKey) == "" {
			return expressionPickStructuredOut{}, false
		}
		return p, true
	}

	if p, ok := tryParse(text); ok {
		return p, true
	}

	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var p expressionPickStructuredOut
		if err := dec.Decode(&p); err != nil {
			continue
		}
		if strings.TrimSpace(p.ExpressionKey) == "" {
			continue
		}
		return p, true
	}
	return expressionPickStructuredOut{}, false
}

const maxExpressionReasoningRunes = 512

func truncateExpressionReasoning(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxExpressionReasoningRunes {
		return s
	}
	return strings.TrimSpace(string(r[:maxExpressionReasoningRunes]))
}

func parseExpressionPickPayload(text string) (key, reasoning string) {
	p, ok := decodeExpressionPickStructured(text)
	if !ok {
		return "", ""
	}
	return strings.TrimSpace(p.ExpressionKey), truncateExpressionReasoning(p.Reasoning)
}
