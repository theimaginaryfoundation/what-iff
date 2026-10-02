package stcard

import (
	"errors"
	"strings"
	"testing"
)

func TestParseOverLimitShedsScenarioThenPersonalityAndSaysSo(t *testing.T) {
	data := map[string]any{
		"name":        "Ada",
		"description": strings.Repeat("d", 60),
		"personality": strings.Repeat("p", 60),
		"scenario":    strings.Repeat("s", 60),
	}

	// Dropping the scenario alone is enough.
	imp, err := Parse(card(t, data), ImportOptions{MaxPromptUnits: 200})
	if err != nil {
		t.Fatal(err)
	}
	if got := imp.OmittedFields; len(got) != 1 || got[0] != "scenario" {
		t.Errorf("OmittedFields = %v, want [scenario]", got)
	}
	if strings.Contains(imp.SystemPrompt, HeaderScenario) || !strings.Contains(imp.SystemPrompt, HeaderPersonality) {
		t.Errorf("prompt:\n%s", imp.SystemPrompt)
	}
	if len(imp.Warnings) != 1 || !strings.Contains(imp.Warnings[0], "scenario") || !strings.Contains(imp.Warnings[0], "export") {
		t.Errorf("warnings = %v", imp.Warnings)
	}
	if imp.Data["scenario"] != data["scenario"] {
		t.Error("the omitted field must stay in the passthrough blob")
	}

	// A tighter limit sheds the personality too.
	imp, err = Parse(card(t, data), ImportOptions{MaxPromptUnits: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got := imp.OmittedFields; len(got) != 2 || got[0] != "scenario" || got[1] != "personality" {
		t.Errorf("OmittedFields = %v", got)
	}
}

func TestParseFitsWithoutShedding(t *testing.T) {
	imp, err := Parse(card(t, map[string]any{"name": "Ada", "description": "d", "scenario": "s"}), ImportOptions{MaxPromptUnits: 500})
	if err != nil || len(imp.OmittedFields) != 0 || !strings.Contains(imp.SystemPrompt, HeaderScenario) {
		t.Errorf("err=%v omitted=%v prompt=%q", err, imp.OmittedFields, imp.SystemPrompt)
	}
}

func TestParseStillTooLongAfterSheddingExplainsWhy(t *testing.T) {
	data := map[string]any{"name": "Ada", "description": strings.Repeat("d", 300), "scenario": "s"}

	_, err := Parse(card(t, data), ImportOptions{MaxPromptUnits: 100})

	if !errors.Is(err, ErrPromptTooLong) {
		t.Fatalf("err = %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "limit of 100") || !strings.Contains(msg, "scenario") {
		t.Errorf("message = %q", msg)
	}
}

func TestParseOnlyAHugeScenarioIsTooLongNotEmpty(t *testing.T) {
	_, err := Parse(card(t, map[string]any{"name": "Ada", "scenario": strings.Repeat("s", 300)}), ImportOptions{MaxPromptUnits: 100})
	if !errors.Is(err, ErrPromptTooLong) {
		t.Errorf("err = %v, want ErrPromptTooLong", err)
	}
}

func TestParseLoreNeverCausesShedding(t *testing.T) {
	data := map[string]any{
		"name": "Ada", "description": "d", "scenario": "s",
		"character_book": map[string]any{"entries": []any{map[string]any{"keys": []any{"k"}, "content": strings.Repeat("x", 500)}}},
	}
	imp, err := Parse(card(t, data), ImportOptions{MaxPromptUnits: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.OmittedFields) != 0 || len(imp.LoreFiles) != 1 {
		t.Errorf("omitted=%v lore=%d: the book should spill to files, leaving the scenario alone", imp.OmittedFields, len(imp.LoreFiles))
	}
}

func TestBuildRestoresOmittedFieldsUntilThePromptHasThemAgain(t *testing.T) {
	blob := map[string]any{"scenario": "The original scenario.", "personality": "Original personality."}
	prompt := Compose(Segments{Description: "Desc."})

	c, _ := Build(ExportInput{Name: "Ada", SystemPrompt: prompt, Card: blob, OmittedFields: []string{"scenario"}})
	if c.Data["scenario"] != "The original scenario." {
		t.Errorf("omitted scenario not restored: %v", c.Data["scenario"])
	}
	if c.Data["personality"] != "" {
		t.Errorf("a field that was not omitted follows the prompt: %v", c.Data["personality"])
	}

	// Once the user writes their own Scenario section, it wins.
	edited := Compose(Segments{Description: "Desc.", Scenario: "Mine now."})
	c, _ = Build(ExportInput{Name: "Ada", SystemPrompt: edited, Card: blob, OmittedFields: []string{"scenario"}})
	if c.Data["scenario"] != "Mine now." {
		t.Errorf("scenario = %v", c.Data["scenario"])
	}
}
