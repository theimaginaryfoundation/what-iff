// Package params implements job parameters (ADR 0x044): `{{name}}` placeholders in a job prompt,
// their typed definitions, the owner's values, and rendering the prompt for a run.
// It is pure: no I/O, no datastore, so the API, the agent tools, the scheduler and the
// private overlay share one set of rules.
package params

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// Limits (ADR 0x044, decision 5).
const (
	MaxParameters     = 20
	MaxOptions        = 50
	MaxListItems      = 50
	MaxValueChars     = 2000
	MaxLabelChars     = 200
	MaxDescriptionLen = 500
)

var (
	placeholderRE = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
	nameRE        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Placeholders returns the distinct placeholder names in prompt, in order of first use.
func Placeholders(prompt string) []string {
	var names []string
	for _, m := range placeholderRE.FindAllStringSubmatch(prompt, -1) {
		if !slices.Contains(names, m[1]) {
			names = append(names, m[1])
		}
	}
	return names
}

// ValidationError lists everything wrong with a job's parameters, so a client can show it all at once.
type ValidationError struct {
	// Undefined are placeholders in the prompt with no definition.
	Undefined []string `json:"undefined_placeholders,omitempty"`
	// Problems describe invalid definitions or values.
	Problems []string `json:"problems,omitempty"`
}

func (e *ValidationError) Error() string {
	var parts []string
	if len(e.Undefined) > 0 {
		parts = append(parts, "placeholders with no parameter definition: "+strings.Join(e.Undefined, ", "))
	}
	parts = append(parts, e.Problems...)
	return strings.Join(parts, "; ")
}

func (e *ValidationError) empty() bool { return len(e.Undefined) == 0 && len(e.Problems) == 0 }

func (e *ValidationError) addf(format string, args ...any) {
	e.Problems = append(e.Problems, fmt.Sprintf(format, args...))
}

// DefaultFormat is the format a multi-value type renders in when its definition sets none.
func DefaultFormat(t modeltypes.JobParameterType) modeltypes.JobParameterFormat {
	switch t {
	case modeltypes.JobParameterMultiSelect:
		return modeltypes.JobParameterFormatComma
	case modeltypes.JobParameterList:
		return modeltypes.JobParameterFormatBullets
	default:
		return ""
	}
}

// Prepare normalises a job's definitions and values and checks them against its prompt.
// It returns the definitions and values to store, or a *ValidationError.
// Values for unknown names, and select values with an option no longer offered, are dropped
// rather than rejected (decision 2); missing values are not an error (see Missing).
func Prepare(prompt string, defs []modeltypes.JobParameter, values modeltypes.JobParameterValues) ([]modeltypes.JobParameter, modeltypes.JobParameterValues, error) {
	verr := &ValidationError{}
	normDefs := normalizeDefinitions(defs, verr)
	for _, name := range Placeholders(prompt) {
		if !slices.ContainsFunc(normDefs, func(d modeltypes.JobParameter) bool { return d.Name == name }) {
			verr.Undefined = append(verr.Undefined, name)
		}
	}
	normValues := normalizeValues(normDefs, values, verr)
	if !verr.empty() {
		return nil, nil, verr
	}
	return normDefs, normValues, nil
}

func normalizeDefinitions(defs []modeltypes.JobParameter, verr *ValidationError) []modeltypes.JobParameter {
	if len(defs) > MaxParameters {
		verr.addf("a job can have at most %d parameters", MaxParameters)
	}
	out := make([]modeltypes.JobParameter, 0, len(defs))
	seen := map[string]bool{}
	for _, d := range defs {
		d.Name = strings.TrimSpace(d.Name)
		if !nameRE.MatchString(d.Name) {
			verr.addf("parameter name %q must start with a letter or underscore and use only letters, digits and underscores", d.Name)
			continue
		}
		if seen[d.Name] {
			verr.addf("parameter %s is defined more than once", d.Name)
			continue
		}
		seen[d.Name] = true

		d.Label = strings.TrimSpace(d.Label)
		if d.Label == "" {
			d.Label = d.Name
		}
		if utf8.RuneCountInString(d.Label) > MaxLabelChars {
			verr.addf("parameter %s: label is longer than %d characters", d.Name, MaxLabelChars)
		}
		d.Description = strings.TrimSpace(d.Description)
		if utf8.RuneCountInString(d.Description) > MaxDescriptionLen {
			verr.addf("parameter %s: description is longer than %d characters", d.Name, MaxDescriptionLen)
		}

		switch d.Type {
		case modeltypes.JobParameterSingleSelect, modeltypes.JobParameterMultiSelect,
			modeltypes.JobParameterText, modeltypes.JobParameterList:
		default:
			verr.addf("parameter %s: type must be single_select, multi_select, text or list", d.Name)
			continue
		}

		if d.Type.IsSelect() {
			d.Options = normalizeOptions(d.Name, d.Options, verr)
		} else {
			d.Options = nil
		}

		if d.Type.IsMulti() {
			switch d.Format {
			case "":
				d.Format = DefaultFormat(d.Type)
			case modeltypes.JobParameterFormatComma, modeltypes.JobParameterFormatBullets:
			default:
				verr.addf("parameter %s: format must be comma or bullets", d.Name)
			}
		} else {
			d.Format = ""
		}
		out = append(out, d)
	}
	return out
}

func normalizeOptions(name string, options []string, verr *ValidationError) []string {
	out := make([]string, 0, len(options))
	for _, o := range options {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if slices.Contains(out, o) {
			verr.addf("parameter %s: option %q is listed more than once", name, o)
			continue
		}
		if utf8.RuneCountInString(o) > MaxValueChars {
			verr.addf("parameter %s: an option is longer than %d characters", name, MaxValueChars)
			continue
		}
		out = append(out, o)
	}
	if len(out) == 0 {
		verr.addf("parameter %s: a select needs at least one option", name)
	}
	if len(out) > MaxOptions {
		verr.addf("parameter %s: a select can have at most %d options", name, MaxOptions)
	}
	return out
}

func normalizeValues(defs []modeltypes.JobParameter, values modeltypes.JobParameterValues, verr *ValidationError) modeltypes.JobParameterValues {
	out := modeltypes.JobParameterValues{}
	for _, d := range defs {
		v, ok := values[d.Name]
		if !ok {
			continue
		}
		if v.List != d.Type.IsMulti() {
			if d.Type.IsMulti() {
				verr.addf("parameter %s: value must be a list", d.Name)
			} else {
				verr.addf("parameter %s: value must be a string", d.Name)
			}
			continue
		}
		switch d.Type {
		case modeltypes.JobParameterText:
			text := strings.TrimSpace(v.Text)
			if utf8.RuneCountInString(text) > MaxValueChars {
				verr.addf("parameter %s: value is longer than %d characters", d.Name, MaxValueChars)
				continue
			}
			if text != "" {
				out[d.Name] = modeltypes.TextValue(text)
			}
		case modeltypes.JobParameterSingleSelect:
			text := strings.TrimSpace(v.Text)
			if slices.Contains(d.Options, text) {
				out[d.Name] = modeltypes.TextValue(text)
			}
		case modeltypes.JobParameterMultiSelect:
			if items, ok := selectedOptions(d.Options, v.Items); ok {
				out[d.Name] = modeltypes.ListValue(items...)
			}
		case modeltypes.JobParameterList:
			items := make([]string, 0, len(v.Items))
			tooLong := false
			for _, item := range v.Items {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				if utf8.RuneCountInString(item) > MaxValueChars {
					tooLong = true
					continue
				}
				items = append(items, item)
			}
			if tooLong {
				verr.addf("parameter %s: an item is longer than %d characters", d.Name, MaxValueChars)
				continue
			}
			if len(items) > MaxListItems {
				verr.addf("parameter %s: a list can have at most %d items", d.Name, MaxListItems)
				continue
			}
			if len(items) > 0 {
				out[d.Name] = modeltypes.ListValue(items...)
			}
		}
	}
	return out
}

// selectedOptions returns the distinct selected options in the definition's order. The whole
// selection is invalid (false) if it is empty or picks any option no longer offered.
func selectedOptions(options, selected []string) ([]string, bool) {
	picked := map[string]bool{}
	for _, s := range selected {
		s = strings.TrimSpace(s)
		if !slices.Contains(options, s) {
			return nil, false
		}
		picked[s] = true
	}
	var out []string
	for _, o := range options {
		if picked[o] {
			out = append(out, o)
		}
	}
	return out, len(out) > 0
}

// IsSet reports whether v is a usable value for d (decision 2).
func IsSet(d modeltypes.JobParameter, v modeltypes.JobParameterValue, ok bool) bool {
	if !ok || v.List != d.Type.IsMulti() {
		return false
	}
	switch d.Type {
	case modeltypes.JobParameterText:
		return strings.TrimSpace(v.Text) != ""
	case modeltypes.JobParameterSingleSelect:
		return slices.Contains(d.Options, strings.TrimSpace(v.Text))
	case modeltypes.JobParameterMultiSelect:
		_, valid := selectedOptions(d.Options, v.Items)
		return valid
	case modeltypes.JobParameterList:
		return slices.ContainsFunc(v.Items, func(s string) bool { return strings.TrimSpace(s) != "" })
	}
	return false
}

// Missing returns the names of defined parameters without a usable value, in definition order.
func Missing(defs []modeltypes.JobParameter, values modeltypes.JobParameterValues) []string {
	var missing []string
	for _, d := range defs {
		v, ok := values[d.Name]
		if !IsSet(d, v, ok) {
			missing = append(missing, d.Name)
		}
	}
	return missing
}

// MissingError is returned by Render when a parameter has no value.
type MissingError struct{ Names []string }

func (e *MissingError) Error() string {
	return "job parameters have no value: " + strings.Join(e.Names, ", ")
}

// Render replaces each defined placeholder with its value (decision 3). Placeholders with no
// definition are left as written. It fails with *MissingError if any parameter is missing.
func Render(prompt string, defs []modeltypes.JobParameter, values modeltypes.JobParameterValues) (string, error) {
	if missing := Missing(defs, values); len(missing) > 0 {
		return "", &MissingError{Names: missing}
	}
	byName := make(map[string]modeltypes.JobParameter, len(defs))
	for _, d := range defs {
		byName[d.Name] = d
	}
	var b strings.Builder
	last := 0
	for _, m := range placeholderRE.FindAllStringSubmatchIndex(prompt, -1) {
		start, end := m[0], m[1]
		d, ok := byName[prompt[m[2]:m[3]]]
		if !ok {
			continue
		}
		before := prompt[last:start]
		last = end
		v := values[d.Name]
		bulletsMidLine := d.Type.IsMulti() && formatOf(d) == modeltypes.JobParameterFormatBullets && !atLineStart(prompt, start)
		if bulletsMidLine {
			before = strings.TrimRight(before, " \t")
		}
		b.WriteString(before)
		if !d.Type.IsMulti() {
			b.WriteString(strings.TrimSpace(v.Text))
			continue
		}
		items := cleanItems(v.Items)
		if formatOf(d) == modeltypes.JobParameterFormatComma {
			b.WriteString(strings.Join(items, ", "))
			continue
		}
		if bulletsMidLine {
			b.WriteString("\n")
		}
		for i, item := range items {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("- ")
			b.WriteString(item)
		}
		if !atLineEnd(prompt, end) {
			b.WriteString("\n")
			// The rest of the line starts the next line; drop the space that separated it.
			rest := prompt[last:]
			last += len(rest) - len(strings.TrimLeft(rest, " \t"))
		}
	}
	b.WriteString(prompt[last:])
	return b.String(), nil
}

func formatOf(d modeltypes.JobParameter) modeltypes.JobParameterFormat {
	if d.Format == "" {
		return DefaultFormat(d.Type)
	}
	return d.Format
}

func cleanItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// atLineStart reports whether only spaces or tabs precede pos on its line.
func atLineStart(s string, pos int) bool {
	lineStart := strings.LastIndexByte(s[:pos], '\n') + 1
	return strings.TrimLeft(s[lineStart:pos], " \t") == ""
}

// atLineEnd reports whether only spaces or tabs follow pos on its line.
func atLineEnd(s string, pos int) bool {
	rest := s[pos:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimRight(rest, " \t\r") == ""
}
