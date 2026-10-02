package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// contextInputsHashLen is how many hex characters of a SHA-256 identify a scratchpad or summary
// version: enough to tell versions apart, short enough to keep the snapshot small.
const contextInputsHashLen = 12

// contextInputs builds the turn's input manifest: references to the memories, mode, scratchpad
// and summary the model context was built from. It records IDs and short hashes only, never
// content. Returns nil when there is nothing to record.
func contextInputs(chatCtx *chatContext, modelContext *provider.ModelContext) *modeltypes.ContextInputs {
	if chatCtx == nil {
		return nil
	}
	in := &modeltypes.ContextInputs{MemoryEnrichmentFailed: chatCtx.memoryEnrichmentFailed}

	seen := make(map[string]struct{}, len(chatCtx.liveMemories))
	for i, mem := range chatCtx.liveMemories {
		if mem == nil {
			continue
		}
		id := mem.ID.String()
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		stage := modeltypes.ContextMemoryStagePrefetch
		if i >= chatCtx.prefetchedMemoryCount {
			stage = modeltypes.ContextMemoryStageTool
		}
		in.Memories = append(in.Memories, modeltypes.ContextMemoryInput{
			ID:        id,
			Scope:     mem.Scope,
			Stage:     stage,
			Relevance: mem.Relevance,
		})
	}

	if modelContext != nil {
		for _, ref := range modelContext.MemoryRefs {
			id := strings.TrimSpace(ref.MemoryID)
			if id == "" {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			in.ReplayedMemoryIDs = append(in.ReplayedMemoryIDs, id)
		}
	}

	if chatCtx.activeMood != nil {
		in.MoodID = chatCtx.activeMood.ID.String()
	}
	if chatCtx.chat != nil {
		in.ScratchpadSHA = shortContentHash(chatCtx.chat.Scratchpad)
		in.SummarySHA = shortContentHash(chatCtx.chat.CheckpointSummary)
	}

	if len(in.Memories) == 0 && len(in.ReplayedMemoryIDs) == 0 && !in.MemoryEnrichmentFailed &&
		in.MoodID == "" && in.ScratchpadSHA == "" && in.SummarySHA == "" {
		return nil
	}
	return in
}

// shortContentHash returns the first contextInputsHashLen hex characters of the content's
// SHA-256, or "" for empty content.
func shortContentHash(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:contextInputsHashLen]
}
