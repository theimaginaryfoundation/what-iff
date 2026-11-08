package modeltypes

import (
	"bytes"
	"encoding/json"
	"errors"
)

// JobParameterType is the kind of input a job parameter takes (ADR 0x044).
type JobParameterType string

const (
	// JobParameterSingleSelect picks one of the definition's options; the value is a string.
	JobParameterSingleSelect JobParameterType = "single_select"
	// JobParameterMultiSelect picks one or more of the options; the value is a list.
	JobParameterMultiSelect JobParameterType = "multi_select"
	// JobParameterText is one free-text value; the value is a string.
	JobParameterText JobParameterType = "text"
	// JobParameterList is any number of free-text items; the value is a list.
	JobParameterList JobParameterType = "list"
)

// IsMulti reports whether values of this type are lists.
func (t JobParameterType) IsMulti() bool {
	return t == JobParameterMultiSelect || t == JobParameterList
}

// IsSelect reports whether the type picks from fixed options.
func (t JobParameterType) IsSelect() bool {
	return t == JobParameterSingleSelect || t == JobParameterMultiSelect
}

// JobParameterFormat is how a multi-value parameter renders into the prompt.
type JobParameterFormat string

const (
	// JobParameterFormatComma joins the items with ", " on one line.
	JobParameterFormatComma JobParameterFormat = "comma"
	// JobParameterFormatBullets renders a Markdown bullet list, one "- item" per line.
	JobParameterFormatBullets JobParameterFormat = "bullets"
)

// JobParameter defines one `{{name}}` placeholder of a job prompt (ADR 0x044).
// Definitions travel with a shared job; values (JobParameterValues) never do.
type JobParameter struct {
	Name        string           `json:"name"`
	Label       string           `json:"label,omitempty"`
	Description string           `json:"description,omitempty"`
	Type        JobParameterType `json:"type"`
	// Options are the allowed values of a select type.
	Options []string `json:"options,omitempty"`
	// Format applies to multi_select and list only.
	Format JobParameterFormat `json:"format,omitempty"`
}

// JobParameterValue is one parameter's value: a string for text and single_select, a list for
// multi_select and list. It marshals as a JSON string or array accordingly.
type JobParameterValue struct {
	Text  string
	Items []string
	// List is true when the value is a list (Items), false for a string (Text).
	List bool
}

// TextValue builds a string value.
func TextValue(s string) JobParameterValue { return JobParameterValue{Text: s} }

// ListValue builds a list value.
func ListValue(items ...string) JobParameterValue {
	if items == nil {
		items = []string{}
	}
	return JobParameterValue{Items: items, List: true}
}

// MarshalJSON writes a string or an array.
func (v JobParameterValue) MarshalJSON() ([]byte, error) {
	if v.List {
		items := v.Items
		if items == nil {
			items = []string{}
		}
		return json.Marshal(items)
	}
	return json.Marshal(v.Text)
}

// UnmarshalJSON reads a string or an array of strings.
func (v *JobParameterValue) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")):
		*v = JobParameterValue{}
		return nil
	case trimmed[0] == '[':
		var items []string
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return errors.New("parameter value must be a string or a list of strings")
		}
		*v = ListValue(items...)
		return nil
	case trimmed[0] == '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		*v = TextValue(s)
		return nil
	default:
		return errors.New("parameter value must be a string or a list of strings")
	}
}

// JobParameterValues maps a parameter name to the owner's value for it.
type JobParameterValues map[string]JobParameterValue
