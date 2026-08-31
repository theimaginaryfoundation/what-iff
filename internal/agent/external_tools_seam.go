package agent

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ExtraToolHandler matches the core tool handler contract.
type ExtraToolHandler func(context.Context, []byte) (string, []*models.FileAttachment, error)

// extraToolHandlersForChat, when non-nil, returns additional handlers keyed by
// tool name for the active chat.
var extraToolHandlersForChat func(a *Agent, chat *models.Chat) map[string]ExtraToolHandler

// additionalDisabledToolsForChat, when non-nil, returns tool names that should
// be hidden for this chat at runtime (for example: integration disabled by
// missing env config). It is merged into disabled_tools policy.
var additionalDisabledToolsForChat func(a *Agent, chat *models.Chat) map[string]bool

// additionalGeneratedAttachmentsForChat, when non-nil, returns a callback that
// may contribute extra tool-generated attachments once per assistant message
// after the tool loop completes (for example: a final container manifest sweep).
var additionalGeneratedAttachmentsForChat func(a *Agent, chat *models.Chat) func(context.Context) []*models.FileAttachment

// additionalDeveloperContextForChat, when non-nil, returns extra developer
// instructions appended for this chat turn.
var additionalDeveloperContextForChat func(a *Agent, chat *models.Chat) string

// onToolUseGeneratedAttachmentsForChat, when non-nil, is notified after each
// tool use with any tool-generated attachments for this chat turn.
var onToolUseGeneratedAttachmentsForChat func(a *Agent, chat *models.Chat, use provider.ToolUse, attachments []*models.FileAttachment)
