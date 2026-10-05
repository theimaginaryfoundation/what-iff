package accountexport

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/exporter"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// The id/chat remap rewrites each memory record; it must carry the sensitivity through untouched.
func TestRemapMemoryRecords_PreservesSensitivity(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	id := uuid.New()
	target := uuid.New()
	line := `{"id":"` + id.String() + `","content":"delicate","created_at":"` + now.Format(time.RFC3339) + `","sensitivity":"sensitive"}` + "\n" +
		`{"id":"` + uuid.NewString() + `","content":"legacy","created_at":"` + now.Format(time.RFC3339) + `"}` + "\n"

	out, err := remapMemoryRecords([]byte(line), target, nil, nil)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, lines, 2)
	require.Contains(t, lines[0], `"sensitivity":"sensitive"`)
	require.NotContains(t, lines[1], `"sensitivity"`, "an old export stays without the key, which imports as personal")
	require.Contains(t, lines[0], uuid.NewSHA1(target, id[:]).String(), "ids are still rekeyed")
}

func TestToImportConversations_CarriesMemorySensitivityLimit(t *testing.T) {
	msg := []exporter.ParsedMessage{{Sender: "human", Text: "hi", CreatedAt: time.Now()}}
	out := toImportConversations([]exporter.ParsedConversation{
		{UUID: uuid.NewString(), Name: "public", Messages: msg, WhatiffMemorySensitivityLimit: models.MemorySensitivityPublic},
		{UUID: uuid.NewString(), Name: "legacy", Messages: msg},
	}, nil)
	require.Len(t, out, 2)
	require.Equal(t, models.MemorySensitivityPublic, out[0].MemorySensitivityLimit)
	require.Equal(t, models.MemorySensitivity(""), out[1].MemorySensitivityLimit, "an export without the field imports as unrestricted")
}
