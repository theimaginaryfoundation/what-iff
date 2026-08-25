package agent

import (
	"context"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// agentTestHooks groups test-only dependency injection for *Agent. Every field
// must be nil in production binaries; tests set only what they need.
type agentTestHooks struct {
	GetMemoriesOverride func(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, personalityID uuid.UUID, userMessage string) ([]string, error)
	LoadHistoryOverride func(ctx context.Context, userID, chatID, excludeMessageID uuid.UUID, pageSize int, minDate *time.Time, logContext string) []*models.ChatMessage

	ImageRitualOpenAIInfer          func(ctx context.Context, params responses.ResponseNewParams) (prompt string, result *provider.GenerateResponse, err error)
	ImageRitualClaudeInfer          func(ctx context.Context, params anthropic.MessageNewParams) (prompt string, result *provider.GenerateResponse, err error)
	ImageRitualGenerateImagePNG     func(ctx context.Context, prompt string) (string, error)
	ImageRitualCreateChatMessage    func(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error)
	ImageRitualCreateFileAttachment func(ctx context.Context, userID uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error)
	ImageRitualPersistImage         func(ctx context.Context, userID uuid.UUID, attachment *models.FileAttachment, imageBase64 string) error
}

func (h agentTestHooks) anySet() bool {
	return h.GetMemoriesOverride != nil ||
		h.LoadHistoryOverride != nil ||
		h.ImageRitualOpenAIInfer != nil ||
		h.ImageRitualClaudeInfer != nil ||
		h.ImageRitualGenerateImagePNG != nil ||
		h.ImageRitualCreateChatMessage != nil ||
		h.ImageRitualCreateFileAttachment != nil ||
		h.ImageRitualPersistImage != nil
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
