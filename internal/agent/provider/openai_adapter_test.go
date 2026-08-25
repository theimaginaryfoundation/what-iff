package provider

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

func TestExtractOpenAIToolUses_NilResponse(t *testing.T) {
	t.Parallel()
	require.Empty(t, extractOpenAIToolUses(nil))
}

func TestExtractOpenAIToolUses_FromJSON(t *testing.T) {
	t.Parallel()
	raw := `{
		"id": "resp_fc",
		"usage": {
			"input_tokens": 1,
			"output_tokens": 2,
			"total_tokens": 3
		},
		"output": [
			{
				"type": "function_call",
				"call_id": "call_xyz",
				"name": "do_thing",
				"arguments": "{\"a\":1}"
			}
		]
	}`
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))

	uses := extractOpenAIToolUses(&resp)
	require.Len(t, uses, 1)
	require.Equal(t, "call_xyz", uses[0].ID)
	require.Equal(t, "do_thing", uses[0].Name)
	require.JSONEq(t, `{"a":1}`, string(uses[0].Input))
}

func TestExtractOpenAIToolUses_IgnoresNonFunctionOutput(t *testing.T) {
	t.Parallel()
	raw := `{
		"id": "r2",
		"usage": {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0},
		"output": [
			{
				"type": "message",
				"id": "m1",
				"role": "assistant",
				"status": "completed",
				"content": [{"type": "output_text", "text": "hello"}]
			}
		]
	}`
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	require.Empty(t, extractOpenAIToolUses(&resp))
}

func TestExtractOpenAIToolUses_MultipleFunctionCalls(t *testing.T) {
	t.Parallel()
	raw := `{
		"id": "r3",
		"usage": {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0},
		"output": [
			{"type": "function_call", "call_id": "c1", "name": "t1", "arguments": "{}"},
			{"type": "function_call", "call_id": "c2", "name": "t2", "arguments": "\"raw\""}
		]
	}`
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	uses := extractOpenAIToolUses(&resp)
	require.Len(t, uses, 2)
	require.Equal(t, "c1", uses[0].ID)
	require.Equal(t, "c2", uses[1].ID)
}

func TestOpenAIAdapter_AppendToolResults_PreservesInputAndAppendsOutputs(t *testing.T) {
	t.Parallel()
	base := responses.ResponseInputParam{
		responses.ResponseInputItemParamOfMessage("hello", responses.EasyInputMessageRoleUser),
	}
	p := responses.ResponseNewParams{
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: base},
	}
	a := NewOpenAIAdapter(nil, p)
	a.AppendToolResults([]ToolResult{
		{ID: "call_1", Output: "first out"},
		{ID: "call_2", Output: "", IsErr: true},
	})

	list := a.params.Input.OfInputItemList
	require.Len(t, list, 3)

	require.NotNil(t, list[0].OfMessage)
	require.Equal(t, "hello", list[0].OfMessage.Content.OfString.Value)

	require.NotNil(t, list[1].OfFunctionCallOutput)
	require.Equal(t, "call_1", list[1].OfFunctionCallOutput.CallID)
	require.True(t, list[1].OfFunctionCallOutput.Output.OfString.Valid())
	require.Equal(t, "first out", list[1].OfFunctionCallOutput.Output.OfString.Value)

	require.NotNil(t, list[2].OfFunctionCallOutput)
	require.Equal(t, "call_2", list[2].OfFunctionCallOutput.CallID)
	require.True(t, list[2].OfFunctionCallOutput.Output.OfString.Valid())
	require.Equal(t, "unknown error occurred", list[2].OfFunctionCallOutput.Output.OfString.Value)
}

func TestOpenAIAdapter_AppendToolResults_ContinuationDropsInitialInput(t *testing.T) {
	t.Parallel()
	base := responses.ResponseInputParam{
		responses.ResponseInputItemParamOfMessage("initial model context", responses.EasyInputMessageRoleUser),
	}
	a := NewOpenAIAdapter(nil, responses.ResponseNewParams{
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: base},
	})
	a.previousResponseID = "resp_previous"

	a.AppendToolResults([]ToolResult{{ID: "call_1", Output: "new tool output"}})

	list := a.params.Input.OfInputItemList
	require.Len(t, list, 1)
	require.NotNil(t, list[0].OfFunctionCallOutput)
	require.Equal(t, "call_1", list[0].OfFunctionCallOutput.CallID)
	require.Equal(t, "new tool output", list[0].OfFunctionCallOutput.Output.OfString.Value)
}

func TestOpenAIAdapter_AppendToolResults_EmptyStartingInput(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{}
	a := NewOpenAIAdapter(nil, p)
	a.AppendToolResults([]ToolResult{{ID: "only", Output: "x"}})
	list := a.params.Input.OfInputItemList
	require.Len(t, list, 1)
	require.NotNil(t, list[0].OfFunctionCallOutput)
	require.Equal(t, "only", list[0].OfFunctionCallOutput.CallID)
}

func TestOpenAIAdapter_AppendToolResults_InjectsGeneratedImagesOnUserTurn(t *testing.T) {
	t.Parallel()
	a := NewOpenAIAdapter(nil, responses.ResponseNewParams{})
	a.AppendToolResults([]ToolResult{
		{
			ID:     "call_img",
			Output: `{"success":true}`,
			Images: []UserMessageImage{
				{RawBytes: []byte{0x89, 0x50, 0x4e, 0x47}, MediaType: "image/png"},
			},
		},
	})
	list := a.params.Input.OfInputItemList
	require.Len(t, list, 2)
	require.NotNil(t, list[0].OfFunctionCallOutput)
	require.Equal(t, "user", string(list[1].OfMessage.Role))
	raw, err := json.Marshal(list[1])
	require.NoError(t, err)
	require.Contains(t, string(raw), GeneratedToolImageCaption)
}
