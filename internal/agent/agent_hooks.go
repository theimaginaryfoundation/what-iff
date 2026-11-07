package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// agentTestHooks groups test-only dependency injection for *Agent. Every field
// must be nil in production binaries; tests set only what they need.
type agentTestHooks struct {
	GetMemoriesOverride func(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, personalityID uuid.UUID, userMessage string) ([]string, error)
	LoadHistoryOverride func(ctx context.Context, userID, chatID, excludeMessageID uuid.UUID, pageSize int, minDate *time.Time, logContext string) []*models.ChatMessage

	// ChatTurnStore replaces the datastore behind the per-chat turn gate.
	ChatTurnStore chatTurnStore
	// ImageGenerationPreference replaces the read of the user's image vendor preference.
	ImageGenerationPreference func(ctx context.Context, userID uuid.UUID) (models.ModelProvider, error)
	// PromptStore replaces the datastore behind agent prompts (ADR 0x042); PromptTimeout replaces
	// their wait.
	PromptStore   agentPromptStore
	PromptTimeout time.Duration
}

func (h agentTestHooks) anySet() bool {
	return h.GetMemoriesOverride != nil ||
		h.LoadHistoryOverride != nil ||
		h.ChatTurnStore != nil ||
		h.ImageGenerationPreference != nil ||
		h.PromptStore != nil ||
		h.PromptTimeout != 0
}

// assertNoTestHooksInProduction panics if any test hook is set outside of `go test`.
// Call from constructors and before any code path that could observe hooks (e.g. prepareChatContext).
func assertNoTestHooksInProduction(a *Agent) {
	if testing.Testing() {
		return
	}
	if a != nil && a.testHooks.anySet() {
		panic("agent: test hooks must only be set when running tests")
	}
}
