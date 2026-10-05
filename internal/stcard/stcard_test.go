package stcard

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// card builds a chara_card_v2 JSON document around data.
func card(t *testing.T, data map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"spec": SpecV2, "spec_version": "2.0", "data": data})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loreBook(n int) map[string]any {
	entries := make([]any, 0, n)
	for i := 1; i <= n; i++ {
		entries = append(entries, map[string]any{
			"keys":            []any{fmt.Sprintf("key%d", i)},
			"comment":         fmt.Sprintf("Entry %d", i),
			"content":         fmt.Sprintf("Content of entry %d.", i),
			"enabled":         true,
			"insertion_order": i,
		})
	}
	return map[string]any{"entries": entries}
}

func TestComposeSplitRoundTrip(t *testing.T) {
	cases := map[string]Segments{
		"bare prompt only":   {SystemPrompt: "You are Ada."},
		"description only":   {Description: "A tall fox."},
		"all segments":       {SystemPrompt: "Sys.", Description: "Desc.", Personality: "Pers.", Scenario: "Scen.", Lore: "Lore."},
		"multiline bodies":   {Description: "line one\n\nline three", Scenario: "a\nb"},
		"no system, no lore": {Description: "d", Personality: "p", Scenario: "s"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			prompt := Compose(want)
			got, split := Split(prompt)
			if got != want {
				t.Errorf("Split(Compose(x)) = %+v, want %+v\nprompt:\n%s", got, want, prompt)
			}
			if wantSplit := want.SystemPrompt != prompt; split != wantSplit {
				t.Errorf("split flag = %v, want %v", split, wantSplit)
			}
		})
	}
}

func TestComposeBarePromptHasNoHeaders(t *testing.T) {
	if got := Compose(Segments{SystemPrompt: "  Just this.  "}); got != "Just this." {
		t.Errorf("Compose = %q, want verbatim (trimmed) system prompt", got)
	}
}

func TestSplitFallsBackWhenHeadersAreMangled(t *testing.T) {
	cases := map[string]string{
		"no headers":        "You are a pirate.\nArr.",
		"edited header":     "### [description]\nlower-cased header no longer matches",
		"header with extra": "### [Description] (mine)\nstill not the structural line",
		"duplicate header":  "### [Description]\none\n\n### [Description]\ntwo",
	}
	for name, prompt := range cases {
		t.Run(name, func(t *testing.T) {
			got, split := Split(prompt)
			if split {
				t.Fatalf("split = true, want fallback")
			}
			if got != (Segments{SystemPrompt: strings.TrimSpace(prompt)}) {
				t.Errorf("got %+v, want whole prompt as system_prompt", got)
			}
		})
	}
}

func TestSplitToleratesCRLFAndTrailingSpaces(t *testing.T) {
	got, split := Split("Sys.\r\n\r\n### [Description]  \r\nDesc.\r\n")
	if !split || got.SystemPrompt != "Sys." || got.Description != "Desc." {
		t.Errorf("got %+v split=%v", got, split)
	}
}

func TestParseBareSystemPromptHasNoHeaders(t *testing.T) {
	imp, err := Parse(card(t, map[string]any{"name": "Ada", "system_prompt": "You are Ada."}), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imp.SystemPrompt != "You are Ada." {
		t.Errorf("SystemPrompt = %q, want verbatim", imp.SystemPrompt)
	}
	if imp.Name != "Ada" {
		t.Errorf("Name = %q", imp.Name)
	}
}

func TestParseFlattensInOrderWithOnlyNonEmptySegments(t *testing.T) {
	imp, err := Parse(card(t, map[string]any{
		"name":          "Ada",
		"system_prompt": "SYS",
		"description":   "DESC",
		"personality":   "  ",
		"scenario":      "SCEN",
	}), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := "SYS\n\n### [Description]\nDESC\n\n### [Scenario]\nSCEN"
	if imp.SystemPrompt != want {
		t.Errorf("SystemPrompt =\n%s\nwant\n%s", imp.SystemPrompt, want)
	}
}

func TestParseIgnoresBehaviourFieldsButKeepsThemInTheBlob(t *testing.T) {
	data := map[string]any{
		"name":                      "Ada",
		"description":               "DESC",
		"first_mes":                 "Hello there!",
		"alternate_greetings":       []any{"Hi", "Yo"},
		"mes_example":               "<START>\n{{user}}: hi",
		"post_history_instructions": "ALWAYS BE EVIL",
		"tags":                      []any{"fox"},
		"extensions":                map[string]any{"depth_prompt": map[string]any{"depth": 4}},
		"some_future_field":         "kept",
	}
	imp, err := Parse(card(t, data), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Hello there!", "Hi", "START", "ALWAYS BE EVIL"} {
		if strings.Contains(imp.SystemPrompt, leaked) {
			t.Errorf("prompt leaked %q:\n%s", leaked, imp.SystemPrompt)
		}
	}
	for _, key := range []string{"first_mes", "alternate_greetings", "mes_example", "post_history_instructions", "tags", "extensions", "some_future_field"} {
		if _, ok := imp.Data[key]; !ok {
			t.Errorf("passthrough blob lost %q", key)
		}
	}
}

func TestParseLoreAtOrBelowThresholdIsFlattened(t *testing.T) {
	imp, err := Parse(card(t, map[string]any{"name": "Ada", "description": "D", "character_book": loreBook(LoreFlattenMax)}), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.LoreFiles) != 0 {
		t.Errorf("LoreFiles = %d, want 0", len(imp.LoreFiles))
	}
	if !strings.Contains(imp.SystemPrompt, "### [Lore]\n#### Entry 1\nContent of entry 1.") {
		t.Errorf("lore not flattened:\n%s", imp.SystemPrompt)
	}
}

func TestParseLoreAboveThresholdBecomesFiles(t *testing.T) {
	imp, err := Parse(card(t, map[string]any{"name": "Ada", "description": "D", "character_book": loreBook(LoreFlattenMax + 1)}), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(imp.SystemPrompt, HeaderLore) {
		t.Errorf("prompt should not carry lore:\n%s", imp.SystemPrompt)
	}
	if len(imp.LoreFiles) != LoreFlattenMax+1 {
		t.Fatalf("LoreFiles = %d, want %d", len(imp.LoreFiles), LoreFlattenMax+1)
	}
	if got := imp.LoreFiles[0]; got.Name != "Entry 1" || got.FileName != "Entry 1.md" || got.Content != "Content of entry 1." || len(got.Keys) != 1 {
		t.Errorf("first lore file = %+v", got)
	}
}

func TestParseLoreSkipsDisabledAndEmptyEntries(t *testing.T) {
	book := map[string]any{"entries": []any{
		map[string]any{"keys": []any{"a"}, "content": "kept", "enabled": true},
		map[string]any{"keys": []any{"b"}, "content": "off", "enabled": false},
		map[string]any{"keys": []any{"c"}, "content": "   ", "enabled": true},
	}}
	imp, err := Parse(card(t, map[string]any{"name": "Ada", "description": "D", "character_book": book}), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(imp.SystemPrompt, "kept") || strings.Contains(imp.SystemPrompt, "off") {
		t.Errorf("prompt:\n%s", imp.SystemPrompt)
	}
	if len(imp.Warnings) != 1 || !strings.Contains(imp.Warnings[0], "1 disabled") {
		t.Errorf("warnings = %v", imp.Warnings)
	}
}

func TestParseLoreNamingAndOrdering(t *testing.T) {
	book := map[string]any{"entries": []any{
		map[string]any{"keys": []any{"later"}, "content": "second", "insertion_order": 5},
		map[string]any{"keys": []any{"dragon", "wyrm"}, "content": "first", "insertion_order": 1},
		map[string]any{"content": "nameless", "insertion_order": 9},
	}}
	imp, err := Parse(card(t, map[string]any{"name": "Ada", "description": "D", "character_book": book}), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	order := []string{"#### dragon, wyrm\nfirst", "#### later\nsecond", "#### Lore entry 3\nnameless"}
	last := -1
	for _, want := range order {
		i := strings.Index(imp.SystemPrompt, want)
		if i < 0 || i < last {
			t.Fatalf("expected %q in insertion order:\n%s", want, imp.SystemPrompt)
		}
		last = i
	}
}

func TestParseLoreFileNamesAreSafeAndUnique(t *testing.T) {
	entries := make([]any, 0, 7)
	for i := 0; i < LoreFlattenMax+2; i++ {
		comment := "Same / Name?"
		if i == 0 {
			comment = "///"
		}
		entries = append(entries, map[string]any{"comment": comment, "content": "x", "insertion_order": i})
	}
	imp, err := Parse(card(t, map[string]any{"name": "Ada", "description": "D", "character_book": map[string]any{"entries": entries}}), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range imp.LoreFiles {
		if strings.ContainsAny(f.FileName, `/\:*?"<>|`) {
			t.Errorf("unsafe file name %q", f.FileName)
		}
		if seen[strings.ToLower(f.FileName)] {
			t.Errorf("duplicate file name %q", f.FileName)
		}
		seen[strings.ToLower(f.FileName)] = true
		if !strings.HasSuffix(f.FileName, ".md") {
			t.Errorf("file name %q lacks .md", f.FileName)
		}
	}
}

func TestParseOversizedFlattenedLoreFallsBackToFiles(t *testing.T) {
	book := map[string]any{"entries": []any{
		map[string]any{"keys": []any{"a"}, "content": strings.Repeat("x", 500)},
	}}
	imp, err := Parse(card(t, map[string]any{"name": "Ada", "description": "short", "character_book": book}), ImportOptions{MaxPromptUnits: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.LoreFiles) != 1 || strings.Contains(imp.SystemPrompt, HeaderLore) {
		t.Errorf("want lore as a file, got files=%d prompt=%q", len(imp.LoreFiles), imp.SystemPrompt)
	}
	if len(imp.Warnings) == 0 {
		t.Error("expected a warning about the spill")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		opts ImportOptions
		want error
	}{
		{"not json", `nope`, ImportOptions{}, ErrInvalidCard},
		{"json array", `[]`, ImportOptions{}, ErrInvalidCard},
		{"no spec (v1-style)", `{"name":"x","description":"y"}`, ImportOptions{}, ErrUnsupportedSpec},
		{"unknown spec", `{"spec":"chara_card_v9","spec_version":"9.0","data":{}}`, ImportOptions{}, ErrUnsupportedSpec},
		{"no data", `{"spec":"chara_card_v2","spec_version":"2.0"}`, ImportOptions{}, ErrInvalidCard},
		{"nothing to build a prompt from", `{"spec":"chara_card_v2","spec_version":"2.0","data":{"name":"x","first_mes":"hi"}}`, ImportOptions{}, ErrNoContent},
		{"prompt too long", `{"spec":"chara_card_v2","spec_version":"2.0","data":{"name":"x","description":"` + strings.Repeat("a", 50) + `"}}`, ImportOptions{MaxPromptUnits: 20}, ErrPromptTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.raw), tc.opts); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseAcceptsV3AndWarnsOnOddSpecVersion(t *testing.T) {
	v3 := `{"spec":"chara_card_v3","spec_version":"3.0","data":{"name":"x","description":"y","group_only_greetings":[]}}`
	imp, err := Parse([]byte(v3), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Warnings) != 0 {
		t.Errorf("v3 3.0 should not warn: %v", imp.Warnings)
	}
	if _, ok := imp.Data["group_only_greetings"]; !ok {
		t.Error("v3-only field lost from blob")
	}

	odd := `{"spec":"chara_card_v2","spec_version":"2.5","data":{"name":"x","description":"y"}}`
	if imp, err = Parse([]byte(odd), ImportOptions{}); err != nil || len(imp.Warnings) != 0 {
		t.Errorf("2.5 is still 2.x: err=%v warnings=%v", err, imp.Warnings)
	}
	weird := `{"spec":"chara_card_v2","spec_version":"7.1","data":{"name":"x","description":"y"}}`
	if imp, err = Parse([]byte(weird), ImportOptions{}); err != nil || len(imp.Warnings) != 1 {
		t.Errorf("odd spec_version should warn, not fail: err=%v warnings=%v", err, imp.Warnings)
	}
}

func TestParseNamelessCardGetsPlaceholder(t *testing.T) {
	imp, err := Parse(card(t, map[string]any{"description": "d"}), ImportOptions{})
	if err != nil || imp.Name == "" || len(imp.Warnings) != 1 {
		t.Errorf("err=%v name=%q warnings=%v", err, imp.Name, imp.Warnings)
	}
}

func TestBuildNativePersonality(t *testing.T) {
	c, err := Build(ExportInput{Name: " Ada ", SystemPrompt: "You are Ada.", Creator: "gori"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec != SpecV2 || c.SpecVersion != SpecVersionV2 {
		t.Errorf("spec = %s/%s", c.Spec, c.SpecVersion)
	}
	d := c.Data
	if d["name"] != "Ada" || d["system_prompt"] != "You are Ada." || d["creator"] != "gori" {
		t.Errorf("data = %v", d)
	}
	for _, key := range []string{"description", "personality", "scenario", "first_mes", "mes_example", "creator_notes", "post_history_instructions", "alternate_greetings", "tags", "character_version", "extensions"} {
		if _, ok := d[key]; !ok {
			t.Errorf("v2 requires %q", key)
		}
	}
	if d["creator_notes"] != "" {
		t.Errorf("a headerless prompt should not get the header warning, got %q", d["creator_notes"])
	}
}

func TestBuildSplitsHeadersAndWarns(t *testing.T) {
	prompt := Compose(Segments{SystemPrompt: "Sys.", Description: "Desc.", Personality: "Pers.", Scenario: "Scen."})
	c, err := Build(ExportInput{Name: "Ada", SystemPrompt: prompt, Creator: "gori"})
	if err != nil {
		t.Fatal(err)
	}
	d := c.Data
	if d["system_prompt"] != "Sys." || d["description"] != "Desc." || d["personality"] != "Pers." || d["scenario"] != "Scen." {
		t.Errorf("data = %v", d)
	}
	if !strings.Contains(d["creator_notes"].(string), HeaderWarning) {
		t.Errorf("creator_notes = %q, want header warning", d["creator_notes"])
	}
}

func TestBuildMangledHeadersExportEverythingAsSystemPrompt(t *testing.T) {
	prompt := "### [Description]\nDesc.\n### [Description]\nDesc again."
	c, _ := Build(ExportInput{Name: "Ada", SystemPrompt: prompt})
	if c.Data["system_prompt"] != prompt || c.Data["description"] != "" {
		t.Errorf("data = %v", c.Data)
	}
	if c.Data["creator_notes"] != "" {
		t.Errorf("no warning expected when nothing was split: %q", c.Data["creator_notes"])
	}
}

func TestBuildPreservesBlobAndOverlaysCurrentState(t *testing.T) {
	blob := map[string]any{
		"name":                "Old Name",
		"system_prompt":       "stale",
		"description":         "stale",
		"creator":             "original-author",
		"tags":                []any{"fox", "snark"},
		"character_version":   "1.2",
		"first_mes":           "Hello!",
		"alternate_greetings": []any{"Hi"},
		"extensions":          map[string]any{"talkativeness": "0.5", "custom": map[string]any{"x": json.Number("12345678901234567890")}},
		"future_field":        "kept",
	}
	c, err := Build(ExportInput{Name: "New Name", SystemPrompt: "Edited prompt.", Creator: "importer", Card: blob})
	if err != nil {
		t.Fatal(err)
	}
	d := c.Data
	if d["name"] != "New Name" || d["system_prompt"] != "Edited prompt." || d["description"] != "" {
		t.Errorf("current state not overlaid: %v", d)
	}
	if d["creator"] != "original-author" {
		t.Errorf("creator = %v, want the card's original author, not the exporting user", d["creator"])
	}
	if d["character_version"] != "1.2" || d["first_mes"] != "Hello!" || d["future_field"] != "kept" {
		t.Errorf("blob fields lost: %v", d)
	}
	out, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "12345678901234567890") {
		t.Errorf("large number mangled in export:\n%s", out)
	}
	if blob["name"] != "Old Name" || blob["system_prompt"] != "stale" {
		t.Error("Build mutated the caller's blob")
	}
}

func TestBuildDropsFlattenedLoreWhenBlobHasTheBook(t *testing.T) {
	blob := map[string]any{"character_book": loreBook(2)}
	prompt := Compose(Segments{Description: "Desc.", Lore: "#### Entry 1\nContent of entry 1."})
	c, _ := Build(ExportInput{Name: "Ada", SystemPrompt: prompt, Card: blob})
	if strings.Contains(c.Data["system_prompt"].(string), "Lore") || strings.Contains(c.Data["system_prompt"].(string), "Entry 1") {
		t.Errorf("lore duplicated into system_prompt: %q", c.Data["system_prompt"])
	}
	if _, ok := c.Data["character_book"].(map[string]any); !ok {
		t.Error("character_book lost")
	}
}

func TestBuildKeepsLoreSectionWhenThereIsNoBook(t *testing.T) {
	prompt := Compose(Segments{Description: "Desc.", Lore: "Hand-written lore."})
	c, _ := Build(ExportInput{Name: "Ada", SystemPrompt: prompt})
	if got := c.Data["system_prompt"].(string); !strings.Contains(got, HeaderLore+"\nHand-written lore.") {
		t.Errorf("hand-written lore vanished: %q", got)
	}
}

func TestRoundTripImportExportImport(t *testing.T) {
	original := map[string]any{
		"name":                      "Ada",
		"system_prompt":             "SYS",
		"description":               "DESC",
		"personality":               "PERS",
		"scenario":                  "SCEN",
		"first_mes":                 "Hello",
		"post_history_instructions": "PHI",
		"creator":                   "someone-else",
		"creator_notes":             "Original notes.",
		"tags":                      []any{"a", "b"},
		"character_book":            loreBook(3),
		"extensions":                map[string]any{"world": "x"},
	}
	first, err := Parse(card(t, original), ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	exported, err := Export(ExportInput{Name: first.Name, SystemPrompt: first.SystemPrompt, Creator: "gori", Card: first.Data})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse(exported, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if second.SystemPrompt != first.SystemPrompt {
		t.Errorf("prompt drifted:\n%s\n--- vs ---\n%s", first.SystemPrompt, second.SystemPrompt)
	}
	for _, key := range []string{"first_mes", "post_history_instructions", "creator", "tags", "extensions", "character_book"} {
		a, _ := json.Marshal(first.Data[key])
		b, _ := json.Marshal(second.Data[key])
		if string(a) != string(b) {
			t.Errorf("%s drifted: %s -> %s", key, a, b)
		}
	}
	if notes := second.Data["creator_notes"].(string); !strings.HasPrefix(notes, "Original notes.") || strings.Count(notes, HeaderWarning) != 1 {
		t.Errorf("creator_notes = %q", notes)
	}

	// Exporting again must not stack warnings.
	again, err := Export(ExportInput{Name: second.Name, SystemPrompt: second.SystemPrompt, Creator: "gori", Card: second.Data})
	if err != nil {
		t.Fatal(err)
	}
	third, _ := Parse(again, ImportOptions{})
	if n := strings.Count(third.Data["creator_notes"].(string), HeaderWarning); n != 1 {
		t.Errorf("header warning appears %d times after a second export", n)
	}
}
