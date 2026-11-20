package agent

import (
	"context"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// contextUserNameForUser, when non-nil, chooses the name in the first-message line
// "The user's name is …" (see userNameContextLine). Returning "" keeps the default, the account's
// username. Nil in the open-source build, so every user is named by their username as before.
//
// Why a seam: an account provisioned by another system (a headless integration) can have a
// generated username nobody would call a person by, while that system knows the person's real
// name. The build that provisions such accounts decides which name to use; the agent only asks.
var contextUserNameForUser func(ctx context.Context, a *Agent, user *models.UserResponse) string

// contextUserName is the name the agent gives the model for this user: the seam's answer when it
// has one, otherwise the username.
func (a *Agent) contextUserName(ctx context.Context, user *models.UserResponse) string {
	if user == nil {
		return ""
	}
	if contextUserNameForUser != nil {
		if name := strings.TrimSpace(contextUserNameForUser(ctx, a, user)); name != "" {
			return name
		}
	}
	return user.Username
}
