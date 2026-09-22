package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// ExpressionCandidatesMode tags Job.Progress payloads for candidate runs so clients can tell them
// apart from default-grid runs (both use JobTypeExpressionGrid and share the media-job slot).
const ExpressionCandidatesMode = "candidates"

// ErrExpressionReferenceImageNotFound is returned when the requested reference image is missing,
// owned by another user, or not an image.
var ErrExpressionReferenceImageNotFound = errors.New("reference image not found")

// ErrExpressionImagesDisabled is returned when the personality's image style is "none".
var ErrExpressionImagesDisabled = errors.New("image generation is disabled for this personality (image style is none)")

// ExpressionCandidate is one generated portrait that has been uploaded to the gallery
// (pinned to the personality) but not assigned to an expression slot.
type ExpressionCandidate struct {
	ExpressionKey string    `json:"expression_key"`
	ImageID       uuid.UUID `json:"image_id"`
}

// ExpressionCandidatesProgress is the Job.Progress payload for a candidate run. It is written at
// enqueue (mode + request) and rewritten with Candidates just before the job completes.
type ExpressionCandidatesProgress struct {
	Mode             string                `json:"mode"`
	Expressions      []string              `json:"expressions"`
	ReferenceImageID *uuid.UUID            `json:"reference_image_id,omitempty"`
	Candidates       []ExpressionCandidate `json:"candidates,omitempty"`
}

// EnqueueExpressionCandidatesJob starts a background expression_grid job that generates nine
// portraits for caller-chosen keys (row-major) without assigning them. referenceImageID, when set,
// grounds both the likeness pass and the image model. The caller reviews the candidates and assigns
// the keepers through the regular expression upsert endpoint.
func (a *Agent) EnqueueExpressionCandidatesJob(ctx context.Context, userID, personalityID uuid.UUID, keys []string, referenceImageID *uuid.UUID) (*models.Job, error) {
	if a == nil || a.ds == nil {
		return nil, fmt.Errorf("expression candidates job: agent not configured")
	}
	if len(keys) != 9 {
		return nil, fmt.Errorf("expression candidates job: expected 9 keys, got %d", len(keys))
	}

	person, err := a.ds.GetPersonality(ctx, userID, personalityID)
	if err != nil {
		return nil, err
	}
	if person.ImageStyle == imageStyleNone {
		return nil, ErrExpressionImagesDisabled
	}
	if referenceImageID != nil {
		att, err := a.ds.GetFileAttachment(ctx, userID, *referenceImageID)
		if err != nil || att == nil || !strings.HasPrefix(strings.ToLower(att.FileType), "image/") {
			return nil, ErrExpressionReferenceImageNotFound
		}
	}

	active, err := a.ds.FindActivePersonalityMediaJob(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find active personality media job: %w", err)
	}
	if active != nil {
		return nil, &ErrPersonalityMediaJobActive{Job: active}
	}

	keys = append([]string(nil), keys...)
	progress, err := json.Marshal(ExpressionCandidatesProgress{
		Mode:             ExpressionCandidatesMode,
		Expressions:      keys,
		ReferenceImageID: referenceImageID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode expression candidates progress: %w", err)
	}

	newJob, err := a.ds.CreateJob(ctx, userID, models.Job{
		JobType:   JobTypeExpressionGrid,
		Reference: personalityID.String(),
		Status:    models.JobStatusPending,
		Progress:  string(progress),
	})
	if err != nil {
		return nil, fmt.Errorf("create expression candidates job: %w", err)
	}

	detachedCtx, ok := middleware.CopyUserToIDContext(ctx, context.Background())
	if !ok {
		return nil, errors.New("user ID not found in context")
	}

	jobID := newJob.ID
	go a.runPersonalityMediaJob(detachedCtx, newJob, func(runCtx context.Context) (uuid.UUID, error) {
		candidates, err := a.GenerateExpressionCandidates(runCtx, userID, personalityID, keys, referenceImageID)
		if err != nil {
			return uuid.Nil, err
		}
		done, err := json.Marshal(ExpressionCandidatesProgress{
			Mode:             ExpressionCandidatesMode,
			Expressions:      keys,
			ReferenceImageID: referenceImageID,
			Candidates:       candidates,
		})
		if err == nil {
			err = a.ds.UpdateJobProgress(runCtx, userID, jobID, string(done))
		}
		if err != nil {
			a.deleteExpressionCandidates(runCtx, userID, candidates)
			return uuid.Nil, fmt.Errorf("record expression candidates: %w", err)
		}
		return personalityID, nil
	})

	return newJob, nil
}

// GenerateExpressionCandidates generates a 3×3 grid for nine row-major keys and uploads each cell
// as a personality-pinned gallery image, without touching expression slots. When referenceImageID
// is set, the image is sent to both the likeness pass and the image model (falling back to
// prompt-only generation if the reference call is rejected). On a mid-upload failure the
// already-uploaded candidates are deleted so a failed run leaves nothing behind.
// Cost matches GenerateDefaultExpressionGrid (~nano chat + one medium image); not quota-metered.
func (a *Agent) GenerateExpressionCandidates(ctx context.Context, userID, personalityID uuid.UUID, keys []string, referenceImageID *uuid.UUID) ([]ExpressionCandidate, error) {
	if err := a.checkExpressionGridConfigured(); err != nil {
		return nil, err
	}

	person, err := a.ds.GetPersonality(ctx, userID, personalityID)
	if err != nil {
		return nil, fmt.Errorf("get personality: %w", err)
	}
	if person == nil {
		return nil, fmt.Errorf("personality not found")
	}
	if person.ImageStyle == imageStyleNone {
		return nil, ErrExpressionImagesDisabled
	}

	ctx = telemetry.WithCallPath(ctx, telemetry.CallPathExpressionGrid)

	ref := expressionGridReference{sendToImageModel: true}
	if referenceImageID != nil {
		ref.bytes, ref.mime = a.loadExpressionReferenceImage(ctx, userID, *referenceImageID)
	}

	cells, err := a.generateExpressionGridCells(ctx, person, keys, ref)
	if err != nil {
		return nil, err
	}

	out := make([]ExpressionCandidate, 0, len(keys))
	for i, key := range keys {
		imgID, err := a.uploadExpressionCellAttachment(ctx, userID, personalityID, key, cells[i])
		if err != nil {
			a.deleteExpressionCandidates(ctx, userID, out)
			return nil, fmt.Errorf("expression %q: %w", key, err)
		}
		out = append(out, ExpressionCandidate{ExpressionKey: key, ImageID: imgID})
	}
	return out, nil
}

// deleteExpressionCandidates best-effort removes uploaded candidate images (stored objects + row).
func (a *Agent) deleteExpressionCandidates(ctx context.Context, userID uuid.UUID, candidates []ExpressionCandidate) {
	for _, c := range candidates {
		if att, err := a.ds.GetFileAttachment(ctx, userID, c.ImageID); err == nil && att != nil && a.fileStore != nil {
			if att.S3Key != "" {
				if err := a.fileStore.DeleteFile(ctx, att.S3Key); err != nil {
					a.logger.Warn("expression candidates: failed to delete candidate object",
						zap.String("s3_key", att.S3Key),
						zap.Error(err))
				}
			}
			_ = a.fileStore.DeleteFile(ctx, storage.FileKeyForImageThumbnail(userID, c.ImageID))
		}
		if err := a.ds.DeleteFileAttachment(ctx, userID, c.ImageID); err != nil {
			a.logger.Warn("expression candidates: failed to delete candidate after failure",
				zap.String("image_id", c.ImageID.String()),
				zap.Error(err))
		}
	}
}
