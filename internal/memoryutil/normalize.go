package memoryutil

import (
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// NormalizeContentForDedupe applies conservative normalization for exact-ish
// dedupe checks (trim, lowercase, collapse whitespace).
func NormalizeContentForDedupe(content string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(content))), " ")
}

// ExtractedSensitivity reads the sensitivity the extraction model gave a memory: "sensitive" stays
// sensitive; an empty value or "personal" is personal; "public" is also personal, because
// extraction never assigns public (only the memory manager does); and any other non-empty value
// fails closed to sensitive rather than reading as less delicate than it may be.
func ExtractedSensitivity(raw models.MemorySensitivity) models.MemorySensitivity {
	switch models.MemorySensitivity(strings.ToLower(strings.TrimSpace(string(raw)))) {
	case models.MemorySensitivitySensitive:
		return models.MemorySensitivitySensitive
	case "", models.MemorySensitivityPersonal, models.MemorySensitivityPublic:
		return models.MemorySensitivityPersonal
	default:
		return models.MemorySensitivitySensitive
	}
}

// NormalizeExtractedMemories trims content, drops blanks, coerces any scope
// outside {User, Chat} to Chat, defaults missing confidence to medium, and reads sensitivity with
// ExtractedSensitivity.
func NormalizeExtractedMemories(mems []models.ExtractedMemory, max int) []models.ExtractedMemory {
	out := make([]models.ExtractedMemory, 0, len(mems))
	for _, m := range mems {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		scope := m.Scope
		if scope != "User" && scope != "Chat" {
			scope = "Chat"
		}
		confidence := m.Confidence
		if confidence == "" {
			confidence = models.MemoryConfidenceMedium
		}
		out = append(out, models.ExtractedMemory{Content: content, Scope: scope, Confidence: confidence, Sensitivity: ExtractedSensitivity(m.Sensitivity)})
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}
