package agent

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

// --- webSearchToolCallsFromOpenAIResponses ---

func TestWebSearchToolCallsFromOpenAIResponses_NilResponsesAreSkipped(t *testing.T) {
	t.Parallel()
	got := webSearchToolCallsFromOpenAIResponses(nil, nil)
	require.Nil(t, got)
}

func TestWebSearchToolCallsFromOpenAIResponses_IncompleteCallIsSkipped(t *testing.T) {
	t.Parallel()
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(`{"output":[
		{"type":"web_search_call","id":"ws_1","status":"in_progress","action":{"type":"search","query":"weather NYC"}}
	]}`), &resp))

	got := webSearchToolCallsFromOpenAIResponses(&resp)
	require.Nil(t, got)
}

func TestWebSearchToolCallsFromOpenAIResponses_CompletedCallExtractsToolCall(t *testing.T) {
	t.Parallel()
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(`{"output":[
		{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"weather NYC"}},
		{"type":"message","role":"assistant","status":"completed","content":[
			{"type":"output_text","text":"It is sunny.","annotations":[
				{"type":"url_citation","url":"https://example.com/a","title":"Example A","start_index":0,"end_index":1}
			]}
		]}
	]}`), &resp))

	got := webSearchToolCallsFromOpenAIResponses(&resp)
	require.Len(t, got, 1)
	require.Equal(t, tools.ToolNameWebSearch, got[0].ToolName)
	require.Equal(t, "weather NYC", got[0].ToolInput)
	require.Contains(t, got[0].ToolOutput, "Citations:")
	require.Contains(t, got[0].ToolOutput, "https://example.com/a")
}

// --- webSearchInputFromCall ---

func TestWebSearchInputFromCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "payload queries win",
			raw:  `{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","queries":["a","b"]}}`,
			want: "a; b",
		},
		{
			name: "payload single query",
			raw:  `{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"solo"}}`,
			want: "solo",
		},
		{
			name: "no query anywhere returns empty",
			raw:  `{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"open_page","url":"https://example.com"}}`,
			want: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ws responses.ResponseFunctionWebSearch
			require.NoError(t, ws.UnmarshalJSON([]byte(tc.raw)))
			require.Equal(t, tc.want, webSearchInputFromCall(ws))
		})
	}
}

// --- extractOpenAIURLCitations ---

func TestExtractOpenAIURLCitations_NilResponseReturnsNil(t *testing.T) {
	t.Parallel()
	require.Nil(t, extractOpenAIURLCitations(nil))
}

func TestExtractOpenAIURLCitations_NoMessageOutputReturnsNil(t *testing.T) {
	t.Parallel()
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(`{"output":[{"type":"reasoning"}]}`), &resp))
	require.Nil(t, extractOpenAIURLCitations(&resp))
}

func TestExtractOpenAIURLCitations_DedupesRepeatedCitations(t *testing.T) {
	t.Parallel()
	var resp responses.Response
	require.NoError(t, json.Unmarshal([]byte(`{"output":[
		{"type":"message","role":"assistant","status":"completed","content":[
			{"type":"output_text","text":"a","annotations":[
				{"type":"url_citation","url":"https://example.com/a","title":"Example A","start_index":0,"end_index":1},
				{"type":"url_citation","url":"https://example.com/a","title":"Example A","start_index":2,"end_index":3}
			]}
		]}
	]}`), &resp))

	got := extractOpenAIURLCitations(&resp)
	require.Len(t, got, 1)
	require.Equal(t, "https://example.com/a", got[0].URL)
}
