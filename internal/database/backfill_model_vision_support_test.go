package database

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/model"
)

func TestBackfillModelVisionSupport(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", "file:"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { _ = client.Close() })
	_, err = db.Exec(`CREATE TABLE models (
		id uuid PRIMARY KEY, name text NOT NULL, display_name text NOT NULL, description text NOT NULL,
		provider text NOT NULL DEFAULT 'openai', tool_support bool NOT NULL DEFAULT false, vision_support bool NULL,
		base_credits_per_slab integer NOT NULL DEFAULT 1, subscription_tier text NOT NULL DEFAULT 'high',
		deleted bool NOT NULL DEFAULT false, is_default bool NOT NULL DEFAULT false)`)
	require.NoError(t, err)

	create := func(name string, provider model.Provider) *ent.ModelCreate {
		return client.Model.Create().SetName(name).SetDisplayName(name).SetDescription("").SetProvider(provider)
	}
	legacyVision := create("qwen3.7-plus", model.ProviderQwen).SaveX(ctx)
	legacyText := create("deepseek-chat", model.ProviderDeepseek).SaveX(ctx)
	adminSetFalse := create("gpt-5.1", model.ProviderOpenai).SetVisionSupport(false).SaveX(ctx)

	require.NoError(t, backfillModelVisionSupport(ctx, client, zap.NewNop()))

	vision := func(m *ent.Model) *bool { return client.Model.GetX(ctx, m.ID).VisionSupport }
	require.True(t, *vision(legacyVision))
	require.False(t, *vision(legacyText))
	require.False(t, *vision(adminSetFalse), "rows with a value must not be overwritten")
}

func TestLegacyVisionSupport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		model    string
		want     bool
	}{
		{provider: "openai", model: "gpt-5.1", want: true},
		{provider: "anthropic", model: "claude-sonnet-4-6", want: true},
		{provider: "zai", model: "glm-5.2", want: true},
		{provider: "google", model: "gemini-3.5-flash", want: true},
		{provider: "qwen", model: "qwen3.7-plus", want: true},
		{provider: "qwen", model: "qwen-plus", want: false},
		{provider: "mistral", model: "mistral-medium-2508", want: true},
		{provider: "mistral", model: "codestral-latest", want: false},
		{provider: "deepseek", model: "deepseek-chat", want: false},
		{provider: "xiaomi", model: "mimo-v2.5-pro", want: false},
		{provider: "xiaomi", model: "mimo-7b-rl", want: false},
		{provider: "xiaomi", model: "mimo-v2.10", want: true},
		{provider: "xiaomi", model: "MiMo-2.6-Flash", want: true},
		{provider: "xiaomi", model: "mimo-v2-omni", want: true},
		{provider: "deepseek", model: "mimo-v2.6", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			t.Parallel()
			if got := legacyVisionSupport(tt.provider, tt.model); got != tt.want {
				t.Fatalf("legacyVisionSupport(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}
