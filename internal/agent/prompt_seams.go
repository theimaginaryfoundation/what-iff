package agent

import (
	"context"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Per-chat prompt overrides. Each is a func var that is nil in this repository, so every chat gets
// What Iff's own prompt. A build that serves another app's users under that app's own voice sets
// them from an init() in a file composed into this package, the same pattern as
// external_tools_seam.go, and answers per chat. An empty answer (after trimming) also means
// "What Iff's own", so an override only has to answer for the chats it cares about.
//
// The resolved value is used exactly where the constant was: the overrides change text, never where
// it goes or what else is sent.

// baseSystemPromptNormsForChat, when non-nil, replaces What Iff's norms (baseSystemPromptNorms) in
// the base prompt of a chat turn and of a sub-agent the turn starts. The mechanics
// (baseSystemPromptMechanics) are always kept after it.
var baseSystemPromptNormsForChat func(ctx context.Context, a *Agent, chat *models.Chat) string

// checkpointSummaryInstructionsForChat, when non-nil, replaces checkpointConversationSummaryInstructions
// when a chat is compacted at a checkpoint. Imported threads (thread_rehydration.go) keep the default:
// they are summarized once, at import, before any of the chat's own configuration applies.
var checkpointSummaryInstructionsForChat func(ctx context.Context, a *Agent, chat *models.Chat) string

// scratchpadUpdatePromptForChat, when non-nil, replaces the app's scratchpadUpdatePrompt as the
// default for a chat whose personality has no ScratchpadUpdatePrompt of its own. The personality's
// own prompt still wins, and the first scratchpad (scratchpadInitialPrompt) is unchanged, exactly
// as for a personality's own prompt.
var scratchpadUpdatePromptForChat func(ctx context.Context, a *Agent, chat *models.Chat) string

// composeBaseSystemPrompt is the base prompt with norms in place of What Iff's: the norms, the
// separator, then the mechanics. Empty norms give baseSystemPrompt itself.
func composeBaseSystemPrompt(norms string) string {
	norms = strings.TrimSpace(norms)
	if norms == "" {
		return baseSystemPrompt
	}
	return norms + baseSystemPromptSeparator + baseSystemPromptMechanics
}

// chatPromptOverride asks one of the seams above, treating nil, a nil chat and a blank answer alike
// as "no override".
func (a *Agent) chatPromptOverride(ctx context.Context, seam func(context.Context, *Agent, *models.Chat) string, chat *models.Chat) string {
	if seam == nil || chat == nil {
		return ""
	}
	return strings.TrimSpace(seam(ctx, a, chat))
}

// baseSystemPromptForChat is the base prompt a chat's turns and sub-agents start with.
func (a *Agent) baseSystemPromptForChat(ctx context.Context, chat *models.Chat) string {
	return composeBaseSystemPrompt(a.chatPromptOverride(ctx, baseSystemPromptNormsForChat, chat))
}

// checkpointSummaryInstructionsFor is the instructions a chat's checkpoint summary is written with.
func (a *Agent) checkpointSummaryInstructionsFor(ctx context.Context, chat *models.Chat) string {
	if s := a.chatPromptOverride(ctx, checkpointSummaryInstructionsForChat, chat); s != "" {
		return s
	}
	return checkpointConversationSummaryInstructions
}

// defaultScratchpadUpdatePromptFor is the scratchpad update prompt for a chat whose personality has
// none of its own.
func (a *Agent) defaultScratchpadUpdatePromptFor(ctx context.Context, chat *models.Chat) string {
	if s := a.chatPromptOverride(ctx, scratchpadUpdatePromptForChat, chat); s != "" {
		return s
	}
	return scratchpadUpdatePrompt
}
