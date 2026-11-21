package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestVisibleMemoriesMirrorsRetrievalScope(t *testing.T) {
	chat, otherChat := uuid.New(), uuid.New()
	persona, otherPersona := uuid.New(), uuid.New()
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mems := []evalMemory{
		{Scope: scopeChat, ChatID: chat, CreatedAt: t0},                      // 0: own chat
		{Scope: scopeChat, ChatID: otherChat, CreatedAt: t0},                 // 1: other chat
		{Scope: scopeUser, CreatedAt: t0},                                    // 2: unpinned global
		{Scope: scopeUser, PinnedPersonalityID: persona, CreatedAt: t0},      // 3: pinned to this personality
		{Scope: scopeUser, PinnedPersonalityID: otherPersona, CreatedAt: t0}, // 4: pinned elsewhere
		{Scope: scopeUser, CreatedAt: t0.Add(48 * time.Hour)},                // 5: created after the turn
		{Scope: scopeChat, ChatID: chat, CreatedAt: t0.Add(24 * time.Hour)},  // 6: same instant as the turn
	}
	turn := evalTurn{ChatID: chat, PersonalityID: persona, CreatedAt: t0.Add(24 * time.Hour)}

	if got, want := visibleMemories(turn, mems, true), []int{0, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("time travel: got %v, want %v", got, want)
	}
	if got, want := visibleMemories(turn, mems, false), []int{0, 2, 3, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Errorf("no time travel: got %v, want %v", got, want)
	}
}

func TestPicksStopsAtLimitAndThreshold(t *testing.T) {
	order := []ranked{{0, 0.1}, {1, 0.2}, {2, 0.3}, {3, 0.4}, {4, 0.5}, {5, 0.6}, {6, 0.7}}
	if got := picks(order, 0.35); len(got) != 3 {
		t.Errorf("threshold: got %d picks, want 3", len(got))
	}
	if got := picks(order, 2); len(got) != retrievalLimit {
		t.Errorf("limit: got %d picks, want %d", len(got), retrievalLimit)
	}
}

func TestCompareTurnRanksAndOverlap(t *testing.T) {
	// Baseline picks 0,1,2; the candidate reorders them, swaps 2 for 3, and ranks 2 at 4.
	base := []ranked{{0, 0.1}, {1, 0.2}, {2, 0.3}, {3, 1.5}}
	cand := []ranked{{1, 0.1}, {0, 0.2}, {3, 0.3}, {2, 0.9}}
	c := compareTurn(base, cand, 1.2, 0.5)

	if c.Overlap != 2 {
		t.Errorf("overlap = %d, want 2", c.Overlap)
	}
	if want := []int{2, 1, 4}; !reflect.DeepEqual(c.BaseRankInCand, want) {
		t.Errorf("BaseRankInCand = %v, want %v", c.BaseRankInCand, want)
	}
	if want := []int{2, 1, 4}; !reflect.DeepEqual(c.CandRankInBase, want) {
		t.Errorf("CandRankInBase = %v, want %v", c.CandRankInBase, want)
	}
	if c.InNeighborhood[10] != 3 {
		t.Errorf("in top 10 = %d, want 3", c.InNeighborhood[10])
	}
	if c.BaseSimOfCand >= c.BaseSimOfBase {
		t.Errorf("candidate picked a baseline-distant memory, so its baseline similarity (%f) should be below the baseline's (%f)", c.BaseSimOfCand, c.BaseSimOfBase)
	}
}

func TestCalibrateThresholdMatchesBaselineCount(t *testing.T) {
	// Two turns whose top distances are 0.1..0.5 and 0.6..1.0; a baseline mean of 3 picks/turn
	// needs six of the ten distances, so the cutoff lands on the sixth (0.6).
	mk := func(start float64) []ranked {
		var o []ranked
		for i := 0; i < 5; i++ {
			o = append(o, ranked{i, start + float64(i)*0.1})
		}
		return o
	}
	got := calibrateThreshold([][]ranked{mk(0.1), mk(0.6)}, 3)
	if got < 0.599 || got > 0.601 {
		t.Errorf("threshold = %f, want 0.6", got)
	}
}

func TestCosineL2RoundTrip(t *testing.T) {
	for _, c := range []float64{-1, 0, 0.28, 0.5, 1} {
		if got := cosineFromL2(l2FromCosine(c)); got < c-1e-9 || got > c+1e-9 {
			t.Errorf("round trip %f → %f", c, got)
		}
	}
}

func TestLoadExport(t *testing.T) {
	chat, persona := uuid.New(), uuid.New()
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	var memZip bytes.Buffer
	mz := zip.NewWriter(&memZip)
	writeJSONL(t, mz, "chat.json", models.MemoryRecord{ID: uuid.New(), Content: "chat fact", ChatID: &chat, CreatedAt: t0})
	writeJSONL(t, mz, "user.json", models.MemoryRecord{ID: uuid.New(), Content: "global fact", CreatedAt: t0})
	writeJSONL(t, mz, "personality-"+persona.String()+".json", models.MemoryRecord{ID: uuid.New(), Content: "pinned fact", CreatedAt: t0})
	if err := mz.Close(); err != nil {
		t.Fatal(err)
	}

	convs := []map[string]any{{
		"uuid": chat.String(), "name": "a chat", "whatiff_personality_id": persona.String(),
		"chat_messages": []map[string]any{
			{"sender": "human", "text": "hello there", "created_at": t0},
			{"sender": "assistant", "text": "hi", "created_at": t0},
			{"sender": "human", "text": "  ", "created_at": t0},
		},
	}}
	path := filepath.Join(t.TempDir(), "export.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("conversations.json")
	_ = json.NewEncoder(w).Encode(convs)
	w, _ = zw.Create("memories.zip")
	_, _ = w.Write(memZip.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	exp, err := loadExport(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(exp.Turns) != 1 || exp.Turns[0].Text != "hello there" || exp.Turns[0].PersonalityID != persona {
		t.Errorf("turns = %+v", exp.Turns)
	}
	if len(exp.Memories) != 3 {
		t.Fatalf("got %d memories, want 3", len(exp.Memories))
	}
	byContent := map[string]evalMemory{}
	for _, m := range exp.Memories {
		byContent[m.Content] = m
	}
	if m := byContent["chat fact"]; m.Scope != scopeChat || m.ChatID != chat {
		t.Errorf("chat memory = %+v", m)
	}
	if m := byContent["pinned fact"]; m.Scope != scopeUser || m.PinnedPersonalityID != persona {
		t.Errorf("pinned memory = %+v", m)
	}
	if m := byContent["global fact"]; m.Scope != scopeUser || m.PinnedPersonalityID != uuid.Nil {
		t.Errorf("global memory = %+v", m)
	}
}

func writeJSONL(t *testing.T, zw *zip.Writer, name string, recs ...models.MemoryRecord) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(w)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
}
