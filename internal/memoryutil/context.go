package memoryutil

import (
	"regexp"
	"strings"
)

var memoryContextMetadataSuffix = regexp.MustCompile(`\s*\[stored_at=[^\]]+\]\s*$`)

// StripMemoryContextMetadata removes the stored_at/confidence/age_days suffix from
// prefetched memory lines persisted in model context.
func StripMemoryContextMetadata(content string) string {
	return strings.TrimSpace(memoryContextMetadataSuffix.ReplaceAllString(content, ""))
}
