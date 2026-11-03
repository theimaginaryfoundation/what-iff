package models

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestJobRunContext(t *testing.T) {
	if JobRunFromContext(context.Background()) != nil {
		t.Fatal("no job run on a bare context")
	}
	if JobRunFromContext(WithJobRun(context.Background(), JobRun{Title: "no id"})) != nil {
		t.Fatal("a job run without a job id is none")
	}
	run := JobRun{AgentJobID: uuid.New(), Title: "Weekly meal planning", Prompt: "Plan."}
	got := JobRunFromContext(CopyJobRun(WithJobRun(context.Background(), run), context.Background()))
	if got == nil || *got != run {
		t.Fatalf("copy lost the job run: %+v", got)
	}
}
