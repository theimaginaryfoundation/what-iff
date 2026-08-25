package memory

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestParseOptionalUUID(t *testing.T) {
	got, err := parseOptionalUUID(nil)
	require.NoError(t, err)
	require.Nil(t, got)

	empty := ""
	got, err = parseOptionalUUID(&empty)
	require.NoError(t, err)
	require.Nil(t, got)

	valid := uuid.New().String()
	got, err = parseOptionalUUID(&valid)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, valid, got.String())

	invalid := "not-a-uuid"
	_, err = parseOptionalUUID(&invalid)
	require.Error(t, err)
}

func TestToCreateMemoryInput(t *testing.T) {
	chatID := uuid.New().String()
	personalityID := uuid.New().String()

	input, err := toCreateMemoryInput(createMemoryRequest{
		Content:             "remember this",
		Level:               models.MemoryLevelThread,
		ChatID:              &chatID,
		PinnedPersonalityID: &personalityID,
		Type:                models.MemoryTypeContext,
		Starred:             true,
	})
	require.NoError(t, err)
	require.Equal(t, "remember this", input.Content)
	require.Equal(t, models.MemoryLevelThread, input.Level)
	require.NotNil(t, input.ChatID)
	require.Equal(t, chatID, input.ChatID.String())
	require.NotNil(t, input.PinnedPersonalityID)
	require.Equal(t, personalityID, input.PinnedPersonalityID.String())
	require.Equal(t, models.MemoryTypeContext, input.Type)
	require.True(t, input.Starred)
}
