package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// UpdatePlanToolDescription tells the model how the Advanced loop uses its plan (ADR 0x026).
const UpdatePlanToolDescription = `Record or update your plan for this request as a checklist.

Use it for work that takes several steps: write the steps first, mark one in_progress while you work on it, and mark each completed (or cancelled, if it turned out unnecessary) as you go. Send the whole list every time; it replaces the previous one.

You keep working until no item is pending or in_progress. When everything is done, reply to the user with your final answer and call no more tools. Skip the plan for a simple request you can answer directly.`

// MaxPlanItems bounds a plan so a runaway model cannot grow the progress payload without limit.
const MaxPlanItems = 50

// maxPlanItemChars bounds one item's text.
const maxPlanItemChars = 500

// UpdatePlanToolSpec is offered only on Advanced-loop turns (it is not in the catalog).
var UpdatePlanToolSpec = FunctionToolSpec{
	Name:        "update_plan",
	Description: UpdatePlanToolDescription,
	Properties: map[string]interface{}{
		"items": map[string]interface{}{
			"type":        "array",
			"description": "The full plan, in order.",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "What this step does, in a short sentence.",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
						"description": "The step's state.",
					},
				},
				"required":             []string{"content", "status"},
				"additionalProperties": false,
			},
		},
	},
	Required: []string{"items"},
}

type updatePlanArgs struct {
	Items []models.PlanItem `json:"items"`
}

// ParseUpdatePlanArgs validates update_plan input and returns the new plan.
func ParseUpdatePlanArgs(raw []byte) ([]models.PlanItem, error) {
	var args updatePlanArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid update_plan arguments: %w", err)
	}
	if len(args.Items) > MaxPlanItems {
		return nil, fmt.Errorf("a plan can have at most %d items", MaxPlanItems)
	}
	out := make([]models.PlanItem, 0, len(args.Items))
	for i, item := range args.Items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			return nil, fmt.Errorf("plan item %d has no content", i+1)
		}
		if r := []rune(content); len(r) > maxPlanItemChars {
			content = string(r[:maxPlanItemChars])
		}
		status := models.PlanItemStatus(strings.ToLower(strings.TrimSpace(string(item.Status))))
		if !status.Valid() {
			return nil, fmt.Errorf("plan item %d has an unknown status %q (use pending, in_progress, completed or cancelled)", i+1, item.Status)
		}
		out = append(out, models.PlanItem{Content: content, Status: status})
	}
	return out, nil
}
