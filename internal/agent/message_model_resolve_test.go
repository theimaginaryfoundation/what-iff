package agent

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func sqlmockModelRow(id uuid.UUID, name, provider, tier string, vision bool) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "display_name", "description", "provider", "tool_support", "vision_support",
		"base_credits_per_slab", "subscription_tier", "deleted",
	}).AddRow(id.String(), name, name, "desc", provider, true, vision, 5, tier, false)
}

func TestResolveModelForChat_NilChatUsesDefault(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	got := a.resolveModelForChat(context.Background(), nil)
	require.Equal(t, defaultModel, got.name)
	require.Equal(t, string(models.ModelProviderOpenAI), got.provider)
	require.Equal(t, "", got.subscriptionTier, "unknown model resolves to empty tier; the meter classifies it")
	require.True(t, got.visionSupport, "default model's vision flag comes from the seed catalog")
}

func TestResolveModelForChat_NameOnlyWithoutDBRowKeepsNameAndDefaultProvider(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*name.*").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "display_name", "description", "provider", "tool_support",
			"base_credits_per_slab", "subscription_tier", "deleted",
		}))

	a := newTestAgent(ds)
	chat := &models.Chat{ModelName: "gemini-3.5-flash"}
	got := a.resolveModelForChat(context.Background(), chat)
	require.Equal(t, "gemini-3.5-flash", got.name)
	require.Equal(t, string(models.ModelProviderOpenAI), got.provider)
	require.Equal(t, "", got.subscriptionTier, "no DB row: tier is unknown/empty")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResolveModelForChat_ModelIDAuthoritativeOverStaleName(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	modelID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*id.*").
		WillReturnRows(sqlmockModelRow(modelID, "gemini-3.5-flash", "google", "medium", true))
	mock.ExpectCommit()

	a := newTestAgent(ds)
	chat := &models.Chat{
		ID:        uuid.New(),
		ModelID:   modelID,
		ModelName: "gpt-5.1",
	}
	got := a.resolveModelForChat(context.Background(), chat)
	require.Equal(t, "gemini-3.5-flash", got.name)
	require.Equal(t, "google", got.provider)
	require.Equal(t, "medium", got.subscriptionTier)
	require.True(t, got.visionSupport)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResolveModelForChat_FallsBackToModelNameLookup(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	modelID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*id.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "display_name", "description", "provider", "tool_support", "deleted"}))
	mock.ExpectRollback()
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*name.*").
		WillReturnRows(sqlmockModelRow(uuid.New(), "deepseek-chat", "deepseek", "high", false))

	a := newTestAgent(ds)
	chat := &models.Chat{
		ID:        uuid.New(),
		ModelID:   modelID,
		ModelName: "deepseek-chat",
	}
	got := a.resolveModelForChat(context.Background(), chat)
	require.Equal(t, "deepseek-chat", got.name)
	require.Equal(t, "deepseek", got.provider)
	require.Equal(t, "high", got.subscriptionTier)
	require.False(t, got.visionSupport, "text-only DB row keeps vision off")
	require.NoError(t, mock.ExpectationsWereMet())
}
