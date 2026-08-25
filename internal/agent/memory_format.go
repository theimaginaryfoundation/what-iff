package agent

import (
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const memoryStalenessNote = "Note on recalled memories: prefer durable facts; be wary of older memories (see age_days), especially ones referencing transient state."

// formatMemoryForContext is a thin alias for the shared formatter so existing call sites read
// unchanged. All load paths (prefetch, find_context, carry-over) go through the same function.
func formatMemoryForContext(mem *models.Memory) string {
	return memoryutil.FormatMemoryForContext(mem)
}

func (c *chatContext) hasMemoryStalenessNote() bool {
	if c == nil {
		return false
	}
	for _, line := range c.memories {
		if line == memoryStalenessNote {
			return true
		}
	}
	return false
}

// addLoadedMemories folds tool-retrieved memories into the turn pool that is
// persisted on the user message as additional_context for later turns.
func (c *chatContext) addLoadedMemories(memories []*models.Memory) {
	if c == nil || len(memories) == 0 {
		return
	}

	seenID := make(map[string]struct{}, len(c.liveMemories))
	for _, mem := range c.liveMemories {
		if mem == nil {
			continue
		}
		seenID[mem.ID.String()] = struct{}{}
	}

	seenContentScope := make(map[string]struct{}, len(c.memories))
	for _, line := range c.memories {
		if line == memoryStalenessNote || isNonMemoryContextLine(line) {
			continue
		}
		scope := ""
		if mem := matchLiveMemoryByFormattedContent(line, c.liveMemories); mem != nil {
			scope = mem.Scope
		}
		key := loadedMemoryPoolKey(memoryutil.StripMemoryContextMetadata(line), scope)
		if key != "" {
			seenContentScope[key] = struct{}{}
		}
	}

	addedMemoryLine := false
	for _, mem := range memories {
		if mem == nil {
			continue
		}
		if _, exists := seenID[mem.ID.String()]; exists {
			continue
		}
		key := loadedMemoryPoolKey(mem.Content, mem.Scope)
		if key == "" {
			continue
		}
		if _, exists := seenContentScope[key]; exists {
			continue
		}
		seenID[mem.ID.String()] = struct{}{}
		seenContentScope[key] = struct{}{}
		c.liveMemories = append(c.liveMemories, mem)

		if !addedMemoryLine && !c.hasMemoryStalenessNote() {
			c.memories = append(c.memories, memoryStalenessNote)
		}
		c.memories = append(c.memories, formatMemoryForContext(mem))
		addedMemoryLine = true
	}
}

func isNonMemoryContextLine(line string) bool {
	return strings.HasPrefix(line, "The user's name is ")
}

func loadedMemoryPoolKey(content, scope string) string {
	norm := memoryutil.NormalizeContentForDedupe(content)
	if norm == "" {
		return ""
	}
	return norm + "\x00" + normalizeMemoryScope(scope)
}
