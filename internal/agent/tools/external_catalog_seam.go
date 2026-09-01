package tools

// AdditionalFunctionToolCatalog, when non-nil, can contribute extra function
// tool definitions at runtime.
var AdditionalFunctionToolCatalog func() []FunctionToolDefinition

func mergedFunctionToolCatalog() []FunctionToolDefinition {
	out := make([]FunctionToolDefinition, 0, len(functionToolCatalog)+2)
	out = append(out, functionToolCatalog...)
	if AdditionalFunctionToolCatalog != nil {
		extras := AdditionalFunctionToolCatalog()
		if len(extras) > 0 {
			out = append(out, extras...)
		}
	}
	return out
}
