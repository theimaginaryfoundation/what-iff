package datastore

import (
	"context"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entchatmessage "github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// toJobModel converts an ent Job entity to a Job model
func toJobModel(e *ent.Job) *models.Job {
	var resultID *uuid.UUID
	if e.ResultID != nil {
		id := *e.ResultID
		resultID = &id
	}

	return &models.Job{
		ID:          e.ID,
		UserID:      e.Edges.Owner.ID,
		JobType:     e.JobType,
		Reference:   e.Reference,
		Status:      models.JobStatus(e.Status),
		Error:       e.Error,
		ResultID:    resultID,
		DraftDeltas: append([]string(nil), e.DraftDeltas...),
		Progress:    e.Progress,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

// CreateJob creates a new background job for a user
func (d *Datastore) CreateJob(ctx context.Context, userID uuid.UUID, jobModel models.Job) (*models.Job, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Create job
	jobCreate := tx.Job.Create().
		SetJobType(jobModel.JobType).
		SetReference(jobModel.Reference).
		SetStatus(job.Status(jobModel.Status)).
		SetOwnerID(userID)

	if jobModel.Error != "" {
		jobCreate.SetError(jobModel.Error)
	}

	if jobModel.ResultID != nil {
		jobCreate.SetNillableResultID(jobModel.ResultID)
	}

	if jobModel.DraftDeltas != nil {
		jobCreate.SetDraftDeltas(jobModel.DraftDeltas)
	}

	if jobModel.Progress != "" {
		jobCreate.SetProgress(jobModel.Progress)
	}

	entJob, err := jobCreate.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load the owner relationship
	entJob, err = tx.Job.Query().
		Where(job.ID(entJob.ID)).
		WithOwner().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("job.owner_load_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toJobModel(entJob), nil
}

// ListJobs returns a paginated list of jobs filtered by the provided criteria
func (d *Datastore) ListJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.JobFilters) (*models.PaginatedResponse, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Build query with user authorization
	query := tx.Job.Query().
		Where(
			job.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner()

	// Apply filters if provided
	if filters.JobType != nil && *filters.JobType != "" {
		query = query.Where(job.JobTypeEQ(*filters.JobType))
	}

	if filters.Reference != nil && *filters.Reference != "" {
		query = query.Where(job.ReferenceEQ(*filters.Reference))
	}

	if filters.Status != nil {
		query = query.Where(job.StatusEQ(job.Status(*filters.Status)))
	}

	// Get total count
	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "jobs"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Apply pagination
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	offset := (pageNum - 1) * pageSize
	query = query.
		Offset(offset).
		Limit(pageSize).
		Order(ent.Desc(job.FieldCreatedAt))

	// Execute query
	entJobs, err := query.All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Convert to model types
	jobModels := make([]any, len(entJobs))
	for i, entJob := range entJobs {
		jobModels[i] = toJobModel(entJob)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return &models.PaginatedResponse{
		Results:    jobModels,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

// GetJob retrieves a single job by ID
func (d *Datastore) GetJob(ctx context.Context, userID, id uuid.UUID) (*models.Job, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Query job with authorization check
	entJob, err := tx.Job.Query().
		Where(
			job.ID(id),
			job.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("job.not_found_or_unauthorized", "JobID", id.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrJobNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toJobModel(entJob), nil
}

// UpdateJob updates an existing job
func (d *Datastore) UpdateJob(ctx context.Context, userID uuid.UUID, jobModel models.Job) (*models.Job, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if job exists and belongs to the user
	exists, err := tx.Job.Query().
		Where(
			job.ID(jobModel.ID),
			job.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !exists {
		d.logger.Error(i18n.T2("job.not_found_or_unauthorized", "JobID", jobModel.ID.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrJobNotFound
	}

	// Update job
	jobUpdate := tx.Job.UpdateOneID(jobModel.ID).
		SetJobType(jobModel.JobType).
		SetReference(jobModel.Reference).
		SetStatus(job.Status(jobModel.Status))

	if jobModel.Error != "" {
		jobUpdate.SetError(jobModel.Error)
	} else {
		jobUpdate.ClearError()
	}

	if jobModel.ResultID != nil {
		jobUpdate.SetNillableResultID(jobModel.ResultID)
	} else {
		jobUpdate.ClearResultID()
	}

	// Preserve draft_deltas unless explicitly set by caller.
	if len(jobModel.DraftDeltas) > 0 {
		jobUpdate.SetDraftDeltas(jobModel.DraftDeltas)
	}

	entJob, err := jobUpdate.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load the owner relationship
	entJob, err = tx.Job.Query().
		Where(job.ID(entJob.ID)).
		WithOwner().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("job.owner_load_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toJobModel(entJob), nil
}

// UpdateJobStatus updates just the status of a job
func (d *Datastore) UpdateJobStatus(ctx context.Context, userID, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if job exists and belongs to the user
	exists, err := tx.Job.Query().
		Where(
			job.ID(id),
			job.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !exists {
		d.logger.Error(i18n.T2("job.not_found_or_unauthorized", "JobID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrJobNotFound
	}

	// Update job status
	jobUpdate := tx.Job.UpdateOneID(id).
		SetStatus(job.Status(status))

	if errorMsg != "" {
		jobUpdate.SetError(errorMsg)
	} else {
		jobUpdate.ClearError()
	}

	entJob, err := jobUpdate.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "job status"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load the owner relationship
	entJob, err = tx.Job.Query().
		Where(job.ID(entJob.ID)).
		WithOwner().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("job.owner_load_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toJobModel(entJob), nil
}

// UpdateJobProgress writes an opaque JSON progress payload for a job owned by the user.
// It is intentionally a single scoped UPDATE (no transaction / owner pre-check round trip) because
// long-running jobs may call it frequently; ownership is enforced in the WHERE clause so a mismatched
// user simply updates zero rows.
func (d *Datastore) UpdateJobProgress(ctx context.Context, userID, id uuid.UUID, progress string) error {
	_, err := d.dbClient.Job.Update().
		Where(job.ID(id), job.HasOwnerWith(user.ID(userID))).
		SetProgress(progress).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "job progress"), zap.Error(err))
		return err
	}
	return nil
}

// AppendJobDraftDeltas appends one or more incremental text chunks to a job's draft_deltas.
// This path is intentionally scoped to a single owner-authorized row update and avoids generic
// full-row writes because streaming may flush frequently.
func (d *Datastore) AppendJobDraftDeltas(ctx context.Context, userID, id uuid.UUID, deltas []string) error {
	if len(deltas) == 0 {
		return nil
	}
	if _, err := d.dbClient.Job.Update().
		Where(job.ID(id), job.HasOwnerWith(user.ID(userID))).
		AppendDraftDeltas(deltas).
		Save(ctx); err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "job draft_deltas"), zap.Error(err))
		return err
	}
	return nil
}

// ClearJobDraftDeltas removes any in-progress draft chunks from a job.
func (d *Datastore) ClearJobDraftDeltas(ctx context.Context, userID, id uuid.UUID) error {
	_, err := d.dbClient.Job.Update().
		Where(job.ID(id), job.HasOwnerWith(user.ID(userID))).
		ClearDraftDeltas().
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "job draft_deltas"), zap.Error(err))
		return err
	}
	return nil
}

// FinalizeCancelledChatJobWithPartial atomically persists a partial assistant message (when present),
// marks the job cancelled, updates result_id, and clears draft_deltas.
func (d *Datastore) FinalizeCancelledChatJobWithPartial(
	ctx context.Context,
	userID, jobID, chatID uuid.UUID,
	generationModel, generationPersonality string,
	generationMoodID *uuid.UUID,
) (*models.Job, *uuid.UUID, error) {
	return d.finalizeChatJobWithPartial(
		ctx, userID, jobID, chatID, generationModel, generationPersonality, generationMoodID, job.StatusCancelled, "",
	)
}

// FinalizeFailedChatJobWithPartial atomically preserves streamed assistant text (when present),
// marks the job failed with failureMessage, updates result_id, and clears draft_deltas.
func (d *Datastore) FinalizeFailedChatJobWithPartial(
	ctx context.Context,
	userID, jobID, chatID uuid.UUID,
	generationModel, generationPersonality, failureMessage string,
	generationMoodID *uuid.UUID,
) (*models.Job, *uuid.UUID, error) {
	return d.finalizeChatJobWithPartial(
		ctx, userID, jobID, chatID, generationModel, generationPersonality, generationMoodID, job.StatusFailed, failureMessage,
	)
}

func (d *Datastore) finalizeChatJobWithPartial(
	ctx context.Context,
	userID, jobID, chatID uuid.UUID,
	generationModel, generationPersonality string,
	generationMoodID *uuid.UUID,
	terminalStatus job.Status,
	failureMessage string,
) (*models.Job, *uuid.UUID, error) {
	if terminalStatus != job.StatusCancelled && terminalStatus != job.StatusFailed {
		return nil, nil, fmt.Errorf("unsupported partial-response terminal status %q", terminalStatus)
	}
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	if err := d.ensureOwnedChatExistsTx(ctx, tx, userID, chatID); err != nil {
		d.rollbackTx(tx)
		return nil, nil, err
	}

	entJob, err := d.loadOwnedJobTx(ctx, tx, userID, jobID)
	if err != nil {
		d.rollbackTx(tx)
		return nil, nil, err
	}

	// Phase 1: idempotency fast-path for already-terminal jobs.
	if entJob.Status == terminalStatus {
		resultID, iErr := d.finalizeTerminalPartialIdempotentTx(ctx, tx, entJob)
		if iErr != nil {
			d.rollbackTx(tx)
			return nil, nil, iErr
		}
		updatedJob, qErr := d.loadJobWithOwnerTx(ctx, tx, jobID)
		if qErr != nil {
			d.rollbackTx(tx)
			return nil, nil, qErr
		}
		if err := tx.Commit(); err != nil {
			d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
			return nil, nil, err
		}
		return toJobModel(updatedJob), resultID, nil
	}

	// Phase 2: consume current draft deltas into a partial assistant message (if any).
	partialText := strings.Join(entJob.DraftDeltas, "")
	resultID, err := d.consumeDraftDeltasToMessageTx(
		ctx, tx, chatID, partialText, generationModel, generationPersonality, generationMoodID,
	)
	if err != nil {
		d.rollbackTx(tx)
		return nil, nil, err
	}

	// Phase 3: mark job terminal + clear deltas.
	if err := d.updateJobToTerminalTx(ctx, tx, userID, jobID, resultID, terminalStatus, failureMessage); err != nil {
		d.rollbackTx(tx)
		return nil, nil, err
	}

	updatedJob, err := d.loadJobWithOwnerTx(ctx, tx, jobID)
	if err != nil {
		d.rollbackTx(tx)
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, nil, err
	}

	return toJobModel(updatedJob), resultID, nil
}

func (d *Datastore) ensureOwnedChatExistsTx(ctx context.Context, tx *ent.Tx, userID, chatID uuid.UUID) error {
	chatExists, err := tx.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !chatExists {
		return ErrChatNotFound
	}
	return nil
}

func (d *Datastore) loadOwnedJobTx(ctx context.Context, tx *ent.Tx, userID, jobID uuid.UUID) (*ent.Job, error) {
	entJob, err := tx.Job.Query().
		Where(job.ID(jobID), job.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return entJob, nil
}

func (d *Datastore) finalizeTerminalPartialIdempotentTx(ctx context.Context, tx *ent.Tx, entJob *ent.Job) (*uuid.UUID, error) {
	if len(entJob.DraftDeltas) > 0 {
		if _, err := tx.Job.UpdateOneID(entJob.ID).SetDraftDeltas([]string{}).Save(ctx); err != nil {
			return nil, err
		}
	}
	if entJob.ResultID == nil {
		return nil, nil
	}
	id := *entJob.ResultID
	return &id, nil
}

func (d *Datastore) consumeDraftDeltasToMessageTx(
	ctx context.Context,
	tx *ent.Tx,
	chatID uuid.UUID,
	partialText, generationModel, generationPersonality string,
	generationMoodID *uuid.UUID,
) (*uuid.UUID, error) {
	if strings.TrimSpace(partialText) == "" {
		return nil, nil
	}
	createMsg := tx.ChatMessage.Create().
		SetMessage(partialText).
		SetOrigin(entchatmessage.OriginAssistant).
		SetReadStatus(entchatmessage.ReadStatusUnread).
		SetChatID(chatID)
	if generationModel != "" {
		createMsg.SetGenerationModel(generationModel)
	}
	if generationPersonality != "" {
		createMsg.SetGenerationPersonality(generationPersonality)
	}
	if generationMoodID != nil && *generationMoodID != uuid.Nil {
		createMsg.SetGenerationMoodID(*generationMoodID)
	}
	entMsg, err := createMsg.Save(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Chat.UpdateOneID(chatID).SetLastMessageTime(entMsg.SentAt).Save(ctx); err != nil {
		return nil, err
	}
	msgID := entMsg.ID
	return &msgID, nil
}

func (d *Datastore) updateJobToTerminalTx(
	ctx context.Context,
	tx *ent.Tx,
	userID, jobID uuid.UUID,
	resultID *uuid.UUID,
	terminalStatus job.Status,
	failureMessage string,
) error {
	jobUpdate := tx.Job.Update().
		Where(job.ID(jobID), job.HasOwnerWith(user.ID(userID))).
		SetStatus(terminalStatus).
		SetDraftDeltas([]string{})
	switch terminalStatus {
	case job.StatusFailed:
		jobUpdate.SetError(failureMessage)
	case job.StatusCancelled:
		jobUpdate.SetError("")
	default:
		return fmt.Errorf("unsupported partial-response terminal status %q", terminalStatus)
	}
	if resultID != nil {
		jobUpdate.SetNillableResultID(resultID)
	} else {
		jobUpdate.ClearResultID()
	}
	_, err := jobUpdate.Save(ctx)
	return err
}

func (d *Datastore) loadJobWithOwnerTx(ctx context.Context, tx *ent.Tx, jobID uuid.UUID) (*ent.Job, error) {
	return tx.Job.Query().
		Where(job.ID(jobID)).
		WithOwner().
		Only(ctx)
}

func (d *Datastore) rollbackTx(tx *ent.Tx) {
	if rerr := tx.Rollback(); rerr != nil {
		d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
	}
}

// SetJobResult sets the result ID for a completed job
func (d *Datastore) SetJobResult(ctx context.Context, userID, id uuid.UUID, resultID uuid.UUID) (*models.Job, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if job exists and belongs to the user
	exists, err := tx.Job.Query().
		Where(
			job.ID(id),
			job.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !exists {
		d.logger.Error(i18n.T2("job.not_found_or_unauthorized", "JobID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrJobNotFound
	}

	// Update job result and set status to complete
	resultIDPtr := &resultID
	entJob, err := tx.Job.UpdateOneID(id).
		SetNillableResultID(resultIDPtr).
		SetStatus(job.StatusComplete).
		Save(ctx)

	if err != nil {
		d.logger.Error(i18n.T("job.set_result_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load the owner relationship
	entJob, err = tx.Job.Query().
		Where(job.ID(entJob.ID)).
		WithOwner().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("job.owner_load_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toJobModel(entJob), nil
}

// DeleteJob deletes a job by ID
func (d *Datastore) DeleteJob(ctx context.Context, userID, id uuid.UUID) error {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if job exists and belongs to the user
	exists, err := tx.Job.Query().
		Where(
			job.ID(id),
			job.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if !exists {
		d.logger.Error(i18n.T2("job.not_found_or_unauthorized", "JobID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return ErrJobNotFound
	}

	// Delete job
	err = tx.Job.DeleteOneID(id).Exec(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "job"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}

	return nil
}

// FindActivePersonalityMediaJob returns the newest non-terminal personality
// background job for the user, if any.
func (d *Datastore) FindActivePersonalityMediaJob(ctx context.Context, userID uuid.UUID) (*models.Job, error) {
	j, err := d.dbClient.Job.Query().
		Where(
			job.HasOwnerWith(user.ID(userID)),
			job.JobTypeIn(
				"expression_grid",
				"personality_portrait",
				"personality_generation",
			),
			job.StatusNotIn(job.StatusComplete, job.StatusCancelled, job.StatusFailed),
		).
		Order(job.ByCreatedAt(sql.OrderDesc())).
		WithOwner().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toJobModel(j), nil
}

// FindLatestActiveChatMessageJob returns the newest non-terminal chat_message job for the given user message id, if any.
func (d *Datastore) FindLatestActiveChatMessageJob(ctx context.Context, userID, userMessageID uuid.UUID) (*models.Job, error) {
	ref := userMessageID.String()
	j, err := d.dbClient.Job.Query().
		Where(
			job.HasOwnerWith(user.ID(userID)),
			job.JobTypeEQ("chat_message"),
			job.ReferenceEQ(ref),
			job.StatusNotIn(job.StatusComplete, job.StatusCancelled, job.StatusFailed),
		).
		Order(job.ByCreatedAt(sql.OrderDesc())).
		WithOwner().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toJobModel(j), nil
}

// FindActivePersonalityGenerationJob returns the newest non-terminal personality_generation
// job for the given flow and user, if any.
func (d *Datastore) FindActivePersonalityGenerationJob(ctx context.Context, userID, flowID uuid.UUID) (*models.Job, error) {
	ref := flowID.String()
	j, err := d.dbClient.Job.Query().
		Where(
			job.HasOwnerWith(user.ID(userID)),
			job.JobTypeEQ("personality_generation"),
			job.ReferenceEQ(ref),
			job.StatusNotIn(job.StatusComplete, job.StatusCancelled, job.StatusFailed),
		).
		Order(job.ByCreatedAt(sql.OrderDesc())).
		WithOwner().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toJobModel(j), nil
}
