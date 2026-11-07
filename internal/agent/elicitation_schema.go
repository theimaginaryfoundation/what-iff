package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"unicode/utf8"
)

// MCP elicitation forms (ADR 0x042): a server's requestedSchema is a flat object of primitive
// fields (string, number, integer, boolean, single-select enums, and multi-select arrays of enum
// strings). The client renders it as a form; the answer is checked against it here before it is
// sent back, as the spec asks clients to do.

// elicitationSchema is the subset of JSON Schema elicitation allows.
type elicitationSchema struct {
	Type       string                         `json:"type"`
	Properties map[string]elicitationProperty `json:"properties"`
	Required   []string                       `json:"required"`
}

type elicitationProperty struct {
	Type        string             `json:"type"`
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description,omitempty"`
	Enum        []any              `json:"enum,omitempty"`
	OneOf       []elicitationConst `json:"oneOf,omitempty"`
	Items       *elicitationItems  `json:"items,omitempty"`
	MinLength   *int               `json:"minLength,omitempty"`
	MaxLength   *int               `json:"maxLength,omitempty"`
	Pattern     string             `json:"pattern,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty"`
	MinItems    *int               `json:"minItems,omitempty"`
	MaxItems    *int               `json:"maxItems,omitempty"`
}

type elicitationConst struct {
	Const any    `json:"const"`
	Title string `json:"title,omitempty"`
}

type elicitationItems struct {
	Type  string             `json:"type,omitempty"`
	Enum  []any              `json:"enum,omitempty"`
	AnyOf []elicitationConst `json:"anyOf,omitempty"`
}

// parseElicitationSchema decodes and checks a requestedSchema: a flat object whose properties all
// have a supported type.
func parseElicitationSchema(raw json.RawMessage) (*elicitationSchema, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("the form has no schema")
	}
	var s elicitationSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("the form's schema is not valid: %w", err)
	}
	if s.Type != "" && s.Type != "object" {
		return nil, fmt.Errorf("the form's schema must be an object")
	}
	for name, p := range s.Properties {
		switch p.Type {
		case "string", "number", "integer", "boolean":
		case "array":
			if p.Items == nil || (len(p.Items.Enum) == 0 && len(p.Items.AnyOf) == 0) {
				return nil, fmt.Errorf("field %q: only arrays of enumerated strings are supported", name)
			}
		default:
			return nil, fmt.Errorf("field %q has an unsupported type %q", name, p.Type)
		}
	}
	return &s, nil
}

// validateElicitationContent checks a form answer against its schema and returns only the schema's
// fields, with numbers that should be integers made integers.
func validateElicitationContent(rawSchema json.RawMessage, content map[string]any) (map[string]any, error) {
	schema, err := parseElicitationSchema(rawSchema)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for name, prop := range schema.Properties {
		v, ok := content[name]
		if !ok || v == nil {
			continue
		}
		clean, err := checkElicitationValue(name, prop, v)
		if err != nil {
			return nil, err
		}
		out[name] = clean
	}
	for _, name := range schema.Required {
		if _, ok := out[name]; !ok {
			label := name
			if p, ok := schema.Properties[name]; ok && p.Title != "" {
				label = p.Title
			}
			return nil, fmt.Errorf("%s is required", label)
		}
	}
	return out, nil
}

func checkElicitationValue(name string, p elicitationProperty, v any) (any, error) {
	switch p.Type {
	case "string":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be text", name)
		}
		if allowed := elicitationChoices(p.Enum, p.OneOf); len(allowed) > 0 && !slices.Contains(allowed, s) {
			return nil, fmt.Errorf("%s: %q is not one of the choices", name, s)
		}
		n := utf8.RuneCountInString(s)
		if p.MinLength != nil && n < *p.MinLength {
			return nil, fmt.Errorf("%s must be at least %d characters", name, *p.MinLength)
		}
		if p.MaxLength != nil && n > *p.MaxLength {
			return nil, fmt.Errorf("%s must be at most %d characters", name, *p.MaxLength)
		}
		if p.Pattern != "" {
			if re, err := regexp.Compile(p.Pattern); err == nil && !re.MatchString(s) {
				return nil, fmt.Errorf("%s is not in the expected format", name)
			}
		}
		if len([]rune(s)) > maxPromptAnswerChars {
			return nil, fmt.Errorf("%s is too long", name)
		}
		return s, nil
	case "number", "integer":
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("%s must be a number", name)
		}
		if p.Type == "integer" && f != math.Trunc(f) {
			return nil, fmt.Errorf("%s must be a whole number", name)
		}
		if p.Minimum != nil && f < *p.Minimum {
			return nil, fmt.Errorf("%s must be at least %v", name, *p.Minimum)
		}
		if p.Maximum != nil && f > *p.Maximum {
			return nil, fmt.Errorf("%s must be at most %v", name, *p.Maximum)
		}
		if p.Type == "integer" {
			return int64(f), nil
		}
		return f, nil
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("%s must be true or false", name)
		}
		return b, nil
	case "array":
		list, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%s must be a list", name)
		}
		allowed := elicitationChoices(p.Items.Enum, p.Items.AnyOf)
		picked := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok || !slices.Contains(allowed, s) {
				return nil, fmt.Errorf("%s: %v is not one of the choices", name, item)
			}
			picked = append(picked, s)
		}
		if p.MinItems != nil && len(picked) < *p.MinItems {
			return nil, fmt.Errorf("%s needs at least %d choices", name, *p.MinItems)
		}
		if p.MaxItems != nil && len(picked) > *p.MaxItems {
			return nil, fmt.Errorf("%s takes at most %d choices", name, *p.MaxItems)
		}
		return picked, nil
	}
	return nil, fmt.Errorf("%s has an unsupported type", name)
}

// elicitationChoices lists the allowed string values of an enum or a oneOf/anyOf of consts.
func elicitationChoices(enum []any, consts []elicitationConst) []string {
	var out []string
	for _, e := range enum {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	for _, c := range consts {
		if s, ok := c.Const.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
