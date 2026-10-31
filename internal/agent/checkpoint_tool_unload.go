package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// unloadToolsUnusedInSegment is the checkpoint's tool step (#304): once a checkpoint has
// summarized a segment (the messages sent at or after segmentStart, the previous checkpoint's
// window start; the whole chat when nil), every loaded tool, MCP or built-in, that no message of
// the segment called is unloaded, so tool definitions the conversation stopped using are not
// carried into the next segment. The model loads them again with load_tools when it needs them.
//
// It runs inside the turn, which holds the chat's turn gate, so no other turn of the chat can load
// a tool between reading the segment's calls and pruning. Failures are logged; tools stay loaded.
func (a *Agent) unloadToolsUnusedInSegment(ctx context.Context, userID, chatID uuid.UUID, segmentStart *time.Time) {
	if a.ds == nil {
		return
	}
	used, err := a.ds.ChatToolNamesUsedSince(ctx, userID, chatID, segmentStart)
	if err != nil {
		a.logger.Warn("checkpoint: failed to read the segment's tool calls; leaving loaded tools as they are",
			zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	// Loaded skills follow the same rule (ADR 0x035 §4); the skill step runs first so a skill's
	// allowed_tools are judged by the segment's calls, not by what this sweep removes.
	a.unloadSkillsUnusedInSegment(ctx, userID, chatID, segmentStart, used)
	unloaded, err := a.ds.RetainChatLoadedTools(ctx, userID, chatID, used)
	if err != nil {
		a.logger.Warn("checkpoint: failed to unload unused tools",
			zap.String("chat_id", chatID.String()), zap.Error(err))
		return
	}
	if len(unloaded) > 0 {
		a.logger.Info("checkpoint: unloaded tools the segment did not use",
			zap.String("chat_id", chatID.String()),
			zap.Strings("tools", unloaded))
	}
}

// checkpointSegmentStart is where the segment a checkpoint summarizes began: the window start the
// previous checkpoint stored, read before this one replaces it.
func checkpointSegmentStart(chatCtx *chatContext) *time.Time {
	if chatCtx == nil || chatCtx.chat == nil {
		return nil
	}
	return chatCtx.chat.LastCheckpointAt
}
