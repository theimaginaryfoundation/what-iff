package provider

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

func requireSameJSON(t *testing.T, want, got any) {
	t.Helper()
	w, err := json.Marshal(want)
	require.NoError(t, err)
	g, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, string(w), string(g))
}

var testUtilitySchema = &UtilitySchema{
	Name:        "Thing",
	Description: "Thing JSON",
	Schema:      map[string]any{"type": "object"},
}

// The Responses rendering of each request shape must match what its call site built
// by hand before utility calls existed, so moving a call site changes nothing on the wire.
func TestUtilityResponsesParams_MatchesHandBuiltRequests(t *testing.T) {
	t.Parallel()
	schemaText := responses.ResponseTextConfigParam{
		Format: responses.ResponseFormatTextConfigUnionParam{
			OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
				Name:        "Thing",
				Schema:      map[string]any{"type": "object"},
				Strict:      openai.Bool(true),
				Description: openai.String("Thing JSON"),
				Type:        "json_schema",
			},
		},
	}
	tests := []struct {
		name string
		req  UtilityRequest
		want responses.ResponseNewParams
	}{
		{
			name: "plain text",
			req:  UtilityRequest{Instructions: "name it", Input: "hello", MaxOutputTokens: 100},
			want: responses.ResponseNewParams{
				Model:           "m",
				MaxOutputTokens: openai.Int(100),
				Instructions:    openai.String("name it"),
				Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String("hello")},
			},
		},
		{
			name: "structured, priority, safety id",
			req: UtilityRequest{
				Instructions: "build it", Input: "answers", MaxOutputTokens: 100,
				SafetyUserID: "u1", ServiceTier: UtilityServiceTierPriority, Schema: testUtilitySchema,
			},
			want: responses.ResponseNewParams{
				Model:            "m",
				SafetyIdentifier: openai.String("u1"),
				MaxOutputTokens:  openai.Int(100),
				Instructions:     openai.String("build it"),
				ServiceTier:      responses.ResponseNewParamsServiceTierPriority,
				Input:            responses.ResponseNewParamsInputUnion{OfString: openai.String("answers")},
				Text:             schemaText,
			},
		},
		{
			name: "history and input, flex",
			req: UtilityRequest{
				Instructions: "pick", ServiceTier: UtilityServiceTierFlex, Schema: testUtilitySchema,
				History: []UtilityMessage{{Role: UtilityMessageDeveloper, Text: "note"}},
				Input:   "pick one",
			},
			want: responses.ResponseNewParams{
				Model:        "m",
				Instructions: openai.String("pick"),
				ServiceTier:  responses.ResponseNewParamsServiceTierFlex,
				Input: responses.ResponseNewParamsInputUnion{OfInputItemList: []responses.ResponseInputItemUnionParam{
					responses.ResponseInputItemParamOfMessage("note", RoleDeveloper),
					responses.ResponseInputItemParamOfMessage("pick one", RoleUser),
				}},
				Text: schemaText,
			},
		},
		{
			name: "image",
			req: UtilityRequest{
				Instructions:      "describe",
				Input:             "this",
				InputImageDataURL: "data:image/png;base64,AAAA",
			},
			want: responses.ResponseNewParams{
				Model:        "m",
				Instructions: openai.String("describe"),
				Input: responses.ResponseNewParamsInputUnion{OfInputItemList: []responses.ResponseInputItemUnionParam{
					responses.ResponseInputItemParamOfMessage(responses.ResponseInputMessageContentListParam{
						responses.ResponseInputContentParamOfInputText("this"),
						{OfInputImage: &responses.ResponseInputImageParam{
							ImageURL: openai.String("data:image/png;base64,AAAA"),
							Detail:   responses.ResponseInputImageDetailAuto,
						}},
					}, responses.EasyInputMessageRoleUser),
				}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireSameJSON(t, tt.want, UtilityResponsesParams("m", tt.req))
		})
	}
}

func TestUtilityResponsesParams_ContinuesConversation(t *testing.T) {
	t.Parallel()
	mc := &ModelContext{}
	mc.Append(SegmentKindUserMessage, RoleUser, "earlier turn", false)
	req := UtilityRequest{Context: mc, Input: "go on", SafetyUserID: "u1", MaxOutputTokens: 384, Schema: testUtilitySchema}

	got := UtilityResponsesParams("m", req)
	want := SanitizeImagesForOpenAIInput(mc).BuildOpenAIResponseParams(OpenAIResponseParamsOptions{Model: "m", SafetyUserID: "u1", MaxOutputTokens: 384})
	want.Input.OfInputItemList = append(want.Input.OfInputItemList, responses.ResponseInputItemParamOfMessage("go on", RoleUser))
	want.Text = UtilityResponsesParams("m", UtilityRequest{Schema: testUtilitySchema}).Text
	requireSameJSON(t, want, got)
}

func TestUtilityClaudeParams(t *testing.T) {
	t.Parallel()
	got := UtilityClaudeParams("claude-x", UtilityRequest{
		Instructions:      "extract",
		History:           []UtilityMessage{{Role: UtilityMessageDeveloper, Text: "skip duplicates"}},
		Input:             "look",
		InputImageDataURL: "data:image/jpeg;base64,AAAA",
		Schema:            testUtilitySchema,
	})
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))

	require.Equal(t, "claude-x", body["model"])
	require.EqualValues(t, DefaultMaxContentLength, body["max_tokens"], "the Messages API requires a cap")
	system := body["system"].([]any)
	require.Len(t, system, 2, "developer turns follow the instructions as system blocks")
	require.Equal(t, "extract", system[0].(map[string]any)["text"])
	require.Equal(t, "skip duplicates", system[1].(map[string]any)["text"])
	msgs := body["messages"].([]any)
	require.Len(t, msgs, 1)
	content := msgs[0].(map[string]any)["content"].([]any)
	require.Equal(t, "text", content[0].(map[string]any)["type"])
	img := content[1].(map[string]any)
	require.Equal(t, "image", img["type"])
	require.Equal(t, "image/jpeg", img["source"].(map[string]any)["media_type"])
	format := body["output_config"].(map[string]any)["format"].(map[string]any)
	require.Equal(t, "json_schema", format["type"])
	require.Equal(t, map[string]any{"type": "object"}, format["schema"])
}

func TestUtilityChatCompletionParams(t *testing.T) {
	t.Parallel()
	got := UtilityChatCompletionParams("mistral-x", UtilityRequest{
		Instructions:      "extract",
		MaxOutputTokens:   200,
		History:           []UtilityMessage{{Role: UtilityMessageDeveloper, Text: "skip duplicates"}},
		Input:             "look",
		InputImageDataURL: "data:image/png;base64,AAAA",
		Schema:            testUtilitySchema,
	})
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))

	require.EqualValues(t, 200, body["max_completion_tokens"])
	msgs := body["messages"].([]any)
	require.Len(t, msgs, 2)
	require.Equal(t, "system", msgs[0].(map[string]any)["role"])
	require.Equal(t, "extract\n\nskip duplicates", msgs[0].(map[string]any)["content"],
		"one leading system message: some providers take a system message only first")
	require.Equal(t, "user", msgs[1].(map[string]any)["role"])
	parts := msgs[1].(map[string]any)["content"].([]any)
	require.Equal(t, "image_url", parts[1].(map[string]any)["type"])
	format := body["response_format"].(map[string]any)
	require.Equal(t, "json_schema", format["type"])
	require.Equal(t, "Thing", format["json_schema"].(map[string]any)["name"])
}

// Models increasingly reject sampling settings (gpt-5.6-luna 400s on any temperature), so a
// utility request has none to send, on any API.
func TestUtilityParams_NoSampling(t *testing.T) {
	t.Parallel()
	req := UtilityRequest{Instructions: "i", Input: "x", Schema: testUtilitySchema}
	for name, params := range map[string]any{
		"responses": UtilityResponsesParams("m", req),
		"claude":    UtilityClaudeParams("m", req),
		"chat":      UtilityChatCompletionParams("m", req),
	} {
		raw, err := json.Marshal(params)
		require.NoError(t, err, name)
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body), name)
		require.NotContains(t, body, "temperature", name)
		require.NotContains(t, body, "top_p", name)
	}
}

func TestSplitImageDataURL(t *testing.T) {
	t.Parallel()
	mt, data, ok := splitImageDataURL("data:image/webp;base64,QUJD")
	require.True(t, ok)
	require.Equal(t, "image/webp", mt)
	require.Equal(t, "QUJD", data)
	for _, bad := range []string{"", "https://x/y.png", "data:image/png,QUJD", "data:;base64,QUJD"} {
		_, _, ok := splitImageDataURL(bad)
		require.False(t, ok, bad)
	}
}

// On the Messages API the instructions come first, then the conversation's own system blocks
// (cache markers and all), then the call's developer turns, in order.
func TestUtilityClaudeParams_SystemBlockOrderWithConversation(t *testing.T) {
	t.Parallel()
	mc := &ModelContext{}
	mc.Append(SegmentKindSystemPrompt, RoleDeveloper, "You are a helpful assistant.", true)
	mc.Append(SegmentKindHistoryTurn, RoleUser, "hi", false)
	got := UtilityClaudeParams("claude-x", UtilityRequest{
		Instructions: "summarize",
		Context:      mc,
		History:      []UtilityMessage{{Role: UtilityMessageDeveloper, Text: "be brief"}},
		Input:        "go",
	})
	var texts []string
	for _, b := range got.System {
		texts = append(texts, b.Text)
	}
	require.GreaterOrEqual(t, len(texts), 3)
	require.Equal(t, "summarize", texts[0])
	require.Contains(t, texts[1:len(texts)-1], "You are a helpful assistant.")
	require.Equal(t, "be brief", texts[len(texts)-1])
	require.Equal(t, mc.BuildClaudeParams("claude-x").System[0].CacheControl, got.System[1].CacheControl, "the conversation's cache marker is kept")
}

// An image-only ask sends just the image on every API: Anthropic rejects an empty text block,
// and the others gain nothing from one.
func TestUtilityParams_ImageAloneSendsNoEmptyText(t *testing.T) {
	t.Parallel()
	req := UtilityRequest{InputImageDataURL: "data:image/png;base64,AAAA"}

	claude := UtilityClaudeParams("claude-x", req)
	require.Len(t, claude.Messages, 1)
	require.Len(t, claude.Messages[0].Content, 1)
	require.NotNil(t, claude.Messages[0].Content[0].OfImage, "the image, and no text block")

	chat, err := json.Marshal(UtilityChatCompletionParams("mistral-x", req))
	require.NoError(t, err)
	require.NotContains(t, string(chat), `"type":"text"`)
	require.Contains(t, string(chat), `"type":"image_url"`)

	resp, err := json.Marshal(UtilityResponsesParams("m", req))
	require.NoError(t, err)
	require.NotContains(t, string(resp), `"input_text"`)
	require.Contains(t, string(resp), `"input_image"`)
}

func TestUtilityResponsesParams_ConversationKeepsServiceTier(t *testing.T) {
	t.Parallel()
	got := UtilityResponsesParams("m", UtilityRequest{Context: &ModelContext{}, ServiceTier: UtilityServiceTierFlex})
	require.Equal(t, responses.ResponseNewParamsServiceTierFlex, got.ServiceTier)
}

func TestOpenAIRejectedImages(t *testing.T) {
	t.Parallel()
	mc := &ModelContext{}
	mc.AppendHistoryTurn(RoleUser, "a", []UserMessageImage{
		{MediaType: "image/png", RawBytes: []byte{1}},
		{MediaType: "image/heic", RawBytes: []byte{2}},
		{MediaType: "image/jpeg"},
	}, false)
	noData, unsupported := OpenAIRejectedImages(mc)
	require.Equal(t, 1, noData)
	require.Equal(t, 1, unsupported)
	noData, unsupported = OpenAIRejectedImages(nil)
	require.Zero(t, noData+unsupported)
}

// The counts reported for a conversation are exactly the images the sanitizer leaves out of it,
// so the metric can't drift from what was actually sent.
func TestOpenAIRejectedImages_MatchesSanitizer(t *testing.T) {
	t.Parallel()
	mc := &ModelContext{}
	mc.AppendHistoryTurn(RoleUser, "a", []UserMessageImage{
		{MediaType: "image/png", RawBytes: []byte{1}},
		{MediaType: "image/heic", RawBytes: []byte{2}},
		{MediaType: "image/jpeg"},
	}, false)
	mc.AppendHistoryTurn(RoleUser, "", []UserMessageImage{{MediaType: "image/tiff", RawBytes: []byte{3}}}, false)
	mc.AppendHistoryTurn(RoleUser, "b", []UserMessageImage{{MediaType: "image/webp", RawBytes: []byte{4}}}, false)

	count := func(m *ModelContext) int {
		n := 0
		for _, seg := range m.Segments {
			n += len(seg.UserImages)
		}
		return n
	}
	noData, unsupported := OpenAIRejectedImages(mc)
	require.Equal(t, count(mc)-count(SanitizeImagesForOpenAIInput(mc)), noData+unsupported)
	require.Equal(t, 5, count(mc), "the sanitizer works on a clone")
}
