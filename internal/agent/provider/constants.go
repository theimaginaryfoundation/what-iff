package provider

const (
	DefaultTemperature         = 1
	ScratchpadTemperature      = 1
	DefaultMaxContentLength    = 8192
	ScratchpadMaxContentLength = 2048
	RoleDeveloper              = "developer"
	RoleUser                   = "user"
	RoleAssistant              = "assistant"

	shortMessageThreshold = 100
)

// z.ai GLM thinking budget.
//
// GLM models think by default and the thinking CANNOT be disabled via the API
// (confirmed against z.ai docs). Left unbounded, reasoning spends the entire
// DefaultMaxContentLength output cap and the turn truncates
// (stop_reason=max_tokens) before emitting any answer text. On the zai path we
// therefore set an explicit thinking budget and raise the output cap so reasoning
// and answer both fit: a 4096-token reasoning budget on top of the usual 8192
// answer budget every other vendor gets, for 12288 total.
const (
	ZAIThinkingBudgetTokens = 4096
	ZAIMaxOutputTokens      = DefaultMaxContentLength + ZAIThinkingBudgetTokens
)

// TextOnlyChatCompletionsImageFallback replaces image-only user turns when rendering for
// Chat Completions providers that reject vision input (DeepSeek; pre-2.6 MiMo and non-vision Qwen/Mistral ids).
const TextOnlyChatCompletionsImageFallback = "[The user attached one or more images. This model does not support vision — ask them to describe the image or switch to a vision-capable model if visual analysis is required.]"

// ExpressionPortraitContinuityPointerNote is appended to developer continuity text when a
// portrait thumbnail was sent in the immediately preceding user message.
const ExpressionPortraitContinuityPointerNote = `
(The expression portrait image appears in the immediately preceding user message.
Treat as background context; acknowledge only when meaningful.)`

// ExpressionPortraitVisualReferenceCaption precedes portrait pixels in the dedicated portrait user turn.
const ExpressionPortraitVisualReferenceCaption = `Expression portrait for the prior assistant turn (visual reference).
This is primarily continuity context for you — no need to comment on it unless it's genuinely relevant.`

// GeneratedToolImageCaption precedes image blocks injected after a tool result in the
// in-turn agent loop (e.g. generate_image). Same Anthropic user-turn rule as
// HistoryAssistantImageCaption for prior assistant turns.
const GeneratedToolImageCaption = "Images produced by the preceding tool call:"

// HistoryAssistantImageCaption precedes image blocks when a prior assistant turn included
// attachments (e.g. generate_image). Required because Anthropic forbids image blocks on
// assistant turns and OpenAI Responses forbids input_text on assistant turns.
const HistoryAssistantImageCaption = "Images from the assistant message above:"
