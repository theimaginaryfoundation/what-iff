package metering

import (
	"context"
	"testing"
)

func TestAgentLoopContext(t *testing.T) {
	if got := AgentLoopFromContext(WithAgentLoop(context.Background(), "advanced")); got != "advanced" {
		t.Fatalf("got %q", got)
	}
	if got := AgentLoopFromContext(WithAgentLoop(context.Background(), "")); got != "" {
		t.Fatalf("empty loop should leave the context alone, got %q", got)
	}
	//nolint:staticcheck // a nil context is exactly what this guards against
	if got := AgentLoopFromContext(nil); got != "" {
		t.Fatalf("nil context: %q", got)
	}
	//nolint:staticcheck
	if AgentLoopFromContext(WithAgentLoop(nil, "advanced")) != "advanced" {
		t.Fatal("WithAgentLoop should accept a nil parent")
	}
}

func TestPendingUsageContext(t *testing.T) {
	if _, ok := PendingUsageFromContext(context.Background()); ok {
		t.Fatal("no pending usage on a plain context")
	}
	//nolint:staticcheck // a nil context is tolerated, as by the other helpers
	if _, ok := PendingUsageFromContext(nil); ok {
		t.Fatal("no pending usage on a nil context")
	}
	u := Usage{Model: "m", Calls: []CallUsage{{InputTokens: 5}}}
	//nolint:staticcheck // a nil context is tolerated
	got, ok := PendingUsageFromContext(WithPendingUsage(nil, u))
	if !ok || got.Model != "m" || len(got.Calls) != 1 {
		t.Fatalf("PendingUsageFromContext = %+v, %v", got, ok)
	}
}
