package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// turnOnlyToolSpecs are tools a turn offers outside the catalog: update_plan on Advanced turns
// (ADR 0x026), where the plan is what tells the loop whether work remains.
func turnOnlyToolSpecs(chatCtx *chatContext) []agenttools.FunctionToolSpec {
	if chatCtx == nil || !chatCtx.turnSettings.Advanced() {
		return nil
	}
	return []agenttools.FunctionToolSpec{agenttools.UpdatePlanToolSpec}
}

// updatePlanTool replaces the run's plan and publishes it to the live timeline.
func updatePlanTool(chatCtx *chatContext, input []byte) (string, error) {
	plan, err := agenttools.ParseUpdatePlanArgs(input)
	if err != nil {
		return "", err
	}
	// Only a changed plan counts as progress for the stall guard: re-sending the same plan is not
	// work.
	if !slices.Equal(plan, chatCtx.plan) {
		chatCtx.planVersion++
	}
	chatCtx.plan = plan
	chatCtx.toolProgress.SetPlan(plan)
	out, err := json.Marshal(map[string]any{
		"success":    true,
		"open_items": len(openPlanItems(plan)),
		"total":      len(plan),
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// openPlanItems returns the items still pending or in progress.
func openPlanItems(plan []models.PlanItem) []models.PlanItem {
	var open []models.PlanItem
	for _, item := range plan {
		if item.Status.Open() {
			open = append(open, item)
		}
	}
	return open
}

// formatPlan renders a plan as a checklist for the model.
func formatPlan(plan []models.PlanItem) string {
	var b strings.Builder
	for _, item := range plan {
		fmt.Fprintf(&b, "- [%s] %s\n", item.Status, item.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

// previousRunOpenPlan returns the open plan items of the thread's latest reply when it was an
// Advanced run that stopped before its work was done (a limit, Stop, a failure), with why it
// stopped. An Advanced turn is told about them (openPlanContext), so "continue" picks the work up
// instead of starting over.
func (a *Agent) previousRunOpenPlan(ctx context.Context, userID, chatID uuid.UUID) ([]models.PlanItem, models.AgentRunEnd) {
	if a.ds == nil {
		return nil, ""
	}
	origin := models.MessageOriginAssistant
	resp, err := a.ds.ListChatMessages(ctx, userID, chatID, 1, 1, models.ChatMessageFilters{Origin: &origin, SkipTotal: true})
	if err != nil {
		a.logger.Warn("advanced loop: failed to load the previous reply's plan", zap.Error(err))
		return nil, ""
	}
	if resp == nil || len(resp.Results) == 0 {
		return nil, ""
	}
	msg, _ := resp.Results[0].(*models.ChatMessage)
	return unfinishedRunPlan(msg)
}

// unfinishedRunPlan returns a reply's open plan items and end reason when it is an Advanced run
// that stopped before its work was done.
func unfinishedRunPlan(msg *models.ChatMessage) ([]models.PlanItem, models.AgentRunEnd) {
	if msg == nil || msg.AgentRun == nil || !msg.AgentRun.EndReason.Unfinished() {
		return nil, ""
	}
	return msg.AgentRun.OpenItems(), msg.AgentRun.EndReason
}

// openPlanContext tells an Advanced turn about the previous run's unfinished plan.
func openPlanContext(open []models.PlanItem, reason models.AgentRunEnd) string {
	return fmt.Sprintf("Your previous run in this conversation stopped before it finished (%s). These items of its plan were still open:\n%s\n\nIf the user asks you to continue, pick these up: put them back in your plan with update_plan and carry on from where the run stopped. Otherwise, follow the user's new request.", reason, formatPlan(open))
}
