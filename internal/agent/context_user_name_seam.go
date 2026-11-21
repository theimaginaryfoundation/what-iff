package agent

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// contextUserNameForUser, when non-nil, chooses the name in the first-message line
// "The user's name is …" (see userNameContextLine). Returning "" keeps the default, the account's
// username. Nil in the open-source build, so every user is named by their username as before.
//
// The answer must be a short, single-line name: it goes into a developer-role message replayed for
// the whole chat, so contextUserName keeps the username instead of any answer that is blank, longer
// than maxContextUserNameRunes, or contains a control, line or paragraph separator, or format
// character.
//
// Why a seam: an account provisioned by another system (a headless integration) can have a
// generated username nobody would call a person by, while that system knows the person's real
// name. The build that provisions such accounts decides which name to use; the agent only asks.
var contextUserNameForUser func(ctx context.Context, a *Agent, user *models.UserResponse) string

// maxContextUserNameRunes is the longest name the seam may give, the length of the users table's
// name columns.
const maxContextUserNameRunes = 50

// contextUserName is the name the agent gives the model for this user: the seam's answer when it
// has a usable one (see contextUserNameForUser), otherwise the username.
func (a *Agent) contextUserName(ctx context.Context, user *models.UserResponse) string {
	if user == nil {
		return ""
	}
	if contextUserNameForUser != nil {
		if name := strings.TrimSpace(contextUserNameForUser(ctx, a, user)); usableContextUserName(name) {
			return name
		}
	}
	return user.Username
}

// usableContextUserName reports whether name can stand in the user-name line: non-empty, at most
// maxContextUserNameRunes long, and on one line with no control or invisible format characters.
func usableContextUserName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > maxContextUserNameRunes {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Cf)
	})
}
