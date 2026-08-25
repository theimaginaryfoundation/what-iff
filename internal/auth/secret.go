package auth

import (
	"errors"
	"fmt"
	"strings"
)

// MinSecretLen mirrors datastore.MinTokenEncryptionSecretLen — the same
// minimum-strength bar applied to every signing/encryption secret in this
// project, not just TOKEN_ENCRYPTION_SECRET.
const MinSecretLen = 32

// knownPlaceholders are values that have shipped as a docker-compose or
// .env.example default at some point. A shipped value is public the moment
// this repo is public, so length alone doesn't make it safe — reject these
// outright regardless of length, alongside the generic terms below.
var knownPlaceholders = []string{
	"devsecret",
	"devrefresh",
	"changeme",
	"changeit",
	"secret",
	"password",
	"example",
	"test",
	"your_super_secret_key_change_this_in_production",
	"change_this_secret_to_a_long_random_value",
}

var (
	errSecretNotConfigured = errors.New("secret is not configured")
	errSecretTooShort      = fmt.Errorf("secret must be at least %d characters", MinSecretLen)
)

// ValidateSecret enforces minimum-strength constraints on a named
// signing/encryption secret read from the environment: non-empty, at least
// MinSecretLen characters, and not a value that has ever shipped as a
// default or documentation example in this repo. name is used only to
// identify which secret failed in the returned error.
func ValidateSecret(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s: %w", name, errSecretNotConfigured)
	}

	lower := strings.ToLower(trimmed)
	for _, p := range knownPlaceholders {
		if lower == p {
			return fmt.Errorf("%s: refusing known placeholder value %q", name, p)
		}
	}

	if len(trimmed) < MinSecretLen {
		return fmt.Errorf("%s: %w", name, errSecretTooShort)
	}

	return nil
}
