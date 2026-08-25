package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEstimateThreadCountPositive(t *testing.T) {
	count := estimateThreadCount(150, "openai", 12, len("x")*80)
	if count < 10 {
		t.Fatalf("expected auto thread count >= 10, got %d", count)
	}
}

func TestBuildOpenAIValidJSON(t *testing.T) {
	body := "hello"
	convs := buildOpenAI(2, 2, body)
	b, err := json.Marshal(convs)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip []openAIExport
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if len(roundTrip) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(roundTrip))
	}
}

func TestBuildAnthropicValidJSON(t *testing.T) {
	convs := buildAnthropic(1, 1, "hi")
	b, err := json.Marshal(convs)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip []anthropicExport
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if len(roundTrip[0].ChatMessages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(roundTrip[0].ChatMessages))
	}
}

func TestMainPackageCompiles(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	if err := os.WriteFile(out, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
}
