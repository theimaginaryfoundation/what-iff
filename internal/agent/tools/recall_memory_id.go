package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// memorySourceLabel is the citation / hop-target form emitted in investigate.sources and
// prefixed onto search/investigate memory lines. Full UUID so related/origin/fetch can round-trip
// without a second lookup ambiguity (short prefixes collide).
func memorySourceLabel(id uuid.UUID) string {
	if id == uuid.Nil {
		return "memory"
	}
	return "memory:" + id.String()
}

// normalizeMemoryTarget strips an optional "memory:" prefix (case-insensitive) and surrounding space.
func normalizeMemoryTarget(raw string) string {
	s := strings.TrimSpace(raw)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "memory:") {
		return strings.TrimSpace(s[len("memory:"):])
	}
	return s
}

func isHexToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.Is(unicode.ASCII_Hex_Digit, r) {
			return false
		}
	}
	return true
}

// resolveMemory looks up a memory by full UUID, "memory:<uuid>", or a unique hex ID prefix
// (e.g. the short form agents historically copied from sources: "memory:df3e519d").
func (t *RecallTool) resolveMemory(ctx context.Context, userID uuid.UUID, target string) (*models.Memory, error) {
	token := normalizeMemoryTarget(target)
	if token == "" {
		return nil, fmt.Errorf("target must be a memory ID (UUID or memory:<uuid>)")
	}

	if id, err := uuid.Parse(token); err == nil {
		mem, err := t.store.GetMemory(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if mem == nil {
			return nil, fmt.Errorf("memory %q not found", target)
		}
		return mem, nil
	}

	// Short / partial hex prefix — resolve uniquely among the user's active memories.
	compact := strings.ReplaceAll(strings.ToLower(token), "-", "")
	if !isHexToken(compact) || len(compact) < 8 {
		return nil, fmt.Errorf("target must be a valid memory ID (UUID or memory:<uuid>)")
	}
	mem, err := t.store.GetMemoryByIDPrefix(ctx, userID, compact)
	if err != nil {
		return nil, err
	}
	if mem == nil {
		return nil, fmt.Errorf("memory %q not found", target)
	}
	return mem, nil
}
