package agent

import (
	"testing"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// TestBuildContextBreakdown_TotalsAndMetadata verifies the agent maps a ModelContext into a
// models.ContextBreakdown with a summed total, the display budget, and the chat's model /
// provider stamped on it.
func TestBuildContextBreakdown_TotalsAndMetadata(t *testing.T) {
	a := &Agent{}
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindSystemPrompt, provider.RoleDeveloper, "you are a helpful assistant", true)
	mc.AppendHistoryTurn(provider.RoleUser, "an earlier user question", nil, false)
	mc.AppendUserMessage(provider.RoleUser, "the current question", nil, false)

	chatCtx := &chatContext{model: "gpt-5", modelProvider: "openai"}

	got := a.buildContextBreakdown(chatCtx, mc, 0, nil)
	if got == nil {
		t.Fatal("expected a breakdown, got nil")
	}
	if got.BudgetTokens != checkpointMaxLastInputTokens {
		t.Errorf("expected budget %d, got %d", checkpointMaxLastInputTokens, got.BudgetTokens)
	}
	if got.Version != modeltypes.ContextBreakdownVersion {
		t.Errorf("expected version %d stamped, got %d", modeltypes.ContextBreakdownVersion, got.Version)
	}
	if got.Model != "gpt-5" || got.Provider != "openai" {
		t.Errorf("expected model/provider stamped, got %q/%q", got.Model, got.Provider)
	}
	if len(got.Segments) != 3 {
		t.Fatalf("expected 3 segment rows, got %d", len(got.Segments))
	}
	sum := 0
	for _, s := range got.Segments {
		sum += s.Tokens
	}
	if got.TotalTokens != sum {
		t.Errorf("expected total %d to equal segment sum %d", got.TotalTokens, sum)
	}
	if got.TotalTokens <= 0 {
		t.Errorf("expected positive total tokens, got %d", got.TotalTokens)
	}
	if got.CapturedAt.IsZero() {
		t.Error("expected CapturedAt to be set")
	}
}

// TestBuildContextBreakdown_NilInputs returns nil rather than panicking for empty/nil context.
func TestBuildContextBreakdown_NilInputs(t *testing.T) {
	a := &Agent{}
	if got := a.buildContextBreakdown(&chatContext{}, nil, 0, nil); got != nil {
		t.Errorf("expected nil for nil model context, got %+v", got)
	}
	if got := a.buildContextBreakdown(&chatContext{}, &provider.ModelContext{}, 0, nil); got != nil {
		t.Errorf("expected nil for empty model context, got %+v", got)
	}
}

func TestBuildContextBreakdown_UsesVendorUsageAndRemainder(t *testing.T) {
	a := &Agent{}
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindSystemPrompt, provider.RoleDeveloper, "base prompt", true)
	mc.SetAdditionalTokenEstimate(provider.SegmentKindToolDefinitions, 15)

	got := a.buildContextBreakdown(nil, mc, 100, nil)
	if got == nil || got.TotalTokens != 100 {
		t.Fatalf("expected billed total of 100, got %+v", got)
	}
	var toolDefinitions, remainder int
	for _, segment := range got.Segments {
		switch segment.Kind {
		case string(provider.SegmentKindToolDefinitions):
			toolDefinitions = segment.Tokens
		case "vendor_prompt_other":
			remainder = segment.Tokens
		}
	}
	if toolDefinitions != 15 {
		t.Errorf("expected 15 tool-definition tokens, got %d", toolDefinitions)
	}
	if remainder <= 0 {
		t.Errorf("expected positive vendor remainder, got %d", remainder)
	}
}

func TestBuildContextBreakdown_MergesCurrentToolCalls(t *testing.T) {
	a := &Agent{}
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindToolResult, provider.RoleDeveloper, "prior tool output", false)

	got := a.buildContextBreakdown(nil, mc, 0, []*models.ToolCall{{
		ToolInput:  `{"kind":"conversations","limit":200}`,
		ToolOutput: `{"kind":"conversations","count":200}`,
	}})
	if got == nil {
		t.Fatal("expected a breakdown")
	}
	var toolResult *modeltypes.ContextSegmentStat
	for i := range got.Segments {
		if got.Segments[i].Kind == string(provider.SegmentKindToolResult) {
			toolResult = &got.Segments[i]
			break
		}
	}
	if toolResult == nil {
		t.Fatal("expected current tool call material in the tool-result bucket")
	}
	if toolResult.Segments != 2 {
		t.Errorf("expected prior and current tool segments, got %d", toolResult.Segments)
	}
	if toolResult.Tokens <= 0 {
		t.Errorf("expected positive tool-result token estimate, got %d", toolResult.Tokens)
	}
}
