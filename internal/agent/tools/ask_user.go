package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// AskUserToolDescription tells the model when and how to ask (ADR 0x042).
const AskUserToolDescription = `Ask the person a question and wait for their answer, or ask their approval before you do something.

Use it when you need a decision or information only they have, or before an action they would want to OK first (sending something on their behalf, deleting or overwriting their data, spending money, anything hard to undo). Don't use it for things you can find out yourself, to confirm something they already told you, or for small choices with a sensible default: make the call and say what you chose.

Questions: send 1 to 4 in "questions". Give each question 2 to 4 short options when the likely answers are known (the person can always write their own answer instead), or no options for an open question. Put your recommended option first and add " (Recommended)" to its label. Set multi_select when several options can apply.

Approval: send "approval" instead of "questions", saying what you want to do and why.

The tool returns the person's answers. If they don't answer in time, it says so: then stop working and end your turn by asking in your reply.`

// MaxAskUserQuestions bounds one ask_user call.
const MaxAskUserQuestions = 4

const (
	maxAskUserOptions       = 6
	maxAskUserQuestionChars = 1000
	maxAskUserLabelChars    = 120
	maxAskUserDescChars     = 400
	maxAskUserHeaderChars   = 24
)

// AskUserToolSpec is offered only on interactive web turns (not in the catalog).
var AskUserToolSpec = FunctionToolSpec{
	Name:        "ask_user",
	Description: AskUserToolDescription,
	Properties: map[string]interface{}{
		"questions": map[string]interface{}{
			"type":        "array",
			"description": "1 to 4 questions to ask at once. Omit when asking for approval.",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"question": map[string]interface{}{
						"type":        "string",
						"description": "The question, as a full sentence ending in a question mark.",
					},
					"header": map[string]interface{}{
						"type":        "string",
						"description": "A very short label for the question (at most 2-3 words), e.g. \"Format\" or \"Deadline\".",
					},
					"options": map[string]interface{}{
						"type":        "array",
						"description": "2 to 4 choices; omit for an open question. The person can always answer in their own words instead.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"label": map[string]interface{}{
									"type":        "string",
									"description": "The choice, in 1 to 5 words.",
								},
								"description": map[string]interface{}{
									"type":        "string",
									"description": "What choosing it means, in one short sentence.",
								},
							},
							"required":             []string{"label"},
							"additionalProperties": false,
						},
					},
					"multi_select": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether more than one option can be picked.",
					},
				},
				"required":             []string{"question"},
				"additionalProperties": false,
			},
		},
		"approval": map[string]interface{}{
			"type":        "object",
			"description": "Ask permission for one action instead of asking questions.",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "What you want to do, in one short sentence (\"Send the draft to the team channel\").",
				},
				"details": map[string]interface{}{
					"type":        "string",
					"description": "Why, and what exactly will happen; what changes and whether it can be undone.",
				},
			},
			"required":             []string{"action"},
			"additionalProperties": false,
		},
	},
}

type askUserArgs struct {
	Questions []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
		MultiSelect bool `json:"multi_select"`
	} `json:"questions"`
	Approval *struct {
		Action  string `json:"action"`
		Details string `json:"details"`
	} `json:"approval"`
}

// ParseAskUserArgs validates ask_user input and returns the prompt to show.
func ParseAskUserArgs(raw []byte) (models.AgentPromptPayload, error) {
	var args askUserArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return models.AgentPromptPayload{}, fmt.Errorf("invalid ask_user arguments: %w", err)
	}
	if args.Approval != nil && len(args.Questions) > 0 {
		return models.AgentPromptPayload{}, fmt.Errorf("send either questions or approval, not both")
	}
	if args.Approval != nil {
		action := strings.TrimSpace(args.Approval.Action)
		if action == "" {
			return models.AgentPromptPayload{}, fmt.Errorf("approval.action is required")
		}
		return models.AgentPromptPayload{
			Kind:    models.AgentPromptKindApproval,
			Title:   TruncateRunes(action, maxAskUserQuestionChars),
			Message: TruncateRunes(strings.TrimSpace(args.Approval.Details), maxAskUserQuestionChars*2),
		}, nil
	}
	if len(args.Questions) == 0 {
		return models.AgentPromptPayload{}, fmt.Errorf("send at least one question, or an approval")
	}
	if len(args.Questions) > MaxAskUserQuestions {
		return models.AgentPromptPayload{}, fmt.Errorf("ask at most %d questions at once", MaxAskUserQuestions)
	}
	out := models.AgentPromptPayload{Kind: models.AgentPromptKindQuestions}
	for i, q := range args.Questions {
		text := strings.TrimSpace(q.Question)
		if text == "" {
			return models.AgentPromptPayload{}, fmt.Errorf("question %d is empty", i+1)
		}
		if len(q.Options) > maxAskUserOptions {
			return models.AgentPromptPayload{}, fmt.Errorf("question %d has more than %d options", i+1, maxAskUserOptions)
		}
		pq := models.AgentPromptQuestion{
			ID:          fmt.Sprintf("q%d", i+1),
			Header:      TruncateRunes(strings.TrimSpace(q.Header), maxAskUserHeaderChars),
			Question:    TruncateRunes(text, maxAskUserQuestionChars),
			MultiSelect: q.MultiSelect && len(q.Options) > 1,
		}
		seen := map[string]bool{}
		for j, o := range q.Options {
			label := TruncateRunes(strings.TrimSpace(o.Label), maxAskUserLabelChars)
			if label == "" {
				return models.AgentPromptPayload{}, fmt.Errorf("question %d option %d has no label", i+1, j+1)
			}
			if seen[label] {
				return models.AgentPromptPayload{}, fmt.Errorf("question %d has the option %q twice", i+1, label)
			}
			seen[label] = true
			pq.Options = append(pq.Options, models.AgentPromptOption{
				Label:       label,
				Description: TruncateRunes(strings.TrimSpace(o.Description), maxAskUserDescChars),
			})
		}
		out.Questions = append(out.Questions, pq)
	}
	return out, nil
}
