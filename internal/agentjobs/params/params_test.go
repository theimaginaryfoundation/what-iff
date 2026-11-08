package params

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	mt "github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

func TestPlaceholders(t *testing.T) {
	got := Placeholders("News on {{topics}} for {{ region }}; again {{topics}}. Not {{1bad}} or {{a-b}} or {single}.")
	want := []string{"topics", "region"}
	if !slices.Equal(got, want) {
		t.Fatalf("Placeholders = %v, want %v", got, want)
	}
}

func TestPrepareValidation(t *testing.T) {
	tests := []struct {
		name      string
		prompt    string
		defs      []mt.JobParameter
		undefined []string
		problem   string
	}{
		{name: "undefined placeholder", prompt: "{{a}} {{b}}", defs: []mt.JobParameter{{Name: "a", Type: mt.JobParameterText}}, undefined: []string{"b"}},
		{name: "unused definition is fine", prompt: "no placeholders", defs: []mt.JobParameter{{Name: "a", Type: mt.JobParameterText}}},
		{name: "bad name", prompt: "", defs: []mt.JobParameter{{Name: "1a", Type: mt.JobParameterText}}, problem: "must start with a letter"},
		{name: "duplicate name", prompt: "", defs: []mt.JobParameter{{Name: "a", Type: mt.JobParameterText}, {Name: "a", Type: mt.JobParameterList}}, problem: "more than once"},
		{name: "unknown type", prompt: "", defs: []mt.JobParameter{{Name: "a", Type: "number"}}, problem: "type must be"},
		{name: "select without options", prompt: "", defs: []mt.JobParameter{{Name: "a", Type: mt.JobParameterSingleSelect, Options: []string{" "}}}, problem: "at least one option"},
		{name: "duplicate option", prompt: "", defs: []mt.JobParameter{{Name: "a", Type: mt.JobParameterMultiSelect, Options: []string{"x", "x "}}}, problem: "more than once"},
		{name: "bad format", prompt: "", defs: []mt.JobParameter{{Name: "a", Type: mt.JobParameterList, Format: "table"}}, problem: "comma or bullets"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Prepare(tt.prompt, tt.defs, nil)
			if len(tt.undefined) == 0 && tt.problem == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("want *ValidationError, got %v", err)
			}
			if !slices.Equal(verr.Undefined, tt.undefined) {
				t.Fatalf("Undefined = %v, want %v", verr.Undefined, tt.undefined)
			}
			if tt.problem != "" && !strings.Contains(verr.Error(), tt.problem) {
				t.Fatalf("error %q does not mention %q", verr.Error(), tt.problem)
			}
		})
	}
}

func TestPrepareNormalisesDefinitions(t *testing.T) {
	defs, _, err := Prepare("", []mt.JobParameter{
		{Name: " region ", Type: mt.JobParameterSingleSelect, Options: []string{" US", "EU", ""}, Format: mt.JobParameterFormatBullets},
		{Name: "tags", Type: mt.JobParameterMultiSelect, Options: []string{"a"}},
		{Name: "topics", Label: " Topics ", Type: mt.JobParameterList},
		{Name: "tone", Type: mt.JobParameterText, Options: []string{"ignored"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if defs[0].Name != "region" || defs[0].Label != "region" || !slices.Equal(defs[0].Options, []string{"US", "EU"}) || defs[0].Format != "" {
		t.Errorf("region = %+v", defs[0])
	}
	if defs[1].Format != mt.JobParameterFormatComma {
		t.Errorf("multi_select default format = %q, want comma", defs[1].Format)
	}
	if defs[2].Format != mt.JobParameterFormatBullets || defs[2].Label != "Topics" {
		t.Errorf("topics = %+v", defs[2])
	}
	if defs[3].Options != nil {
		t.Errorf("text options kept: %v", defs[3].Options)
	}
}

func TestPrepareValues(t *testing.T) {
	defs := []mt.JobParameter{
		{Name: "region", Type: mt.JobParameterSingleSelect, Options: []string{"US", "EU"}},
		{Name: "tags", Type: mt.JobParameterMultiSelect, Options: []string{"a", "b", "c"}},
		{Name: "stale", Type: mt.JobParameterMultiSelect, Options: []string{"a"}},
		{Name: "topics", Type: mt.JobParameterList},
		{Name: "tone", Type: mt.JobParameterText},
		{Name: "blank", Type: mt.JobParameterText},
	}
	_, values, err := Prepare("", defs, mt.JobParameterValues{
		"region":  mt.TextValue("EU"),
		"tags":    mt.ListValue("c", "a", "a"),
		"stale":   mt.ListValue("a", "gone"),
		"topics":  mt.ListValue(" AI ", "", "climate"),
		"tone":    mt.TextValue("  dry "),
		"blank":   mt.TextValue("   "),
		"unknown": mt.TextValue("x"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if values["region"].Text != "EU" || !slices.Equal(values["tags"].Items, []string{"a", "c"}) ||
		!slices.Equal(values["topics"].Items, []string{"AI", "climate"}) || values["tone"].Text != "dry" {
		t.Errorf("values = %+v", values)
	}
	for _, dropped := range []string{"stale", "blank", "unknown"} {
		if _, ok := values[dropped]; ok {
			t.Errorf("%s should be dropped", dropped)
		}
	}
	if got := Missing(defs, values); !slices.Equal(got, []string{"stale", "blank"}) {
		t.Errorf("Missing = %v", got)
	}

	_, _, err = Prepare("", defs, mt.JobParameterValues{"topics": mt.TextValue("AI")})
	if err == nil || !strings.Contains(err.Error(), "must be a list") {
		t.Errorf("shape mismatch error = %v", err)
	}
}

func TestRender(t *testing.T) {
	defs := []mt.JobParameter{
		{Name: "region", Type: mt.JobParameterSingleSelect, Options: []string{"US", "EU"}},
		{Name: "tags", Type: mt.JobParameterMultiSelect, Options: []string{"a", "b"}, Format: mt.JobParameterFormatComma},
		{Name: "topics", Type: mt.JobParameterList, Format: mt.JobParameterFormatBullets},
	}
	values := mt.JobParameterValues{
		"region": mt.TextValue("EU"),
		"tags":   mt.ListValue("a", "b"),
		"topics": mt.ListValue("AI", "climate"),
	}
	tests := []struct{ prompt, want string }{
		{"News in {{region}} on {{ tags }}.", "News in EU on a, b."},
		{"Topics:\n{{topics}}\nThanks", "Topics:\n- AI\n- climate\nThanks"},
		{"Topics: {{topics}} thanks", "Topics:\n- AI\n- climate\nthanks"},
		{"Keep {{undefined}} as is", "Keep {{undefined}} as is"},
	}
	for _, tt := range tests {
		got, err := Render(tt.prompt, defs, values)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("Render(%q) = %q, want %q", tt.prompt, got, tt.want)
		}
	}

	_, err := Render("{{region}}", defs, mt.JobParameterValues{"region": mt.TextValue("EU")})
	var missing *MissingError
	if !errors.As(err, &missing) || !slices.Equal(missing.Names, []string{"tags", "topics"}) {
		t.Errorf("Render with missing values = %v", err)
	}
}

func TestValueJSON(t *testing.T) {
	var values mt.JobParameterValues
	if err := json.Unmarshal([]byte(`{"a":"x","b":["y","z"],"c":null}`), &values); err != nil {
		t.Fatal(err)
	}
	if values["a"].List || values["a"].Text != "x" || !values["b"].List || !slices.Equal(values["b"].Items, []string{"y", "z"}) {
		t.Fatalf("decoded = %+v", values)
	}
	if err := json.Unmarshal([]byte(`{"a":3}`), &values); err == nil {
		t.Fatal("number value should be rejected")
	}
	out, _ := json.Marshal(mt.JobParameterValues{"b": mt.ListValue()})
	if string(out) != `{"b":[]}` {
		t.Fatalf("encoded = %s", out)
	}
}

func TestStatusAfterSave(t *testing.T) {
	tests := []struct {
		current models.AgentJobStatus
		missing bool
		want    models.AgentJobStatus
	}{
		{models.AgentJobStatusActive, true, models.AgentJobStatusNeedsValues},
		{models.AgentJobStatusActive, false, models.AgentJobStatusActive},
		{models.AgentJobStatusNeedsValues, false, models.AgentJobStatusActive},
		{models.AgentJobStatusNeedsValues, true, models.AgentJobStatusNeedsValues},
		{models.AgentJobStatusPaused, true, models.AgentJobStatusPaused},
		{models.AgentJobStatusPaused, false, models.AgentJobStatusPaused},
		{models.AgentJobStatusComplete, true, models.AgentJobStatusComplete},
	}
	for _, tt := range tests {
		if got := StatusAfterSave(tt.current, tt.missing); got != tt.want {
			t.Errorf("StatusAfterSave(%s, %v) = %s, want %s", tt.current, tt.missing, got, tt.want)
		}
	}
}
