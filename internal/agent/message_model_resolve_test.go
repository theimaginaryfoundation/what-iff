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

func sqlmockModelRow(id uuid.UUID, name, provider, tier string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "display_name", "description", "provider", "tool_support",
		"base_credits_per_slab", "subscription_tier", "deleted",
	}).AddRow(id.String(), name, name, "desc", provider, true, 5, tier, false)
}

func TestResolveModelForChat_NilChatUsesDefault(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	name, provider, tier := a.resolveModelForChat(context.Background(), nil)
	require.Equal(t, defaultModel, name)
	require.Equal(t, string(models.ModelProviderOpenAI), provider)
	require.Equal(t, "", tier, "unknown model resolves to empty tier; the meter classifies it")
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
	name, provider, tier := a.resolveModelForChat(context.Background(), chat)
	require.Equal(t, "gemini-3.5-flash", name)
	require.Equal(t, string(models.ModelProviderOpenAI), provider)
	require.Equal(t, "", tier, "no DB row: tier is unknown/empty")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResolveModelForChat_ModelIDAuthoritativeOverStaleName(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	modelID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*id.*").
		WillReturnRows(sqlmockModelRow(modelID, "gemini-3.5-flash", "google", "medium"))
	mock.ExpectCommit()

	a := newTestAgent(ds)
	chat := &models.Chat{
		ID:        uuid.New(),
		ModelID:   modelID,
		ModelName: "gpt-5.1",
	}
	name, provider, tier := a.resolveModelForChat(context.Background(), chat)
	require.Equal(t, "gemini-3.5-flash", name)
	require.Equal(t, "google", provider)
	require.Equal(t, "medium", tier)
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
		WillReturnRows(sqlmockModelRow(uuid.New(), "glm-5.2", "zai", "high"))

	a := newTestAgent(ds)
	chat := &models.Chat{
		ID:        uuid.New(),
		ModelID:   modelID,
		ModelName: "glm-5.2",
	}
	name, provider, tier := a.resolveModelForChat(context.Background(), chat)
	require.Equal(t, "glm-5.2", name)
	require.Equal(t, "zai", provider)
	require.Equal(t, "high", tier)
	require.NoError(t, mock.ExpectationsWereMet())
}
