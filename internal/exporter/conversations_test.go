package exporter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestConversationsRoundTripWhatiffContinuationState(t *testing.T) {
	chatID, personalityID := uuid.New(), uuid.New()
	checkpointAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	data, err := BuildConversationsJSON([]ConversationInput{{
		ID:                         chatID,
		Title:                      "Portable thread",
		CreatedAt:                  checkpointAt,
		PersonalityID:              &personalityID,
		CheckpointSummary:          "The user is implementing account portability.",
		CheckpointUserMessageCount: 7,
		LastCheckpointAt:           &checkpointAt,
		DisabledTools:              []string{"web_search"},
		Tags:                       []string{"portable"},
		IsFavorite:                 true,
		IsAutoMood:                 false,
		Messages: []MessageInput{{
			Origin: models.MessageOriginUser,
			Text:   "Continue this thread.",
			SentAt: checkpointAt,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseConversations(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed conversations = %d, want 1", len(parsed))
	}
	got := parsed[0]
	if got.WhatiffPersonalityID == nil || *got.WhatiffPersonalityID != personalityID {
		t.Fatalf("personality ID = %v, want %v", got.WhatiffPersonalityID, personalityID)
	}
	if got.WhatiffCheckpointSummary == "" || got.WhatiffCheckpointUserMessageCnt != 7 || got.WhatiffLastCheckpointAt == nil {
		t.Fatalf("checkpoint state was not preserved: %#v", got)
	}
	if len(got.WhatiffDisabledTools) != 1 || got.WhatiffDisabledTools[0] != "web_search" || !got.WhatiffIsFavorite || got.WhatiffIsAutoMood {
		t.Fatalf("continuation state was not preserved: %#v", got)
	}
}

func TestConversationsRoundTripSandboxed(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	msg := []MessageInput{{Origin: models.MessageOriginUser, Text: "hi", SentAt: now}}
	data, err := BuildConversationsJSON([]ConversationInput{
		{ID: uuid.New(), Title: "sandboxed thread", CreatedAt: now, Messages: msg, ContextScope: "sandbox"},
		{ID: uuid.New(), Title: "ordinary thread", CreatedAt: now.Add(time.Second), Messages: msg},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseConversations(data)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range parsed {
		got[c.Name] = c.WhatiffContextScope == "sandbox"
	}
	// A sandboxed chat round-trips; an ordinary one (and an export from before the flag existed)
	// parses to false, which is not sandboxed on import.
	want := map[string]bool{"sandboxed thread": true, "ordinary thread": false}
	for name, sandboxed := range want {
		if got[name] != sandboxed {
			t.Errorf("%s: sandboxed = %v, want %v", name, got[name], sandboxed)
		}
	}
	if strings.Contains(string(data), "whatiff_sandboxed\":false") {
		t.Errorf("the default flag should not be written to the bundle")
	}
}

// The streamed conversations.json is byte-identical to encoding the whole array at once (the
// format exports had before streaming), including the empty and all-dropped cases.
func TestConversationsWriterMatchesWholeArrayEncoding(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	personalityID := uuid.New()
	msg := func(origin models.MessageOrigin, text string) MessageInput {
		return MessageInput{Origin: origin, Text: text, SentAt: base}
	}
	full := ConversationInput{
		ID: uuid.New(), Title: "<b>tags</b> & \"quotes\"", CreatedAt: base, PersonalityID: &personalityID,
		LastCheckpointAt: &base, DisabledTools: []string{"a", "b"}, Tags: []string{}, IsAutoMood: true,
		Messages: []MessageInput{
			msg(models.MessageOriginUser, "line one\nline <two> & three"),
			msg(models.MessageOriginAssistant, "{\"nested\": [1, 2]}"),
		},
	}
	dropped := ConversationInput{ID: uuid.New(), Title: "only empty", CreatedAt: base.Add(time.Second),
		Messages: []MessageInput{msg(models.MessageOriginUser, "")}}
	plain := ConversationInput{ID: uuid.New(), Title: "plain", CreatedAt: base.Add(2 * time.Second),
		Messages: []MessageInput{msg(models.MessageOriginUser, "hi")}, ContextScope: "sandbox"}

	for name, convs := range map[string][]ConversationInput{
		"none":        nil,
		"all dropped": {dropped},
		"one":         {full},
		"several":     {full, dropped, plain},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := BuildConversationsJSON(convs)
			if err != nil {
				t.Fatal(err)
			}
			if want := wholeArrayConversationsJSON(t, convs); string(got) != want {
				t.Fatalf("streamed output differs:\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

// wholeArrayConversationsJSON is the pre-streaming encoder: project everything, then encode the
// slice in one call.
func wholeArrayConversationsJSON(t *testing.T, convs []ConversationInput) string {
	t.Helper()
	out := make([]exportConversation, 0, len(convs))
	for _, c := range convs {
		if conv, ok := projectConversation(c); ok {
			out = append(out, conv)
		}
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// A thread's turn settings (ADR 0x026) round-trip; a thread without them writes nothing new.
func TestConversationsRoundTripTurnSettings(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	msg := []MessageInput{{Origin: models.MessageOriginUser, Text: "hi", SentAt: now}}
	data, err := BuildConversationsJSON([]ConversationInput{
		{ID: uuid.New(), Title: "set", CreatedAt: now, Messages: msg, ReasoningEffort: "high", AgentLoop: "advanced"},
		{ID: uuid.New(), Title: "unset", CreatedAt: now.Add(time.Second), Messages: msg},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseConversations(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range parsed {
		switch c.Name {
		case "set":
			if c.WhatiffReasoningEffort != "high" || c.WhatiffAgentLoop != "advanced" {
				t.Errorf("set: got %q/%q", c.WhatiffReasoningEffort, c.WhatiffAgentLoop)
			}
		case "unset":
			if c.WhatiffReasoningEffort != "" || c.WhatiffAgentLoop != "" {
				t.Errorf("unset: got %q/%q", c.WhatiffReasoningEffort, c.WhatiffAgentLoop)
			}
		}
	}
	if strings.Count(string(data), "whatiff_reasoning_effort") != 1 {
		t.Errorf("only the thread with a setting should carry it")
	}
}
