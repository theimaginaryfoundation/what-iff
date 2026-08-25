package agent

import (
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func normalizeMemoryScope(scope string) string {
	switch strings.TrimSpace(scope) {
	case string(models.MemoryLevelGlobal), "User", "user":
		return "User"
	case string(models.MemoryLevelThread), "Chat", "chat":
		return "Chat"
	default:
		return ""
	}
}

func matchLiveMemoryByFormattedContent(formatted string, live []*models.Memory) *models.Memory {
	stripped := memoryutil.StripMemoryContextMetadata(formatted)
	if strings.TrimSpace(stripped) == "" {
		return nil
	}
	norm := memoryutil.NormalizeContentForDedupe(stripped)
	for _, mem := range live {
		if mem == nil {
			continue
		}
		if memoryutil.NormalizeContentForDedupe(mem.Content) == norm {
			return mem
		}
	}
	return nil
}
