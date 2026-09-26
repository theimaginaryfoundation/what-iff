package provider

// Gemini OpenAI-compat tool-call helpers.
//
// Google's Chat Completions shim is stricter than native OpenAI on multi-turn tools:
//   - assistant tool-call turns need non-empty content
//   - tool results need a "name" field matching the function
//   - function calls must echo extra_content.google.thought_signature verbatim
//
// These helpers normalize IDs, preserve raw tool-call JSON (thought signatures),
// and shape outbound messages so the agent loop can round-trip tools reliably.
import (
	"fmt"
	"strings"
	"unicode"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// geminiToolCallContentPlaceholder is the internal content we put on an outbound
// assistant tool-call turn whose content was empty (Gemini rejects empty content
// there). It is provider-request plumbing only and must never become user-visible
// assistant text — but the model sees it as its own prior output and can imitate
// it, echoing it back as response text. geminiToolCallEchoFilter and
// stripGeminiToolCallEcho remove such echoes; both key off this single constant.
const geminiToolCallContentPlaceholder = "[tool call]"

// geminiToolCallID returns a stable tool-call id for Gemini's OpenAI-compatible API.
// Gemini often omits ids; the compat layer requires tool_call_id to match on follow-up turns.
func geminiToolCallID(name, rawID string) string {
	if id := strings.TrimSpace(rawID); id != "" {
		return id
	}
	return strings.TrimSpace(name)
}

// normalizeGeminiToolUses assigns stable ids to tool uses before local execution.
func normalizeGeminiToolUses(uses []ToolUse) []ToolUse {
	if len(uses) == 0 {
		return uses
	}
	out := make([]ToolUse, len(uses))
	seenNames := make(map[string]int)
	for i, u := range uses {
		id := geminiToolCallID(u.Name, u.ID)
		if strings.TrimSpace(u.ID) == "" {
			if n := seenNames[u.Name]; n > 0 {
				id = fmt.Sprintf("%s_%d", id, n+1)
			}
			seenNames[u.Name]++
		}
		out[i] = ToolUse{
			ID:    id,
			Name:  u.Name,
			Input: u.Input,
		}
	}
	return out
}

// geminiAssistantToolCallMessage builds the assistant turn Gemini expects after tool_calls:
// non-empty content, stable tool_call ids, and preserved extra_content (thought_signature).
// thoughtSignatures (keyed by tool-call index) supplies the
// extra_content.google.thought_signature to re-attach when the source tool call
// carries no raw JSON of its own — the streamed case, where the accumulator
// dropped it. Pass nil when unavailable (non-streaming turns keep it in raw JSON).
func geminiAssistantToolCallMessage(msg openai.ChatCompletionMessage, thoughtSignatures map[int64]string) openai.ChatCompletionMessageParamUnion {
	p := msg.ToAssistantMessageParam()
	if len(p.ToolCalls) > 0 && assistantContentEmpty(p.Content) {
		p.Content.OfString = openai.String(geminiToolCallContentPlaceholder)
	}
	if len(msg.ToolCalls) > 0 {
		p.ToolCalls = make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			p.ToolCalls = append(p.ToolCalls, geminiToolCallToParam(tc, thoughtSignatures[int64(i)]))
		}
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &p}
}

// geminiToolCallToParam converts a response tool call to request params while preserving
// Gemini-specific fields such as extra_content.google.thought_signature.
func geminiToolCallToParam(tc openai.ChatCompletionMessageToolCallUnion, thoughtSignature string) openai.ChatCompletionMessageToolCallUnionParam {
	raw := tc.RawJSON()
	if raw == "" {
		// RawJSON is empty for tool calls assembled by ChatCompletionAccumulator
		// during streaming — the accumulator rebuilds them from deltas and does not
		// preserve the raw bytes. The SDK's own ToParam() sets the marshal override
		// to that empty string, which then fails on the next request with
		// "unexpected end of JSON input" (jsontext.Value marshalling empty bytes).
		// Gemini streams every chat turn, so this is the path that image-gen (and any
		// other tool call) reliably takes. Reconstruct the param from the decoded
		// fields instead, re-attaching the thought signature the accumulator dropped
		// (Google rejects a follow-up whose function call omits it).
		return geminiToolCallParamFromFields(tc, thoughtSignature)
	}
	name := gjson.Get(raw, "function.name").String()
	stableID := geminiToolCallID(name, gjson.Get(raw, "id").String())
	if stableID != gjson.Get(raw, "id").String() {
		patched, err := sjson.Set(raw, "id", stableID)
		if err == nil {
			raw = patched
		}
	}
	if !gjson.Valid(raw) || strings.TrimSpace(gjson.Get(raw, "function.name").String()) == "" {
		return tc.ToParam()
	}
	var out openai.ChatCompletionMessageToolCallUnionParam
	param.SetJSON([]byte(raw), &out)
	return out
}

// geminiToolCallParamFromFields reconstructs a request tool-call param directly
// from a response union's decoded fields, giving the union a concrete variant
// (OfFunction/OfCustom) so it marshals from that variant rather than from an empty
// raw-JSON override. Function arguments default to "{}" when empty because Gemini's
// compat layer rejects assistant tool calls with no arguments payload. A non-empty
// thoughtSignature is re-attached as extra_content.google.thought_signature, matching
// the shape Google emits and requires echoed back (function calls only).
func geminiToolCallParamFromFields(tc openai.ChatCompletionMessageToolCallUnion, thoughtSignature string) openai.ChatCompletionMessageToolCallUnionParam {
	if tc.Type == "custom" {
		return openai.ChatCompletionMessageToolCallUnionParam{
			OfCustom: &openai.ChatCompletionMessageCustomToolCallParam{
				ID: geminiToolCallID(tc.Custom.Name, tc.ID),
				Custom: openai.ChatCompletionMessageCustomToolCallCustomParam{
					Name:  tc.Custom.Name,
					Input: tc.Custom.Input,
				},
			},
		}
	}
	args := tc.Function.Arguments
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	fn := &openai.ChatCompletionMessageFunctionToolCallParam{
		ID: geminiToolCallID(tc.Function.Name, tc.ID),
		Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
			Name:      tc.Function.Name,
			Arguments: args,
		},
	}
	if strings.TrimSpace(thoughtSignature) != "" {
		// fn is freshly constructed above, so it owns all extra fields here —
		// nothing else has set any to merge with.
		fn.SetExtraFields(map[string]any{
			"extra_content": map[string]any{
				"google": map[string]any{"thought_signature": thoughtSignature},
			},
		})
	}
	return openai.ChatCompletionMessageToolCallUnionParam{OfFunction: fn}
}

func geminiToolCallHasThoughtSignature(tc openai.ChatCompletionMessageToolCallUnion) bool {
	return strings.Contains(tc.RawJSON(), "thought_signature")
}

// geminiToolResultMessage builds a tool-role message for Gemini's OpenAI-compatible API.
// Unlike native OpenAI, Gemini requires a non-empty "name" matching the function that ran.
func geminiToolResultMessage(result ToolResult, toolName string) openai.ChatCompletionMessageParamUnion {
	tool := openai.ChatCompletionToolMessageParam{
		ToolCallID: result.ID,
		Content: openai.ChatCompletionToolMessageParamContentUnion{
			OfString: openai.String(toolResultOutput(result)),
		},
	}
	if name := strings.TrimSpace(toolName); name != "" {
		tool.SetExtraFields(map[string]any{"name": name})
	}
	return openai.ChatCompletionMessageParamUnion{OfTool: &tool}
}

func assistantContentEmpty(content openai.ChatCompletionAssistantMessageParamContentUnion) bool {
	if !param.IsOmitted(content.OfString) && strings.TrimSpace(content.OfString.Value) != "" {
		return false
	}
	return len(content.OfArrayOfContentParts) == 0
}

// geminiMessagesCarryToolCallPlaceholder reports whether any assistant turn in the
// outbound messages contains geminiToolCallContentPlaceholder — i.e. whether the
// model has been shown the placeholder as its own output and may echo it. Only
// assistant turns count: a user who types the same string gives the model nothing
// to imitate as assistant output, so their text never arms the echo filter.
func geminiMessagesCarryToolCallPlaceholder(messages []openai.ChatCompletionMessageParamUnion) bool {
	for _, m := range messages {
		if m.OfAssistant == nil {
			continue
		}
		c := m.OfAssistant.Content
		if !param.IsOmitted(c.OfString) && strings.Contains(c.OfString.Value, geminiToolCallContentPlaceholder) {
			return true
		}
		for _, part := range c.OfArrayOfContentParts {
			if part.OfText != nil && strings.Contains(part.OfText.Text, geminiToolCallContentPlaceholder) {
				return true
			}
		}
	}
	return false
}

// splitLeadingGeminiToolCallEcho strips any run of leading placeholder echoes
// (optionally whitespace-separated) from s. undecided is true when what remains
// after the stripped echoes is empty/whitespace or a proper prefix of the
// placeholder, so a streaming caller must wait for more text before deciding.
// Only leading echoes are removed: a placeholder later in the text is the model
// discussing or quoting it, not imitating the empty tool-call turn.
func splitLeadingGeminiToolCallEcho(s string) (rest string, undecided bool) {
	for {
		body := strings.TrimLeftFunc(s, unicode.IsSpace)
		if body == "" || (len(body) < len(geminiToolCallContentPlaceholder) && strings.HasPrefix(geminiToolCallContentPlaceholder, body)) {
			return s, true
		}
		if !strings.HasPrefix(body, geminiToolCallContentPlaceholder) {
			return s, false
		}
		s = body[len(geminiToolCallContentPlaceholder):]
	}
}

// stripGeminiToolCallEcho removes leading placeholder echoes from a complete
// response text (the non-streamed / final GenerateResponse.Text path).
func stripGeminiToolCallEcho(text string) string {
	rest, _ := splitLeadingGeminiToolCallEcho(text)
	return strings.TrimSpace(rest)
}

// geminiToolCallEchoFilter wraps a text-delta handler for one streamed response,
// dropping placeholder echoes at the start of that response before they reach the
// draft stream (and, via the concatenated deltas, the persisted assistant message).
// It buffers only while the leading text could still be an echo, then passes every
// later delta straight through. Call Flush when the stream ends.
type geminiToolCallEchoFilter struct {
	next    func(delta string)
	leading bool
	pending string
}

func newGeminiToolCallEchoFilter(next func(delta string)) *geminiToolCallEchoFilter {
	return &geminiToolCallEchoFilter{next: next, leading: true}
}

// HandleDelta implements the text-delta handler signature.
func (f *geminiToolCallEchoFilter) HandleDelta(delta string) {
	if !f.leading {
		f.next(delta)
		return
	}
	rest, undecided := splitLeadingGeminiToolCallEcho(f.pending + delta)
	if undecided {
		f.pending = rest
		return
	}
	f.leading = false
	f.pending = ""
	f.next(rest)
}

// Flush forwards any held-back text that turned out not to be an echo (e.g. a
// response that is literally "[tool" or whitespace).
func (f *geminiToolCallEchoFilter) Flush() {
	if f.pending != "" {
		f.next(f.pending)
		f.pending = ""
	}
	f.leading = false
}
