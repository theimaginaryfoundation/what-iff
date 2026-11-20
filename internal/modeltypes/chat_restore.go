package modeltypes

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxRestoreGuidanceRunes bounds the user's natural-language restoration preferences.
const MaxRestoreGuidanceRunes = 4000

// NormalizeRestoreGuidance trims optional restoration preferences and rejects oversized input.
func NormalizeRestoreGuidance(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxRestoreGuidanceRunes {
		return "", fmt.Errorf("restore guidance must be valid text of at most %d characters", MaxRestoreGuidanceRunes)
	}
	return value, nil
}
