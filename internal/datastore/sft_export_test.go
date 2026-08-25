package datastore

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
)

type sftTestLine struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func decodeSFTLines(t *testing.T, data []byte) []sftTestLine {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	out := make([]sftTestLine, 0, len(lines))
	for _, line := range lines {
		var item sftTestLine
		require.NoError(t, json.Unmarshal(line, &item))
		out = append(out, item)
	}
	return out
}

func TestProcessSFTMessageBatch_ExportsPairWithResolvedPersonality(t *testing.T) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)

	stats := &SFTExportStats{}
	personalities := map[string]*ent.Personality{
		"alpha": {
			Name:         "Alpha",
			SystemPrompt: "alpha system",
			Scratchpad:   "alpha scratch",
		},
	}
	messages := []*ent.ChatMessage{
		{Origin: chatmessage.OriginUser, Message: "hello"},
		{Origin: chatmessage.OriginAssistant, Message: "hi there", GenerationPersonality: "ALPHA"},
	}

	pending, err := processSFTMessageBatch(encoder, messages, personalities, nil, nil, stats)
	require.NoError(t, err)
	require.Nil(t, pending)
	require.Equal(t, 1, stats.ExportedPairs)
	require.Equal(t, 0, stats.SkippedMissingPersonality)

	records := decodeSFTLines(t, out.Bytes())
	require.Len(t, records, 1)
	require.Len(t, records[0].Messages, 3)
	require.Equal(t, "system", records[0].Messages[0].Role)
	require.Equal(t, "alpha system\n\nalpha scratch", records[0].Messages[0].Content)
	require.Equal(t, "user", records[0].Messages[1].Role)
	require.Equal(t, "hello", records[0].Messages[1].Content)
	require.Equal(t, "assistant", records[0].Messages[2].Role)
	require.Equal(t, "hi there", records[0].Messages[2].Content)
}

func TestProcessSFTMessageBatch_UsesFallbackChatPersonality(t *testing.T) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)

	stats := &SFTExportStats{}
	fallback := &ent.Personality{
		Name:         "Fallback",
		SystemPrompt: "fallback system",
		Scratchpad:   "fallback scratch",
	}
	messages := []*ent.ChatMessage{
		{Origin: chatmessage.OriginUser, Message: "question"},
		{Origin: chatmessage.OriginAssistant, Message: "answer", GenerationPersonality: "unknown"},
	}

	pending, err := processSFTMessageBatch(encoder, messages, map[string]*ent.Personality{}, fallback, nil, stats)
	require.NoError(t, err)
	require.Nil(t, pending)
	require.Equal(t, 1, stats.ExportedPairs)
	require.Equal(t, 0, stats.SkippedMissingPersonality)

	records := decodeSFTLines(t, out.Bytes())
	require.Len(t, records, 1)
	require.Equal(t, "fallback system\n\nfallback scratch", records[0].Messages[0].Content)
}

func TestProcessSFTMessageBatch_TracksUnmatchedTurns(t *testing.T) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)

	stats := &SFTExportStats{}
	personalities := map[string]*ent.Personality{
		"alpha": {Name: "Alpha", SystemPrompt: "sys", Scratchpad: "pad"},
	}
	messages := []*ent.ChatMessage{
		{Origin: chatmessage.OriginAssistant, Message: "orphan assistant"},
		{Origin: chatmessage.OriginUser, Message: "first user"},
		{Origin: chatmessage.OriginUser, Message: "second user"},
		{Origin: chatmessage.OriginAssistant, Message: "paired now", GenerationPersonality: "alpha"},
	}

	pending, err := processSFTMessageBatch(encoder, messages, personalities, nil, nil, stats)
	require.NoError(t, err)
	require.Nil(t, pending)
	require.Equal(t, 1, stats.ExportedPairs)
	require.Equal(t, 1, stats.SkippedAssistantWithoutUser)
	require.Equal(t, 1, stats.SkippedUserWithoutAssistant)
}

func TestProcessSFTMessageBatch_SkipsMissingPersonality(t *testing.T) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)

	stats := &SFTExportStats{}
	messages := []*ent.ChatMessage{
		{Origin: chatmessage.OriginUser, Message: "hey"},
		{Origin: chatmessage.OriginAssistant, Message: "response"},
	}

	pending, err := processSFTMessageBatch(encoder, messages, map[string]*ent.Personality{}, nil, nil, stats)
	require.NoError(t, err)
	require.Nil(t, pending)
	require.Equal(t, 0, stats.ExportedPairs)
	require.Equal(t, 1, stats.SkippedMissingPersonality)
	require.Equal(t, 0, out.Len())
}
