package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// TestDefaultContextBudget_MatchesConstants locks the no-op invariant: the seam's
// default budget must reproduce the exact constants the call sites used before the
// seam existed. This is the "Standard == today" reference the ADR relies on; if a
// constant changes, update the intended default here deliberately.
func TestDefaultContextBudget_MatchesConstants(t *testing.T) {
	b := defaultContextBudget()

	assert.Equal(t, checkpointMaxLastInputTokens, b.MaxLastInputTokens)
	assert.Equal(t, checkpointMaxEstimatedContextTokens, b.MaxEstimatedContextTokens)
	assert.Equal(t, checkpointMinTurnsBetweenCheckpoints, b.MinTurnsBetweenCheckpoints)
	assert.Equal(t, defaultHistoryPageSize, b.HistoryPageSize)
	assert.Equal(t, carryOverMaxTurns, b.CarryOverMaxTurns)
	assert.Equal(t, carryOverMaxTokens, b.CarryOverMaxTokens)

	// Guard the literal values too, so an accidental edit to a constant is caught.
	assert.Equal(t, 30_000, b.MaxLastInputTokens)
	assert.Equal(t, 32_000, b.MaxEstimatedContextTokens)
	assert.Equal(t, 5, b.MinTurnsBetweenCheckpoints)
	assert.Equal(t, 50, b.HistoryPageSize)
	assert.Equal(t, 3, b.CarryOverMaxTurns)
	assert.Equal(t, 500, b.CarryOverMaxTokens)
}

// TestResolveContextBudget_NilResolverIsDefault verifies that with no overlay
// resolver installed (the OSS default), every chat resolves to the fixed default.
func TestResolveContextBudget_NilResolverIsDefault(t *testing.T) {
	prev := contextBudgetForChat
	t.Cleanup(func() { contextBudgetForChat = prev })
	contextBudgetForChat = nil

	assert.Equal(t, defaultContextBudget(), resolveContextBudget(nil))
	assert.Equal(t, defaultContextBudget(), resolveContextBudget(&models.Chat{ID: uuid.New()}))
}

// TestResolveContextBudget_OverlayResolverWins proves the swap-point: when the
// overlay installs contextBudgetForChat, resolution uses it instead of the default.
// A nil chat still short-circuits to the default (the resolver is never called
// without a chat to key on).
func TestResolveContextBudget_OverlayResolverWins(t *testing.T) {
	prev := contextBudgetForChat
	t.Cleanup(func() { contextBudgetForChat = prev })

	want := contextBudget{
		MaxLastInputTokens:         110_000,
		MaxEstimatedContextTokens:  120_000,
		MinTurnsBetweenCheckpoints: 5,
		HistoryPageSize:            90,
		CarryOverMaxTurns:          8,
		CarryOverMaxTokens:         1600,
	}
	var gotChat *models.Chat
	contextBudgetForChat = func(chat *models.Chat) contextBudget {
		gotChat = chat
		return want
	}

	chat := &models.Chat{ID: uuid.New()}
	assert.Equal(t, want, resolveContextBudget(chat))
	assert.Same(t, chat, gotChat)

	// Nil chat never reaches the resolver.
	gotChat = nil
	assert.Equal(t, defaultContextBudget(), resolveContextBudget(nil))
	assert.Nil(t, gotChat)
}
