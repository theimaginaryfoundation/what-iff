package provider

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// OpenAIAdapter implements AgentAdapter for the OpenAI Responses API.
// It is stateful: previousResponseID is updated after each Call so that
// within-turn tool-call continuations are threaded correctly by the server.
// lastResponse holds the most recent raw API response, exposed via
// LastRawResponse() for callers that need it (e.g. attachment saving).
type OpenAIAdapter struct {
	provider           *OpenAIProvider
	params             responses.ResponseNewParams
	previousResponseID string
	lastResponse       *responses.Response
	rawResponses       []*responses.Response
	webSearchCompleted int
	textDeltaHandler   func(delta string)
}

// NewOpenAIAdapter constructs an OpenAIAdapter from pre-built ResponseNewParams.
// The caller is responsible for populating params (instructions, input, tools, etc.)
// before constructing the adapter.
func NewOpenAIAdapter(provider *OpenAIProvider, params responses.ResponseNewParams) *OpenAIAdapter {
	return &OpenAIAdapter{
		provider: provider,
		params:   params,
	}
}

// Call makes one Responses API request. It returns a final GenerateResponse when
// the model produces a text answer, or a non-empty []ToolUse when tools are
// requested (GenerateResponse is nil in that case).
func (a *OpenAIAdapter) Call(ctx context.Context) (*GenerateResponse, []ToolUse, error) {
	if a.previousResponseID != "" {
		a.params.PreviousResponseID = openai.String(a.previousResponseID)
	}
	var (
		resp *responses.Response
		err  error
	)
	if a.textDeltaHandler != nil {
		resp, err = a.provider.CallWithRetryStreaming(ctx, a.params, a.textDeltaHandler)
	} else {
		resp, err = a.provider.CallWithRetry(ctx, a.params)
	}
	if err != nil {
		return nil, nil, WrapSafetyViolationError(models.SafetyViolationProviderOpenAI, fmt.Errorf("OpenAI API call failed: %w", err))
	}

	a.previousResponseID = resp.ID
	a.lastResponse = resp
	a.rawResponses = append(a.rawResponses, resp)
	a.webSearchCompleted += countCompletedWebSearchesOpenAI(resp)

	toolUses := extractOpenAIToolUses(resp)
	if len(toolUses) == 0 {
		return OpenAIToGenerateResponse(resp), nil, nil
	}

	return nil, toolUses, nil
}

func (a *OpenAIAdapter) SetTextDeltaHandler(handler func(delta string)) {
	a.textDeltaHandler = handler
}

// LastRawResponse returns the most recent raw Responses API response.
// Callers use this to access provider-specific metadata (e.g. attachment IDs)
// that is not captured in the provider-agnostic GenerateResponse.
// This method is intentionally outside AgentAdapter — it has no Claude equivalent
// and must be called on the concrete *OpenAIAdapter type at the call site.
func (a *OpenAIAdapter) LastRawResponse() *responses.Response {
	return a.lastResponse
}

// WebSearchCompletedCount returns how many completed native web_search_call items
// were observed across all adapter Call and ForceFinalResponse invocations this turn.
func (a *OpenAIAdapter) WebSearchCompletedCount() int {
	return a.webSearchCompleted
}

// CurrentParams returns a copy of the adapter's current request params.
// Test-only helper — not part of AgentAdapter. Call on the concrete type.
func (a *OpenAIAdapter) CurrentParams() responses.ResponseNewParams {
	return a.params
}

// AppendToolResults formats results as function-call-output items for the next Call.
// The initial request carries the full ModelContext; once a response ID exists, OpenAI
// retains that context server-side, so only the newly produced outputs must be sent.
func (a *OpenAIAdapter) AppendToolResults(results []ToolResult) {
	var messages []responses.ResponseInputItemUnionParam
	if a.previousResponseID == "" && a.params.Input.OfInputItemList != nil {
		messages = a.params.Input.OfInputItemList
	}

	for _, r := range results {
		messages = append(messages, responses.ResponseInputItemParamOfFunctionCallOutput(r.ID, toolResultOutput(r)))
		if !toolResultHasImageBytes(r.Images) {
			continue
		}
		var parts responses.ResponseInputMessageContentListParam
		parts = append(parts, responses.ResponseInputContentParamOfInputText(GeneratedToolImageCaption))
		parts = appendOpenAIResponseInputImages(parts, r.Images)
		messages = append(messages, responses.ResponseInputItemParamOfMessage(parts, RoleUser))
	}

	a.params.Input = responses.ResponseNewParamsInputUnion{
		OfInputItemList: messages,
	}
}

// ForceFinalResponse strips tools, appends a nudge, and issues one last Call.
func (a *OpenAIAdapter) ForceFinalResponse(ctx context.Context) (*GenerateResponse, error) {
	a.params.Tools = nil

	var messages []responses.ResponseInputItemUnionParam
	if a.params.Input.OfInputItemList != nil {
		messages = a.params.Input.OfInputItemList
	}
	messages = append(messages, responses.ResponseInputItemParamOfMessage(
		"Please provide your best final response based on the information gathered so far without additional tool calls.",
		"user",
	))
	a.params.Input = responses.ResponseNewParamsInputUnion{
		OfInputItemList: messages,
	}

	var (
		resp *responses.Response
		err  error
	)
	if a.textDeltaHandler != nil {
		resp, err = a.provider.CallWithRetryStreaming(ctx, a.params, a.textDeltaHandler)
	} else {
		resp, err = a.provider.CallWithRetry(ctx, a.params)
	}
	if err != nil {
		return nil, WrapSafetyViolationError(models.SafetyViolationProviderOpenAI, fmt.Errorf("OpenAI final-response call failed: %w", err))
	}
	a.lastResponse = resp
	a.rawResponses = append(a.rawResponses, resp)
	a.webSearchCompleted += countCompletedWebSearchesOpenAI(resp)
	return OpenAIToGenerateResponse(resp), nil
}

// AllRawResponses returns every Responses API payload observed this turn (each Call
// and ForceFinalResponse). Used to extract native web search output for persistence.
func (a *OpenAIAdapter) AllRawResponses() []*responses.Response {
	if len(a.rawResponses) == 0 {
		return nil
	}
	out := make([]*responses.Response, len(a.rawResponses))
	copy(out, a.rawResponses)
	return out
}

func countCompletedWebSearchesOpenAI(resp *responses.Response) int {
	if resp == nil {
		return 0
	}
	n := 0
	for _, out := range resp.Output {
		if out.Type != "web_search_call" {
			continue
		}
		ws := out.AsWebSearchCall()
		if ws.Status == responses.ResponseFunctionWebSearchStatusCompleted {
			n++
		}
	}
	return n
}

// extractOpenAIToolUses pulls function-call output items from a Response and
// normalises them to the provider-agnostic ToolUse type.
func extractOpenAIToolUses(resp *responses.Response) []ToolUse {
	if resp == nil {
		return nil
	}
	var uses []ToolUse
	for _, output := range resp.Output {
		if output.Type == "function_call" {
			tc := output.AsFunctionCall()
			uses = append(uses, ToolUse{
				ID:    tc.CallID,
				Name:  tc.Name,
				Input: []byte(tc.Arguments),
			})
		}
	}
	return uses
}
