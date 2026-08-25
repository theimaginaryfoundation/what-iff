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

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

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
func geminiAssistantToolCallMessage(msg openai.ChatCompletionMessage) openai.ChatCompletionMessageParamUnion {
	p := msg.ToAssistantMessageParam()
	if len(p.ToolCalls) > 0 && assistantContentEmpty(p.Content) {
		p.Content.OfString = openai.String(geminiToolCallContentPlaceholder)
	}
	if len(msg.ToolCalls) > 0 {
		p.ToolCalls = make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			p.ToolCalls = append(p.ToolCalls, geminiToolCallToParam(tc))
		}
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &p}
}

// geminiToolCallToParam converts a response tool call to request params while preserving
// Gemini-specific fields such as extra_content.google.thought_signature.
func geminiToolCallToParam(tc openai.ChatCompletionMessageToolCallUnion) openai.ChatCompletionMessageToolCallUnionParam {
	raw := tc.RawJSON()
	if raw == "" {
		return tc.ToParam()
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
