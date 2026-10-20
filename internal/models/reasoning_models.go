package models

import "strings"

// Model-name rules for reasoning effort (ADR 0x026), in one place. Providers name their model
// families by prefix, and which reasoning controls a model takes depends on the family, not only
// the provider. ReasoningSupportFor (reasoning.go) and the provider request mappings
// (internal/agent/provider/reasoning_effort.go) both read these tables, so a new model family is
// a table edit here. Each table cites the provider doc it follows; models.reasoning_test.go pins
// the matrix.
//
// The tables fail closed: a model they do not list gets no reasoning control (nothing is sent,
// so the request is what it was before ADR 0x026). Sending a control a model rejects fails the
// turn, while leaving a new model out only hides the setting until it is added.

// --- Anthropic (platform.claude.com/docs/en/build-with-claude/extended-thinking, /effort) ---

// anthropicAlwaysThinking are models that reject a request disabling thinking: effort is their
// only control. Sonnet 5.5 can only switch thinking off with a "between_tools" mode the request
// layer does not model; treating it as always-on keeps off at low effort, which it accepts.
var anthropicAlwaysThinking = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-fable", "claude-mythos"}

// anthropicSummarizesByDefault are models that return summarized thinking without a display
// setting (the 4.6 generation), so none is sent.
var anthropicSummarizesByDefault = []string{"claude-opus-4-6", "claude-sonnet-4-6"}

// anthropicAdaptive are the models that take adaptive thinking with an effort and can also
// disable thinking.
var anthropicAdaptive = []string{
	"claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-7", "claude-opus-4-8",
	"claude-opus-5", "claude-sonnet-5",
}

// anthropicThinkingBudget are models that predate adaptive thinking and effort: they take a
// thinking budget instead. Dated and "-0" snapshot names of the 4.0 models are listed so the
// plain "claude-sonnet-4" prefix cannot catch the 4.6 generation.
var anthropicThinkingBudget = []string{
	"claude-haiku-4-5", "claude-sonnet-4-5", "claude-opus-4-5", "claude-opus-4-1",
	"claude-opus-4-0", "claude-opus-4-2025", "claude-sonnet-4-0", "claude-sonnet-4-2025",
	"claude-3-7-sonnet",
}

func anthropicReasoningSupport(n string) ReasoningSupport {
	switch {
	case IsAnthropicAlwaysThinkingModel(n):
		return ReasoningSupportAlways
	case hasPrefix(n, anthropicAdaptive), hasPrefix(n, anthropicThinkingBudget):
		return ReasoningSupportOptional
	}
	return ReasoningSupportNone
}

// IsAnthropicAlwaysThinkingModel reports whether a native Anthropic model cannot disable thinking.
func IsAnthropicAlwaysThinkingModel(name string) bool {
	return hasPrefix(name, anthropicAlwaysThinking)
}

// AnthropicSummarizesThinkingByDefault reports whether a model needs no thinking display setting.
func AnthropicSummarizesThinkingByDefault(name string) bool {
	return hasPrefix(name, anthropicSummarizesByDefault)
}

// AnthropicUsesThinkingBudget reports whether a model takes budget_tokens rather than effort.
func AnthropicUsesThinkingBudget(name string) bool { return hasPrefix(name, anthropicThinkingBudget) }

// --- OpenAI (developers.openai.com/api/docs/guides/reasoning) ---

var (
	// openAIEffortModels take reasoning.effort including an off value ("none" or "minimal").
	openAIEffortModels = []string{"gpt-5"}
	// openAIAlwaysReasoning are the o-series: they take low/medium/high but cannot turn
	// reasoning off.
	openAIAlwaysReasoning = []string{"o1", "o3", "o4"}
	// openAINoEffort are names in those families without an effort control: chat-tuned aliases,
	// the pro models (fixed at high), and the first o1 previews.
	openAINoEffort = []string{"-chat", "-pro", "o1-mini", "o1-preview"}
)

func openAIReasoningSupport(n string) ReasoningSupport {
	switch {
	case hasAny(n, openAINoEffort...):
		return ReasoningSupportNone
	case hasPrefix(n, openAIEffortModels):
		return ReasoningSupportOptional
	case hasPrefix(n, openAIAlwaysReasoning):
		return ReasoningSupportAlways
	}
	return ReasoningSupportNone
}

// openAIMinimalOnly are the original GPT-5 models, which take "minimal" but not "none" (gpt-5.1
// and later take "none"). Dated snapshots of these names are covered by OpenAIRejectsNoneEffort.
var openAIMinimalOnly = []string{"gpt-5", "gpt-5-mini", "gpt-5-nano"}

// OpenAIRejectsNoneEffort reports whether a model's lowest effort is "minimal" rather than "none".
func OpenAIRejectsNoneEffort(name string) bool {
	n := normalizeModelName(name)
	for _, m := range openAIMinimalOnly {
		if n == m || strings.HasPrefix(n, m+"-20") {
			return true
		}
	}
	return false
}

// --- Gemini (ai.google.dev/gemini-api/docs/openai, /thinking) ---

// geminiNoReasoning marks media and non-chat model names; geminiThinkingOptional can disable
// thinking (budget 0 / effort "none"); geminiAlwaysThinking cannot.
var (
	geminiNoReasoning      = []string{"image", "tts", "live", "embedding", "nano-banana", "veo", "imagen"}
	geminiThinkingOptional = []string{"gemini-2.5-flash"}
	geminiAlwaysThinking   = []string{"gemini-2.5-pro", "gemini-3"}
)

func geminiReasoningSupport(n string) ReasoningSupport {
	switch {
	case hasAny(n, geminiNoReasoning...):
		return ReasoningSupportNone
	case hasPrefix(n, geminiThinkingOptional):
		return ReasoningSupportOptional
	case hasPrefix(n, geminiAlwaysThinking):
		return ReasoningSupportAlways
	}
	return ReasoningSupportNone
}

// --- Qwen (alibabacloud.com/help/en/model-studio/deep-thinking) ---

var (
	// qwenThinkingOnly cannot disable thinking.
	qwenThinkingOnly = []string{"qwen3.8-2.4t", "qwen3.7-max-preview", "qwen3.7-max-2026-05-17"}
	// qwenNoReasoning marks media, translation and embedding names.
	qwenNoReasoning = []string{"image", "tts", "embedding", "-mt-", "-vl", "asr"}
	// qwenHybrid take enable_thinking (Qwen 3.x and the commercial qwen-plus / qwen-flash).
	qwenHybrid = []string{"qwen3", "qwen-plus", "qwen-flash"}
	// qwenEffort take reasoning_effort (low/medium/xhigh) instead of a thinking budget.
	qwenEffort = []string{"qwen3.8"}
)

func qwenReasoningSupport(n string) ReasoningSupport {
	switch {
	case hasPrefix(n, qwenThinkingOnly):
		return ReasoningSupportAlways
	case hasAny(n, qwenNoReasoning...):
		return ReasoningSupportNone
	case hasPrefix(n, qwenHybrid):
		return ReasoningSupportOptional
	}
	return ReasoningSupportNone
}

// QwenUsesReasoningEffort reports whether a Qwen model takes reasoning_effort rather than a
// thinking budget (DashScope rejects the two together).
func QwenUsesReasoningEffort(name string) bool { return hasPrefix(name, qwenEffort) }

// --- DeepSeek (api-docs.deepseek.com/guides/thinking_mode) ---

// deepSeekHybrid are the hybrid models (reasoning_effort none/low/high/max, thinking on by
// default). The legacy deepseek-chat / deepseek-reasoner aliases and the non-chat models are not
// listed.
var (
	deepSeekHybrid      = []string{"deepseek-flash", "deepseek-v4"}
	deepSeekNoReasoning = []string{"-vl", "ocr", "coder"}
)

func deepSeekReasoningSupport(n string) ReasoningSupport {
	if hasPrefix(n, deepSeekHybrid) && !hasAny(n, deepSeekNoReasoning...) {
		return ReasoningSupportOptional
	}
	return ReasoningSupportNone
}

// --- Mistral (docs.mistral.ai/studio/conversations/reasoning) ---

// mistralReasoning are the hybrid models documented to take reasoning_effort ("none"/"high" only,
// hence a toggle). Aliases (mistral-small-latest, mistral-large-latest, ...) are left out: their
// target changes without notice.
var mistralReasoning = []string{"mistral-small-4", "mistral-medium-3-5", "mistral-medium-3.5", "mistral-large-4"}

func mistralReasoningSupport(n string) ReasoningSupport {
	if hasPrefix(n, mistralReasoning) {
		return ReasoningSupportToggle
	}
	return ReasoningSupportNone
}

// --- helpers ---

func normalizeModelName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func hasPrefix(name string, prefixes []string) bool {
	n := normalizeModelName(name)
	for _, p := range prefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func hasAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
