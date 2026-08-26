package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestAgentTestHooks_AnySet(t *testing.T) {
	t.Parallel()

	require.False(t, agentTestHooks{}.anySet())

	require.True(t, agentTestHooks{
		GetMemoriesOverride: func(ctx context.Context, userID, chatID, personalityID uuid.UUID, userMessage string) ([]string, error) {
			return nil, nil
		},
	}.anySet())

	require.True(t, agentTestHooks{
		ImageRitualPersistImage: func(ctx context.Context, userID uuid.UUID, attachment *models.FileAttachment, imageBase64 string) error {
			return nil
		},
	}.anySet())
}
