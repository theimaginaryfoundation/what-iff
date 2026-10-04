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

// NormalizeExtractedMemories trims content, drops blanks, coerces any scope
// outside {User, Chat} to Chat, defaults missing confidence to medium, and reads sensitivity as
// "sensitive" or personal (extraction never assigns public).
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
		sensitivity := models.MemorySensitivityPersonal
		if models.MemorySensitivity(strings.ToLower(strings.TrimSpace(string(m.Sensitivity)))) == models.MemorySensitivitySensitive {
			sensitivity = models.MemorySensitivitySensitive
		}
		out = append(out, models.ExtractedMemory{Content: content, Scope: scope, Confidence: confidence, Sensitivity: sensitivity})
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}
