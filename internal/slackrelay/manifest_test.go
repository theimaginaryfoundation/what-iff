package slackrelay

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifest(t *testing.T) {
	m := Manifest("Vix")
	link, err := CreateAppURL(m)
	require.NoError(t, err)
	u, err := url.Parse(link)
	require.NoError(t, err)
	assert.Equal(t, "api.slack.com", u.Host)
	assert.Equal(t, "1", u.Query().Get("new_app"))

	var back struct {
		Display  map[string]string `json:"display_information"`
		Features struct {
			BotUser map[string]any `json:"bot_user"`
		} `json:"features"`
		OAuth struct {
			Scopes struct {
				Bot []string `json:"bot"`
			} `json:"scopes"`
		} `json:"oauth_config"`
		Settings struct {
			Events struct {
				Bot []string `json:"bot_events"`
			} `json:"event_subscriptions"`
			SocketMode    bool `json:"socket_mode_enabled"`
			TokenRotation bool `json:"token_rotation_enabled"`
		} `json:"settings"`
	}
	require.NoError(t, json.Unmarshal([]byte(u.Query().Get("manifest_json")), &back))
	assert.Equal(t, "Vix", back.Display["name"])
	assert.Equal(t, "Vix", back.Features.BotUser["display_name"])
	assert.Equal(t, BotScopes, back.OAuth.Scopes.Bot)
	assert.Equal(t, BotEvents, back.Settings.Events.Bot)
	assert.NotContains(t, back.Settings.Events.Bot, "app_mention", "a mention also arrives as a message; both would answer twice")
	assert.True(t, back.Settings.SocketMode)
	assert.False(t, back.Settings.TokenRotation, "the bot token must not expire")

	long := Manifest(strings.Repeat("x", 50))
	assert.Len(t, long["display_information"].(map[string]any)["name"], maxAppNameRunes)
	assert.Equal(t, "What Iff persona", Manifest("  ")["display_information"].(map[string]any)["name"])
}
