package memoryutil

import (
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// CollapsedExtractedMemory is one unique extracted memory after batch dedupe.
type CollapsedExtractedMemory struct {
	Content             string
	Scope               string
	Confidence          models.MemoryConfidence
	BatchDuplicateCount int
}

var memoryConfidenceRank = map[models.MemoryConfidence]int{
	models.MemoryConfidenceLow:    0,
	models.MemoryConfidenceMedium: 1,
	models.MemoryConfidenceHigh:   2,
}

func isConfidenceLower(a, b models.MemoryConfidence) bool {
	return memoryConfidenceRank[a] < memoryConfidenceRank[b]
}

func collapseKey(content, scope string) string {
	return NormalizeContentForDedupe(content) + "\x00" + scope
}

// CollapseExtractedMemories folds duplicate rows from one extraction pass.
// Duplicates are matched on normalized content + scope; confidence keeps the lowest rank.
func CollapseExtractedMemories(mems []models.ExtractedMemory) []CollapsedExtractedMemory {
	normalized := NormalizeExtractedMemories(mems, 0)
	if len(normalized) == 0 {
		return nil
	}

	order := make([]string, 0, len(normalized))
	byKey := make(map[string]CollapsedExtractedMemory, len(normalized))

	for _, item := range normalized {
		key := collapseKey(item.Content, item.Scope)
		existing, ok := byKey[key]
		if !ok {
			byKey[key] = CollapsedExtractedMemory{
				Content:             item.Content,
				Scope:               item.Scope,
				Confidence:          item.Confidence,
				BatchDuplicateCount: 1,
			}
			order = append(order, key)
			continue
		}
		existing.BatchDuplicateCount++
		if isConfidenceLower(item.Confidence, existing.Confidence) {
			existing.Confidence = item.Confidence
		}
		byKey[key] = existing
	}

	out := make([]CollapsedExtractedMemory, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}
