package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// PurgeChatWorkspace removes a deleted conversation's chat/ workspace files and their stored
// objects. Best effort: call it after the conversation is deleted; failures are logged.
func (a *Agent) PurgeChatWorkspace(ctx context.Context, userID, chatID uuid.UUID) {
	a.purgeWorkspaceRoot(ctx, userID, models.WorkspaceRootChat, chatID)
}

// PurgePersonalityWorkspace removes a deleted personality's agent/ notebook and its stored
// objects. Best effort: call it after the personality is deleted; failures are logged.
func (a *Agent) PurgePersonalityWorkspace(ctx context.Context, userID, personalityID uuid.UUID) {
	a.purgeWorkspaceRoot(ctx, userID, models.WorkspaceRootAgent, personalityID)
}

func (a *Agent) purgeWorkspaceRoot(ctx context.Context, userID uuid.UUID, root string, rootRef uuid.UUID) {
	if a == nil || a.workspaceTool == nil {
		return
	}
	if err := a.workspaceTool.PurgeRoot(context.WithoutCancel(ctx), userID, root, rootRef); err != nil {
		a.logger.Warn("failed to purge workspace files",
			zap.String("root", root), zap.String("root_ref", rootRef.String()), zap.Error(err))
	}
}
