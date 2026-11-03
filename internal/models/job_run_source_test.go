package models

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestJobRunSource(t *testing.T) {
	if _, ok := JobRunSourceFromContext(context.Background()); ok {
		t.Fatal("no source on a bare context")
	}
	if _, ok := JobRunSourceFromContext(WithJobRunSource(context.Background(), JobRunSource{Title: "no id"})); ok {
		t.Fatal("a source without a job id is no source")
	}
	src := JobRunSource{AgentJobID: uuid.New(), Title: "Weekly meal planning"}
	got, ok := JobRunSourceFromContext(CopyJobRunSource(WithJobRunSource(context.Background(), src), context.Background()))
	if !ok || got != src {
		t.Fatalf("copy lost the source: %+v, %v", got, ok)
	}
}
