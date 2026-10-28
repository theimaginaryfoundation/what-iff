package slackrelay

import (
	"context"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// The Slack relay is an experimental feature (models.ExperimentalFeatureSlackRelay). It is off
// unless the operator switches it on with SLACK_RELAY_ENABLED=true; a build with a per-user
// experimental gate (featuregate.NewExperimental) further limits it to the users it is turned on
// for. The routes, the gateway and the post_to_slack tool all ask UserMayUse.

// Enabled reports whether the operator switched the relay on: SLACK_RELAY_ENABLED is true, 1 or
// on. It is read on every call, so a value from the server's .env file counts.
func Enabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("SLACK_RELAY_ENABLED"))) {
	case "true", "1", "on", "yes":
		return true
	}
	return false
}

// UserMayUse reports whether userID may use the relay: it is switched on, and the per-user
// experimental policy (when a build links one) has it on for them.
func UserMayUse(ctx context.Context, userID uuid.UUID) bool {
	return Enabled() && featuregate.ExperimentalEnabled(ctx, userID, models.ExperimentalFeatureSlackRelay)
}
