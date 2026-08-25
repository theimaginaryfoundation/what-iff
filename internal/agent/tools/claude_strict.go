package tools

// claudeStrictToolNames lists tools registered with Anthropic strict mode enabled.
// Complex multi-field tools benefit most from schema-guaranteed inputs; simple tools
// stay non-strict so their descriptions do not contribute to Anthropic's combined
// strict-schema grammar compile budget.
var claudeStrictToolNames = map[string]struct{}{
	CreateAgentJobToolSpec.Name: {},
	RunSubagentToolSpec.Name:    {},
	GenerateImageToolSpec.Name:  {},
}

// ClaudeToolStrict reports whether a function tool should use Anthropic strict mode.
func ClaudeToolStrict(spec FunctionToolSpec) bool {
	_, ok := claudeStrictToolNames[spec.Name]
	return ok
}
