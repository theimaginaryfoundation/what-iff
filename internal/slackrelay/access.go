package slackrelay

import (
	"os"
	"strings"
)

// The Slack relay is off unless the operator switches it on with SLACK_RELAY_ENABLED=true; when
// it is on, it is on for every user of the server. The routes, the gateway and the post_to_slack
// tool all ask Enabled.

// Enabled reports whether the operator switched the relay on: SLACK_RELAY_ENABLED is true, 1 or
// on. It is read on every call, so a value from the server's .env file counts.
func Enabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("SLACK_RELAY_ENABLED"))) {
	// The same values as the plugin's other switches and the ADR. The relay has not shipped,
	// so no deployment relies on the "yes" an earlier draft also took.
	case "true", "1", "on":
		return true
	}
	return false
}
