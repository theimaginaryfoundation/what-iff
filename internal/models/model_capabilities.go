package models

// DeriveModelCapabilities converts the model facts that already drive provider
// routing into the structured capability contract exposed by the API. toolNames
// should contain the built-in function tools available to tool-calling models.
// Provider-native web search and MCP are added here only where the current agent
// runtime actually wires those features.
func DeriveModelCapabilities(model *Model, toolNames []string) ModelCapabilities {
	if model == nil {
		return ModelCapabilities{Tools: []string{}}
	}

	provider := ProviderForModel(model.Provider, model.Name)
	caps := ModelCapabilities{
		ToolCalling: model.ToolSupport,
		Vision:      modelSupportsVision(provider, model.Name),
		MCP:         model.ToolSupport && (provider == ModelProviderOpenAI || provider == ModelProviderAnthropic),
		Tools:       []string{},
	}
	if !model.ToolSupport {
		return caps
	}

	caps.Tools = append(caps.Tools, toolNames...)
	if provider == ModelProviderOpenAI || provider == ModelProviderAnthropic {
		caps.Tools = append(caps.Tools, "web_search")
	}
	return caps
}

func modelSupportsVision(provider ModelProvider, name string) bool {
	switch provider {
	case ModelProviderOpenAI:
		// The OpenAI Responses path already accepts image inputs for catalog models.
		return true
	case ModelProviderAnthropic, ModelProviderZAI:
		// These share the existing Anthropic Messages multimodal rendering path.
		return true
	default:
		return ChatCompletionsSupportsVision(string(provider), name)
	}
}
