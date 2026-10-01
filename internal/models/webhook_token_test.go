package models

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNormalizeWebhookScopes(t *testing.T) {
	t.Parallel()

	t.Run("nothing requested is the default, write-only", func(t *testing.T) {
		for _, in := range [][]string{nil, {}} {
			got, err := NormalizeWebhookScopes(in)
			require.NoError(t, err)
			require.Equal(t, []WebhookScope{WebhookScopeMessagesWrite}, got)
		}
	})

	t.Run("read access has to be asked for", func(t *testing.T) {
		got, err := NormalizeWebhookScopes([]string{"chat:read"})
		require.NoError(t, err)
		require.Equal(t, []WebhookScope{WebhookScopeChatRead}, got, "asking only for read does not also grant write")
	})

	t.Run("duplicates collapse and order is stable", func(t *testing.T) {
		got, err := NormalizeWebhookScopes([]string{" chat:read ", "messages:write", "chat:read"})
		require.NoError(t, err)
		require.Equal(t, []WebhookScope{WebhookScopeMessagesWrite, WebhookScopeChatRead}, got)
	})

	t.Run("an unknown scope is an error naming the valid ones", func(t *testing.T) {
		for _, bad := range []string{"admin", "chat:write", "", "CHAT:READ", "*"} {
			_, err := NormalizeWebhookScopes([]string{"chat:read", bad})
			require.Error(t, err, "%q", bad)
			require.Contains(t, err.Error(), "messages:write")
			require.Contains(t, err.Error(), "chat:read")
		}
	})
}

// TestEffectiveWebhookScopes is the backward-compatibility guarantee: a token stored before scopes
// existed has no scopes recorded, and must keep doing exactly what it did (post) and nothing more.
func TestEffectiveWebhookScopes(t *testing.T) {
	t.Parallel()

	require.Equal(t, []WebhookScope{WebhookScopeMessagesWrite}, EffectiveWebhookScopes(nil), "legacy token: write only")
	require.Equal(t, []WebhookScope{WebhookScopeMessagesWrite}, EffectiveWebhookScopes([]string{}))
	require.NotContains(t, EffectiveWebhookScopes(nil), WebhookScopeChatRead, "a legacy token must never read")

	require.Equal(t, []WebhookScope{WebhookScopeChatRead}, EffectiveWebhookScopes([]string{"chat:read"}))
	require.Equal(t, []WebhookScope{WebhookScopeChatRead}, EffectiveWebhookScopes([]string{"chat:read", "retired:scope"}),
		"a scope the code no longer knows is dropped, never passed through")
}

func TestEffectiveWebhookScopesDoesNotAliasDefaults(t *testing.T) {
	t.Parallel()

	got := EffectiveWebhookScopes(nil)
	got[0] = WebhookScopeChatRead
	require.Equal(t, []WebhookScope{WebhookScopeMessagesWrite}, EffectiveWebhookScopes(nil),
		"mutating a returned slice must not change what legacy tokens get")
	require.Equal(t, []WebhookScope{WebhookScopeMessagesWrite}, DefaultWebhookScopes)
}

func TestWebhookAuthPrincipalHasScope(t *testing.T) {
	t.Parallel()

	p := &WebhookAuthPrincipal{Scopes: []WebhookScope{WebhookScopeChatRead}}
	require.True(t, p.HasScope(WebhookScopeChatRead))
	require.False(t, p.HasScope(WebhookScopeMessagesWrite))
	require.False(t, (*WebhookAuthPrincipal)(nil).HasScope(WebhookScopeChatRead), "a nil principal has no scopes")
}

func TestNewWebhookPersonalityCarriesNoPrivateState(t *testing.T) {
	t.Parallel()

	accent := "#fff"
	got := NewWebhookPersonality(&Personality{
		ID: uuid.New(), Name: "Vix", AccentColor: &accent,
		SystemPrompt: "secret", Scratchpad: "secret", ScratchpadHistory: []string{"secret"},
	})
	require.Equal(t, "Vix", got.Name)
	require.Equal(t, &accent, got.AccentColor)
}
