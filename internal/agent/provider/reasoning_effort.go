package provider

import (
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// User-selected reasoning effort (ADR 0x026) mapped onto each provider's request. Every
// function here takes the effective effort (models.EffectiveReasoningEffort): "" means no
// selection, and the request is left exactly as it was before the setting existed.

// ApplyOpenAIReasoning sets reasoning.effort on a Responses request. off maps to "none", or
// "minimal" for the original GPT-5 models, which do not accept "none". Any other level also
// asks for reasoning summaries, which the adapter captures as the turn's reasoning text, and
// raises the output cap so reasoning does not crowd out the reply.
func ApplyOpenAIReasoning(params *responses.ResponseNewParams, effort models.ReasoningEffort) {
	if params == nil || effort == "" {
		return
	}
	if effort == models.ReasoningEffortOff {
		params.Reasoning.Effort = openAINoReasoningEffort(string(params.Model))
		return
	}
	params.Reasoning.Effort = shared.ReasoningEffort(effort)
	params.Reasoning.Summary = shared.ReasoningSummaryAuto
	raiseOpenAIOutputCap(params)
}

// openAINoReasoningEffort is the lowest effort a GPT-5-family model accepts: "none" from
// gpt-5.1 on, "minimal" before it (models.OpenAIRejectsNoneEffort).
func openAINoReasoningEffort(model string) shared.ReasoningEffort {
	if models.OpenAIRejectsNoneEffort(model) {
		return shared.ReasoningEffortMinimal
	}
	return shared.ReasoningEffortNone
}

func raiseOpenAIOutputCap(params *responses.ResponseNewParams) {
	// Only raise a cap that is set; an uncapped request stays uncapped.
	if params.MaxOutputTokens.Valid() && params.MaxOutputTokens.Value < ReasoningMaxOutputTokens {
		params.MaxOutputTokens = openai.Int(ReasoningMaxOutputTokens)
	}
}

// ApplyClaudeReasoning maps an effort onto a native Anthropic Messages request and reports
// whether thinking blocks must be replayed across tool rounds (the API requires the assistant
// turn that called a tool to keep its thinking when thinking is on).
//   - low/medium/high: adaptive thinking with summarized display and output_config.effort;
//     models that predate adaptive thinking and effort (models.AnthropicUsesThinkingBudget: the
//     4.5 generation and older) get a thinking budget instead.
//   - off: thinking disabled. Models that cannot disable thinking never get off: their
//     reasoning support is "always", which runs off as low.
func ApplyClaudeReasoning(params *anthropic.MessageNewParams, effort models.ReasoningEffort) (replayThinking bool) {
	if params == nil || effort == "" {
		return false
	}
	model := strings.ToLower(string(params.Model))
	if effort == models.ReasoningEffortOff {
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
		return false
	}
	if params.MaxTokens < ReasoningMaxOutputTokens {
		params.MaxTokens = ReasoningMaxOutputTokens
	}
	if models.AnthropicUsesThinkingBudget(model) {
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(claudeThinkingBudget(effort))
		return true
	}
	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	if !models.AnthropicSummarizesThinkingByDefault(model) {
		// Newer models omit thinking text unless asked; the turn shows it, so ask for summaries.
		adaptive.SetExtraFields(map[string]any{"display": "summarized"})
	}
	params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
	params.OutputConfig.Effort = anthropic.OutputConfigEffort(effort)
	return true
}

// ClaudeHighEffortMaxOutputTokens is the output cap for a streamed Claude call at high effort,
// where thinking alone can use most of ReasoningMaxOutputTokens. Non-streamed calls (subagents)
// keep ReasoningMaxOutputTokens: Anthropic's SDK refuses a non-streamed request whose max_tokens
// implies more than about ten minutes of output.
const ClaudeHighEffortMaxOutputTokens = 4 * DefaultMaxContentLength

// RaiseClaudeStreamingOutputCap raises a streamed call's output cap at high effort.
func RaiseClaudeStreamingOutputCap(params *anthropic.MessageNewParams, effort models.ReasoningEffort) {
	if params != nil && effort == models.ReasoningEffortHigh && params.MaxTokens < ClaudeHighEffortMaxOutputTokens {
		params.MaxTokens = ClaudeHighEffortMaxOutputTokens
	}
}

// claudeThinkingBudget is the budget_tokens for models that still take one. It stays under
// ReasoningMaxOutputTokens so the reply keeps room.
func claudeThinkingBudget(effort models.ReasoningEffort) int64 {
	switch effort {
	case models.ReasoningEffortLow:
		return 2048
	case models.ReasoningEffortHigh:
		return 12288
	}
	return 6144
}

// ZAIReasoningEffortFor maps an effort onto z.ai GLM's output_config.effort, which accepts only
// low/high/max. GLM always reasons: no selection keeps today's fixed ZAIReasoningEffort, and
// medium maps to that same value.
func ZAIReasoningEffortFor(effort models.ReasoningEffort) anthropic.OutputConfigEffort {
	switch effort {
	case models.ReasoningEffortOff, models.ReasoningEffortLow:
		return anthropic.OutputConfigEffortLow
	case models.ReasoningEffortHigh:
		return anthropic.OutputConfigEffortMax
	}
	return ZAIReasoningEffort
}

// Chat Completions providers (Gemini, Qwen, DeepSeek, Mistral). Their requests share one
// params type; provider-specific fields go in through mergeChatExtraFields so one setting never
// erases another.

// mergeChatExtraFields adds fields to params' extra JSON fields, keeping any already set.
func mergeChatExtraFields(params *openai.ChatCompletionNewParams, fields map[string]any) {
	merged := map[string]any{}
	for k, v := range params.ExtraFields() {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	params.SetExtraFields(merged)
}

// raiseChatOutputCap gives reasoning room: the Chat Completions output cap
// (max_completion_tokens) counts thinking tokens on every provider here. An uncapped request
// stays uncapped.
func raiseChatOutputCap(params *openai.ChatCompletionNewParams) {
	if params.MaxCompletionTokens.Valid() && params.MaxCompletionTokens.Value < ReasoningMaxOutputTokens {
		params.MaxCompletionTokens = openai.Int(ReasoningMaxOutputTokens)
	}
}

// ApplyGeminiReasoning sets reasoning_effort on Google's OpenAI-compatible endpoint, which maps it
// onto thinking_level (Gemini 3+) or a thinking budget (2.5). Off ("none") is sent only to models
// that can disable thinking (2.5 Flash); Gemini 3+ always reasons, so its off arrives here as low
// (models.ReasoningSupportFor). Thought text is not requested: the compatible endpoint does not
// document how it returns it (it would need include_thoughts in extra_body, which cannot be
// combined with reasoning_effort).
func ApplyGeminiReasoning(params *openai.ChatCompletionNewParams, effort models.ReasoningEffort) {
	if params == nil || effort == "" {
		return
	}
	if effort == models.ReasoningEffortOff {
		params.ReasoningEffort = shared.ReasoningEffortNone
		return
	}
	params.ReasoningEffort = shared.ReasoningEffort(effort)
	raiseChatOutputCap(params)
}

// ApplyQwenReasoning sets DashScope's thinking fields. Off sends enable_thinking=false. A level
// turns thinking on and bounds it: Qwen 3.8 takes reasoning_effort (low/medium/xhigh; high maps to
// xhigh), earlier hybrid models take a thinking_budget, which DashScope rejects alongside
// reasoning_effort.
func ApplyQwenReasoning(params *openai.ChatCompletionNewParams, effort models.ReasoningEffort) {
	if params == nil || effort == "" {
		return
	}
	if effort == models.ReasoningEffortOff {
		mergeChatExtraFields(params, map[string]any{"enable_thinking": false})
		return
	}
	if models.QwenUsesReasoningEffort(string(params.Model)) {
		level := shared.ReasoningEffort(effort)
		if effort == models.ReasoningEffortHigh {
			level = shared.ReasoningEffortXhigh
		}
		params.ReasoningEffort = level
		mergeChatExtraFields(params, map[string]any{"enable_thinking": true})
	} else {
		mergeChatExtraFields(params, map[string]any{"enable_thinking": true, "thinking_budget": claudeThinkingBudget(effort)})
	}
	raiseChatOutputCap(params)
}

// ApplyDeepSeekReasoning sets DeepSeek's reasoning_effort (none/low/high/max; thinking is on at
// high when nothing is sent). Off also sends thinking disabled, to be explicit; medium maps to
// high (DeepSeek has no medium) and high to max.
func ApplyDeepSeekReasoning(params *openai.ChatCompletionNewParams, effort models.ReasoningEffort) {
	if params == nil || effort == "" {
		return
	}
	switch effort {
	case models.ReasoningEffortOff:
		params.ReasoningEffort = shared.ReasoningEffortNone
		mergeChatExtraFields(params, map[string]any{"thinking": map[string]any{"type": "disabled"}})
		return
	case models.ReasoningEffortLow:
		params.ReasoningEffort = shared.ReasoningEffortLow
	case models.ReasoningEffortMedium:
		params.ReasoningEffort = shared.ReasoningEffortHigh
	case models.ReasoningEffortHigh:
		params.ReasoningEffort = shared.ReasoningEffort("max")
	}
	raiseChatOutputCap(params)
}

// ApplyMistralReasoning sets reasoning_effort on Mistral's hybrid models, which document only
// "none" and "high" (a toggle). With reasoning on, Mistral returns content as typed chunks, which
// the Mistral adapter parses (mistral_content.go).
func ApplyMistralReasoning(params *openai.ChatCompletionNewParams, effort models.ReasoningEffort) {
	if params == nil || effort == "" {
		return
	}
	if effort == models.ReasoningEffortOff {
		params.ReasoningEffort = shared.ReasoningEffortNone
		return
	}
	params.ReasoningEffort = shared.ReasoningEffortHigh
	raiseChatOutputCap(params)
}
