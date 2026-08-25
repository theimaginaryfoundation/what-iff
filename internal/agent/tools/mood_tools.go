package tools

const ListModesToolDescription = `List modes attached to the current personality.

Each mode has a name, description, prompt_snippet, and optionally a recommended_model.
Use change_mode to switch the active mode for this conversation.`

const ChangeModeToolDescription = `Switch the active mode for this conversation.

When a mode is activated:
- Its prompt_snippet is injected as context for future turns.
- Its attached skills (MCPs) become available.
- If recommended_model is set and no model_override is given, the chat model changes to it.

Pass mode_id as the UUID from list_modes.
Optionally pass model_override (UUID or name) to force a specific model regardless of the mode's recommendation.`

var ListMoodsToolSpec = FunctionToolSpec{
	Name:        "list_modes",
	Description: ListModesToolDescription,
	Properties:  map[string]interface{}{},
	Required:    []string{},
}

var ChangeMoodToolSpec = FunctionToolSpec{
	Name:        "change_mode",
	Description: ChangeModeToolDescription,
	Properties: map[string]interface{}{
		"mode_id": map[string]interface{}{
			"type":        "string",
			"description": "UUID of the mode to activate.",
		},
		"model_override": map[string]interface{}{
			"type":        "string",
			"description": "Optional: UUID or name of the model to switch to, overriding the mode's recommended_model.",
		},
	},
	Required: []string{"mode_id"},
}
