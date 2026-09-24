package provider

import "github.com/openai/openai-go/v3/responses"

// Vendor-native web search for OpenAI (ADR 0x021): the fallback when first-party web search
// is not configured. Nothing here matches when PARALLEL_API_KEY is set, because the native
// web_search tool is then never sent (see applyWebSearchPolicy).

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
