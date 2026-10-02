package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Scratchpad optimistic concurrency (issue #254).
//
// A personality's scratchpad is shared by every chat with that personality, and a checkpoint
// rewrites it wholesale from the scratchpad its turn started with. Two chats checkpointing at once
// would otherwise each overwrite the other's update. So a checkpoint's write is conditional on the
// scratchpad revision its turn read; on a conflict the update is generated once more against the
// latest scratchpad and written conditionally on that. If it conflicts again the update is skipped
// (logged), never forced over the newer content.

// scratchpadStore is the slice of the datastore a checkpoint's scratchpad write uses.
type scratchpadStore interface {
	GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error)
	UpdatePersonalityScratchpadIfRevision(ctx context.Context, userID, personalityID uuid.UUID, content string, expectedRevision int) (*models.Personality, error)
}

// scratchpadGenerateFunc produces an updated scratchpad. latest is nil on the first attempt, which
// builds on the scratchpad the turn started with; on the retry after a conflict it is the newer
// personality state, and the update must build on latest.Scratchpad instead.
type scratchpadGenerateFunc func(ctx context.Context, latest *models.Personality) (ScratchpadUpdate, error)

// concurrentScratchpadNote tells the model on a conflict retry that the scratchpad moved on while
// this conversation ran, and gives it the version to build on.
func concurrentScratchpadNote(latest string) string {
	return "The scratchpad was updated by another conversation after this one began. " +
		"Its latest version is below and replaces the one shown earlier: build your update on it, " +
		"keeping its content unless this conversation supersedes it.\n\nLATEST SCRATCHPAD:\n" + latest
}

// appendScratchpadUpdateTurn appends the scratchpad-update request to a Claude-path context as one
// user turn; on a conflict retry the latest scratchpad leads it. (A scratchpad segment would be
// hoisted into the system blocks as "current scratchpad", which the note is not.)
func appendScratchpadUpdateTurn(mc *provider.ModelContext, latest *models.Personality, prompt string) {
	if latest != nil {
		prompt = concurrentScratchpadNote(latest.Scratchpad) + "\n\n" + prompt
	}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, prompt, false)
}

// commitScratchpadUpdate generates a scratchpad update and writes it only if the scratchpad is
// still at baseRevision, the revision the content was based on. On a conflict it reloads the
// personality, regenerates against the latest scratchpad and writes conditionally on that
// revision; a second conflict skips the update with an error wrapping ErrScratchpadConflict.
func commitScratchpadUpdate(
	ctx context.Context,
	store scratchpadStore,
	logger *zap.Logger,
	userID, personalityID uuid.UUID,
	baseRevision int,
	generate scratchpadGenerateFunc,
) (ScratchpadUpdate, error) {
	update, err := generate(ctx, nil)
	if err != nil {
		return ScratchpadUpdate{}, err
	}
	_, err = store.UpdatePersonalityScratchpadIfRevision(ctx, userID, personalityID, update.Content, baseRevision)
	if err == nil {
		return update, nil
	}
	if !errors.Is(err, datastore.ErrScratchpadConflict) {
		return ScratchpadUpdate{}, fmt.Errorf("failed to save updated scratchpad: %w", err)
	}

	latest, err := store.GetPersonality(ctx, userID, personalityID)
	if err != nil {
		return ScratchpadUpdate{}, fmt.Errorf("failed to reload scratchpad after a concurrent update: %w", err)
	}
	logger.Info("scratchpad changed concurrently; regenerating the update against the latest version",
		zap.String("personality_id", personalityID.String()),
		zap.Int("base_revision", baseRevision),
		zap.Int("latest_revision", latest.ScratchpadRevision))
	update, err = generate(ctx, latest)
	if err != nil {
		return ScratchpadUpdate{}, fmt.Errorf("regenerate scratchpad after a concurrent update: %w", err)
	}
	_, err = store.UpdatePersonalityScratchpadIfRevision(ctx, userID, personalityID, update.Content, latest.ScratchpadRevision)
	if errors.Is(err, datastore.ErrScratchpadConflict) {
		logger.Warn("scratchpad changed concurrently again; skipping this update rather than overwriting it",
			zap.String("personality_id", personalityID.String()),
			zap.Int("latest_revision", latest.ScratchpadRevision))
		return ScratchpadUpdate{}, fmt.Errorf("scratchpad update skipped: %w", err)
	}
	if err != nil {
		return ScratchpadUpdate{}, fmt.Errorf("failed to save updated scratchpad: %w", err)
	}
	return update, nil
}
