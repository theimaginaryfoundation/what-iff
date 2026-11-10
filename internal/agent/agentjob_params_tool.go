package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agentjobs/params"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// jobParamValueArg is one parameter value in create_agent_job / update_agent_job (ADR 0x044).
// The tools take values as a list rather than a name-keyed object, since strict tool schemas
// can't describe an object with free-form keys.
type jobParamValueArg struct {
	Name  string    `json:"name"`
	Value *string   `json:"value,omitempty"`
	Items *[]string `json:"items,omitempty"`
}

// mergeJobParamValues applies the tool's values over base (nil for a new job). A value with
// neither value nor items, or with an empty one, clears that parameter. The result is checked
// against the definitions by the datastore (params.Prepare).
func mergeJobParamValues(base modeltypes.JobParameterValues, args []jobParamValueArg) (modeltypes.JobParameterValues, error) {
	out := modeltypes.JobParameterValues{}
	for name, v := range base {
		out[name] = v
	}
	for _, a := range args {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return nil, errors.New("each parameter value needs a name")
		}
		switch {
		case a.Value != nil && a.Items != nil:
			return nil, fmt.Errorf("parameter value %s: pass value or items, not both", name)
		case a.Items != nil && len(*a.Items) > 0:
			out[name] = modeltypes.ListValue(*a.Items...)
		case a.Value != nil && strings.TrimSpace(*a.Value) != "":
			out[name] = modeltypes.TextValue(*a.Value)
		default:
			delete(out, name)
		}
	}
	return out, nil
}

// jobParamsToolError turns a job-parameter error into the message the model gets, or "" when
// err is not one.
func jobParamsToolError(err error) string {
	var verr *params.ValidationError
	if errors.As(err, &verr) {
		return "invalid job parameters: " + verr.Error()
	}
	var missing *params.MissingError
	if errors.As(err, &missing) || errors.Is(err, datastore.ErrAgentJobNeedsValues) {
		names := ""
		if missing != nil {
			names = strings.Join(missing.Names, ", ")
		}
		return "the job can't be active until every parameter has a value; missing: " + names
	}
	return ""
}

// missingParamsNote is the note a tool result carries when the job waits for values.
func missingParamsNote(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	return "The job can't run until these parameters have values: " + strings.Join(missing, ", ") +
		". Ask the user for them, then set them with update_agent_job (parameter_values)."
}
