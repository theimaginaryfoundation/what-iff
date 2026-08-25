package datastore

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestReadBackupJSONLAllowsLargeRecords(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("chat_messages.jsonl")
	require.NoError(t, err)

	message := strings.Repeat("x", 256*1024)
	_, err = w.Write([]byte(`{"message":"` + message + `"}` + "\n"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	records, invalid, err := readBackupJSONL[models.AccountBackupChatMessage](zr, "chat_messages.jsonl")
	require.NoError(t, err)
	require.Zero(t, invalid)
	require.Len(t, records, 1)
	require.Equal(t, message, records[0].Message)
}

func TestReadBackupJSONLMissingSectionIsEmpty(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	_, err := zw.Create("manifest.json")
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	records, invalid, err := readBackupJSONL[models.AccountBackupMood](zr, "moods.jsonl")
	require.NoError(t, err)
	require.Zero(t, invalid)
	require.Empty(t, records)
}

func TestReadBackupJSONLRituals(t *testing.T) {
	pid := uuid.New()
	rec := models.AccountBackupRitual{
		ID:            uuid.New(),
		Name:          "Skill",
		Description:   "desc",
		Content:       "body",
		Hotkeys:       "meta+k",
		PersonalityID: &pid,
		CreatedAt:     time.Unix(1700000000, 0).UTC(),
		UpdatedAt:     time.Unix(1700000001, 0).UTC(),
	}
	line, err := json.Marshal(rec)
	require.NoError(t, err)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("rituals.jsonl")
	require.NoError(t, err)
	_, err = w.Write(append(line, '\n'))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	got, invalid, err := readBackupJSONL[models.AccountBackupRitual](zr, "rituals.jsonl")
	require.NoError(t, err)
	require.Zero(t, invalid)
	require.Len(t, got, 1)
	require.Equal(t, rec.ID, got[0].ID)
	require.Equal(t, rec.Name, got[0].Name)
	require.Equal(t, rec.PersonalityID, got[0].PersonalityID)
}
