package buzzrelay

import (
	"os"
	"strings"
)

// The Buzz relay is off unless the operator switches it on with BUZZ_RELAY_ENABLED=true; when it
// is on, it is on for every user of the server. The routes, the gateway and the post_to_buzz
// tool all ask Enabled.

// Enabled reports whether the operator switched the relay on: BUZZ_RELAY_ENABLED is true, 1 or
// on. It is read on every call, so a value from the server's .env file counts.
func Enabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("BUZZ_RELAY_ENABLED"))) {
	case "true", "1", "on", "yes":
		return true
	}
	return false
}
