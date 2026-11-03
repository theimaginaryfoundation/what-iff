package slackrelay

import (
	"encoding/json"
	"net/url"
	"strings"
)

// BotScopes are the bot token scopes a persona's app asks for (docs/adr/0x032-slack-relay.md §1).
var BotScopes = []string{
	"channels:history", "groups:history", // message events, and a thread's first message
	"channels:read", "groups:read", // channels to bind, their names, renames
	"channels:join",             // joining a public channel from the app
	"chat:write",                // replies and notices
	"files:read", "files:write", // files in and out
	"reactions:write", // the "working on it" reaction
	"users:read",      // speakers' names, bots, the app id
}

// BotEvents are the events a persona's app subscribes to. app_mention is left out on purpose:
// a mention also arrives as a message event.
var BotEvents = []string{
	"message.channels", "message.groups",
	"channel_rename", "group_rename",
	"channel_left", "group_left",
	"app_uninstalled", "tokens_revoked",
}

// Slack's limits on an app's names and description.
const (
	maxAppNameRunes     = 35
	maxBotNameRunes     = 80
	maxDescriptionRunes = 140
)

// Manifest is the app manifest for a persona's Slack app: named for the persona, Socket Mode
// on, token rotation off (the bot token must not expire), and exactly the scopes and events the
// relay uses.
func Manifest(personaName string) map[string]any {
	personaName = strings.TrimSpace(personaName)
	if personaName == "" {
		personaName = "What Iff persona"
	}
	name := clip(personaName, maxAppNameRunes)
	return map[string]any{
		"display_information": map[string]any{
			"name":        name,
			"description": clip(name+", a What Iff persona.", maxDescriptionRunes),
		},
		"features": map[string]any{
			"bot_user": map[string]any{
				"display_name":  clip(personaName, maxBotNameRunes),
				"always_online": true,
			},
		},
		"oauth_config": map[string]any{
			"scopes": map[string]any{"bot": BotScopes},
		},
		"settings": map[string]any{
			"event_subscriptions":    map[string]any{"bot_events": BotEvents},
			"socket_mode_enabled":    true,
			"token_rotation_enabled": false,
			"org_deploy_enabled":     false,
			"interactivity":          map[string]any{"is_enabled": false},
		},
	}
}

// CreateAppURL is the link that opens Slack's "create an app" flow with manifest filled in.
func CreateAppURL(manifest map[string]any) (string, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	return "https://api.slack.com/apps?new_app=1&manifest_json=" + url.QueryEscape(string(raw)), nil
}

// AppSettingsURL is an app's settings page in Slack (where its icon is uploaded).
func AppSettingsURL(appID string) string {
	return "https://api.slack.com/apps/" + url.PathEscape(appID) + "/general"
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n]))
	}
	return s
}
