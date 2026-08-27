package modeltypes

import (
	"fmt"
	"strings"
)

const (
	// MaxChatTags limits the total number of tags on a chat.
	MaxChatTags = 10
	// MaxChatTagLength limits the number of characters in a single tag.
	MaxChatTagLength = 10
)

// NormalizeAndValidateChatTags trims each tag and validates count/content constraints.
//
// Semantics:
//   - nil input returns nil output (useful for PATCH-style "unset" handling).
//   - empty-but-non-nil input returns empty slice (explicit clear).
//
// Callers that need PATCH semantics should check nil before deciding whether to write.
func NormalizeAndValidateChatTags(tags []string) ([]string, error) {
	// make([]string, 0) below is non-nil, so nil input must be special-cased
	// to actually return nil — otherwise the documented nil-in/nil-out PATCH
	// contract silently never holds.
	if tags == nil {
		return nil, nil
	}
	if len(tags) > MaxChatTags {
		return nil, fmt.Errorf("chat can have at most %d tags", MaxChatTags)
	}
	normalized := make([]string, len(tags))
	for i, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			return nil, fmt.Errorf("chat tags cannot be empty")
		}
		if len(trimmed) > MaxChatTagLength {
			return nil, fmt.Errorf("chat tags must be at most %d characters", MaxChatTagLength)
		}
		normalized[i] = trimmed
	}
	return normalized, nil
}

// ValidateChatTags validates tags without returning a normalized copy.
func ValidateChatTags(tags []string) error {
	_, err := NormalizeAndValidateChatTags(tags)
	return err
}
