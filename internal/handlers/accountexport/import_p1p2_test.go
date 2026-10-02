package accountexport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/exporter"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func makeSummaryCandidates(n int) []summaryImportCandidate {
	cs := make([]summaryImportCandidate, n)
	for i := range cs {
		cs[i] = summaryImportCandidate{chatID: uuid.New(), summary: "s"}
	}
	return cs
}

func vectorsFor(inputs []string) [][]float32 {
	out := make([][]float32, len(inputs))
	for i := range out {
		out[i] = []float32{0.1}
	}
	return out
}

func TestIndexSummaryMemories_BatchesAndUpsertsAll(t *testing.T) {
	var embedCalls, upsertCalls, maxBatch int
	embed := func(_ context.Context, in []string) ([][]float32, error) {
		embedCalls++
		if len(in) > maxBatch {
			maxBatch = len(in)
		}
		return vectorsFor(in), nil
	}
	upsert := func(_ context.Context, _, _ uuid.UUID, _ string, _ []float32) error { upsertCalls++; return nil }

	// 450 candidates -> 3 batches (200 + 200 + 50), 450 upserts, one request per batch (not per item).
	if failed := indexSummaryMemories(context.Background(), uuid.New(), makeSummaryCandidates(450), embed, upsert, zap.NewNop()); failed {
		t.Fatal("unexpected partial failure")
	}
	if embedCalls != 3 {
		t.Errorf("embed calls = %d, want 3", embedCalls)
	}
	if maxBatch != summaryEmbedBatchSize {
		t.Errorf("max batch = %d, want %d", maxBatch, summaryEmbedBatchSize)
	}
	if upsertCalls != 450 {
		t.Errorf("upsert calls = %d, want 450", upsertCalls)
	}
}

func TestIndexSummaryMemories_CapsAndReportsPartial(t *testing.T) {
	var upsertCalls int
	embed := func(_ context.Context, in []string) ([][]float32, error) { return vectorsFor(in), nil }
	upsert := func(_ context.Context, _, _ uuid.UUID, _ string, _ []float32) error { upsertCalls++; return nil }

	failed := indexSummaryMemories(context.Background(), uuid.New(), makeSummaryCandidates(maxSummariesIndexed+5), embed, upsert, zap.NewNop())
	if !failed {
		t.Error("want partial failure=true when the cap trims candidates")
	}
	if upsertCalls != maxSummariesIndexed {
		t.Errorf("upsert calls = %d, want %d (cap)", upsertCalls, maxSummariesIndexed)
	}
}

func TestIndexSummaryMemories_StopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var embedCalls int
	embed := func(_ context.Context, in []string) ([][]float32, error) { embedCalls++; return vectorsFor(in), nil }
	upsert := func(_ context.Context, _, _ uuid.UUID, _ string, _ []float32) error { return nil }

	if failed := indexSummaryMemories(ctx, uuid.New(), makeSummaryCandidates(10), embed, upsert, zap.NewNop()); !failed {
		t.Error("cancelled context should report failure=true")
	}
	if embedCalls != 0 {
		t.Errorf("embed calls after cancel = %d, want 0", embedCalls)
	}
}

func TestIndexSummaryMemories_EmbedErrorContinues(t *testing.T) {
	embed := func(_ context.Context, _ []string) ([][]float32, error) { return nil, errors.New("boom") }
	var upsertCalls int
	upsert := func(_ context.Context, _, _ uuid.UUID, _ string, _ []float32) error { upsertCalls++; return nil }

	if failed := indexSummaryMemories(context.Background(), uuid.New(), makeSummaryCandidates(10), embed, upsert, zap.NewNop()); !failed {
		t.Error("embed error should report failure=true")
	}
	if upsertCalls != 0 {
		t.Errorf("upsert calls after embed error = %d, want 0", upsertCalls)
	}
}

func TestSummaryImportCandidates_SelectsOnlyResolvedNonEmpty(t *testing.T) {
	src1, src2 := uuid.New(), uuid.New()
	dst1 := uuid.New()
	chatIDs := map[uuid.UUID]uuid.UUID{src1: dst1} // src2 deliberately unresolved

	parsed := []exporter.ParsedConversation{
		{UUID: src1.String(), WhatiffCheckpointSummary: "a summary"}, // included
		{UUID: src1.String(), WhatiffCheckpointSummary: "   "},       // empty summary -> skip
		{UUID: src2.String(), WhatiffCheckpointSummary: "unmapped"},  // no destination -> skip
		{UUID: "not-a-uuid", WhatiffCheckpointSummary: "bad id"},     // invalid uuid -> skip
	}
	got := summaryImportCandidates(parsed, chatIDs)
	if len(got) != 1 || got[0].chatID != dst1 || got[0].summary != "a summary" {
		t.Fatalf("candidates = %+v, want exactly one {%s, \"a summary\"}", got, dst1)
	}
}

func TestParseImportSelection(t *testing.T) {
	t.Run("empty is nil", func(t *testing.T) {
		sel, err := parseImportSelection("  ")
		if err != nil || sel != nil {
			t.Fatalf("got sel=%v err=%v, want nil,nil", sel, err)
		}
	})

	t.Run("valid parses", func(t *testing.T) {
		id := uuid.New()
		raw, _ := json.Marshal(models.AccountImportSelection{PersonalityIDs: []uuid.UUID{id}, IncludeMemories: true})
		sel, err := parseImportSelection(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		if sel == nil || len(sel.PersonalityIDs) != 1 || sel.PersonalityIDs[0] != id || !sel.IncludeMemories {
			t.Fatalf("unexpected selection: %+v", sel)
		}
	})

	t.Run("oversized rejected before parse", func(t *testing.T) {
		if _, err := parseImportSelection(strings.Repeat("a", maxSelectionBytes+1)); !errors.Is(err, errSelectionTooLarge) {
			t.Fatalf("want errSelectionTooLarge, got %v", err)
		}
	})

	t.Run("malformed rejected", func(t *testing.T) {
		if _, err := parseImportSelection("{not json"); !errors.Is(err, errSelectionInvalidJSON) {
			t.Fatalf("want errSelectionInvalidJSON, got %v", err)
		}
	})

	t.Run("too many ids rejected", func(t *testing.T) {
		ids := make([]uuid.UUID, maxSelectionIDs+1)
		for i := range ids {
			ids[i] = uuid.New()
		}
		raw, _ := json.Marshal(models.AccountImportSelection{ConversationIDs: ids})
		if _, err := parseImportSelection(string(raw)); !errors.Is(err, errSelectionTooManyItems) {
			t.Fatalf("want errSelectionTooManyItems, got %v", err)
		}
	})
}
