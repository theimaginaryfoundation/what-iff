package tools

// Skill lifecycle tools (ADR 0x035 §2). A skill's index line (slug and description) is always in
// the developer context; its instructions arrive only when the model loads it. Loaded skills
// persist per chat like loaded tools and unload at a checkpoint when the segment did not use them.

const LoadSkillDescription = `Load one or more skills by slug from the "Skills available" list in your instructions, when the user's request matches a skill's description.

Loading a skill returns its full instructions (follow them for the task), lists its reference files, brings its connectors into scope with an inventory of their tools (call load_tools to load the ones you need), and loads the built-in tools the skill names. A loaded skill stays loaded across turns in this chat until you unload it or a checkpoint finds it unused.

Do not load a skill whose instructions you already have in this turn; do not load skills speculatively.`

const UnloadSkillDescription = `Unload skills you loaded with load_skill and no longer need, by slug. Skills preloaded by the active mode, a job, or the user's /skill call are not loaded state and cannot be unloaded here.`

var LoadSkillToolSpec = FunctionToolSpec{
	Name:        "load_skill",
	Description: LoadSkillDescription,
	Properties: map[string]interface{}{
		"skills": map[string]interface{}{
			"type":        "array",
			"description": "Skill slugs to load, as listed under \"Skills available\" (for example [\"pdf-forms\"]).",
			"items": map[string]interface{}{
				"type": "string",
			},
			"minItems": 1,
		},
	},
	Required: []string{"skills"},
}

var UnloadSkillToolSpec = FunctionToolSpec{
	Name:        "unload_skill",
	Description: UnloadSkillDescription,
	Properties: map[string]interface{}{
		"skills": map[string]interface{}{
			"type":        "array",
			"description": "Skill slugs to unload, or [\"all\"] to unload every skill you loaded in this chat.",
			"items": map[string]interface{}{
				"type": "string",
			},
			"minItems": 1,
		},
	},
	Required: []string{"skills"},
}
