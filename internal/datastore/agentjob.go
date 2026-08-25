package datastore

import (
	"context"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/agentjob"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func normalizeAgentJobDatastoreUnexpectedError(err error) error {
	if err == nil {
		return nil
	}

	// Never leak raw Ent/DB errors past the datastore boundary.
	// We still log the original error at the call site for observability.
	return ErrInternalDatastore
}

func toAgentJobModel(e *ent.AgentJob) *models.AgentJob {
	if e == nil {
		return nil
	}

	userID := uuid.Nil
	if e.Edges.Owner != nil {
		userID = e.Edges.Owner.ID
	}

	var chatID *uuid.UUID
	if e.Edges.Chat != nil {
		id := e.Edges.Chat.ID
		chatID = &id
	}
	personalityID := e.PersonalityID
	modelID := e.ModelID

	title := e.Title
	schedule := e.Schedule
	runAt := e.RunAt
	nextRunAt := e.NextRunAt
	lastRunAt := e.LastRunAt

	lastError := ""
	if e.LastError != nil {
		lastError = *e.LastError
	}

	var rituals []*models.Ritual
	if len(e.Edges.Rituals) > 0 {
		rituals = make([]*models.Ritual, len(e.Edges.Rituals))
		for i, r := range e.Edges.Rituals {
			rituals[i] = toRitualModel(r)
		}
	}

	return &models.AgentJob{
		ID:            e.ID,
		UserID:        userID,
		ChatID:        chatID,
		PersonalityID: personalityID,
		ModelID:       modelID,
		Title:         title,
		Prompt:        e.Prompt,
		ScheduleInput: e.ScheduleInput,
		ScheduleType:  models.AgentJobScheduleType(e.ScheduleType),
		Schedule:      schedule,
		RunAt:         runAt,
		Timezone:      e.Timezone,
		Status:        models.AgentJobStatus(e.Status),
		NextRunAt:     nextRunAt,
		LastRunAt:     lastRunAt,
		LastError:     lastError,
		RunCount:      e.RunCount,
		Rituals:       rituals,
		CreatedAt:     e.CreatedAt,
		UpdatedAt:     e.UpdatedAt,
	}
}

// CreateAgentJob creates a new AgentJob for a user.
func (d *Datastore) CreateAgentJob(ctx context.Context, userID uuid.UUID, jobModel models.AgentJob) (*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	userExists, err := tx.User.Query().Where(user.ID(userID)).Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "user"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if !userExists {
		tx.Rollback()
		return nil, ErrUnauthorized
	}

	create := tx.AgentJob.Create().
		SetOwnerID(userID).
		SetPrompt(jobModel.Prompt).
		SetScheduleInput(jobModel.ScheduleInput).
		SetScheduleType(agentjob.ScheduleType(jobModel.ScheduleType)).
		SetTimezone(jobModel.Timezone).
		SetStatus(agentjob.Status(jobModel.Status)).
		SetRunCount(jobModel.RunCount)

	if jobModel.Title != nil {
		create.SetNillableTitle(jobModel.Title)
	}
	if jobModel.Schedule != nil {
		create.SetNillableSchedule(jobModel.Schedule)
	}
	if jobModel.RunAt != nil {
		create.SetNillableRunAt(jobModel.RunAt)
	}
	if jobModel.NextRunAt != nil {
		create.SetNillableNextRunAt(jobModel.NextRunAt)
	}
	if jobModel.ChatID != nil {
		chatExists, err := tx.Chat.Query().
			Where(chat.ID(*jobModel.ChatID), chat.HasOwnerWith(user.ID(userID))).
			Exist(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
			tx.Rollback()
			return nil, normalizeAgentJobDatastoreUnexpectedError(err)
		}
		if !chatExists {
			tx.Rollback()
			return nil, ErrChatNotFound
		}
		create.SetChatID(*jobModel.ChatID)
	}
	overridePatch := models.SetAgentJobOverridesPatch{}
	if jobModel.PersonalityID != nil {
		overridePatch.UpdatePersonality = true
		overridePatch.PersonalityID = jobModel.PersonalityID
	}
	if jobModel.ModelID != nil {
		overridePatch.UpdateModel = true
		overridePatch.ModelID = jobModel.ModelID
	}
	if overridePatch.UpdatePersonality || overridePatch.UpdateModel {
		if err := validateAgentJobOverrideIDs(ctx, tx, userID, overridePatch); err != nil {
			if err == ErrInvalidRequestBody {
				tx.Rollback()
				return nil, err
			}
			d.logger.Error("failed to validate agent job overrides", zap.Error(err))
			tx.Rollback()
			return nil, normalizeAgentJobDatastoreUnexpectedError(err)
		}
	}
	create.SetNillablePersonalityID(jobModel.PersonalityID)
	create.SetNillableModelID(jobModel.ModelID)
	if jobModel.LastRunAt != nil {
		create.SetNillableLastRunAt(jobModel.LastRunAt)
	}
	if strings.TrimSpace(jobModel.LastError) != "" {
		create.SetLastError(jobModel.LastError)
	}

	entJob, err := create.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "agent job"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	entJob, err = tx.AgentJob.Query().
		Where(agentjob.ID(entJob.ID)).
		WithOwner().
		WithChat().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.relationships_load_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return toAgentJobModel(entJob), nil
}

// ListAgentJobs returns a paginated list of AgentJobs filtered by the provided criteria.
func (d *Datastore) ListAgentJobs(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.AgentJobFilters) (*models.PaginatedResponse, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	query := tx.AgentJob.Query().
		Where(agentjob.HasOwnerWith(user.ID(userID))).
		WithOwner().
		WithChat()

	if filters.Status != nil {
		query = query.Where(agentjob.StatusEQ(agentjob.Status(*filters.Status)))
	}
	if filters.ScheduleType != nil {
		query = query.Where(agentjob.ScheduleTypeEQ(agentjob.ScheduleType(*filters.ScheduleType)))
	}
	if filters.Query != nil && strings.TrimSpace(*filters.Query) != "" {
		q := strings.TrimSpace(*filters.Query)
		query = query.Where(
			agentjob.Or(
				agentjob.PromptContainsFold(q),
				agentjob.ScheduleInputContainsFold(q),
				agentjob.TitleContainsFold(q),
			),
		)
	}

	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "agent jobs"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (pageNum - 1) * pageSize

	entJobs, err := query.
		Offset(offset).
		Limit(pageSize).
		Order(ent.Desc(agentjob.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "agent job"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	results := make([]any, len(entJobs))
	for i, e := range entJobs {
		results[i] = toAgentJobModel(e)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return &models.PaginatedResponse{
		Results:    results,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

// GetAgentJob retrieves a single AgentJob by ID.
func (d *Datastore) GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	entJob, err := tx.AgentJob.Query().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID))).
		WithOwner().
		WithChat().
		WithRituals().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			tx.Rollback()
			return nil, ErrAgentJobNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "agent job"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return toAgentJobModel(entJob), nil
}

// shouldReactivateAgentJobAfterScheduleSave returns true when a terminal job is given a new
// schedule that still has an upcoming run, so the scheduler (which only tracks active jobs)
// should pick it up again.
func shouldReactivateAgentJobAfterScheduleSave(prev agentjob.Status, nextRunAt *time.Time) bool {
	if nextRunAt == nil {
		return false
	}
	switch prev {
	case agentjob.StatusComplete, agentjob.StatusFailed:
		return true
	default:
		return false
	}
}

// UpdateAgentJobSchedule updates schedule-related fields for an AgentJob.
func (d *Datastore) UpdateAgentJobSchedule(ctx context.Context, userID, id uuid.UUID, scheduleInput string, scheduleType models.AgentJobScheduleType, schedule *string, runAt *time.Time, timezone string, nextRunAt *time.Time) (*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	existing, err := tx.AgentJob.Query().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			tx.Rollback()
			return nil, ErrAgentJobNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "agent job"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	update := tx.AgentJob.Update().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID))).
		SetScheduleInput(scheduleInput).
		SetScheduleType(agentjob.ScheduleType(scheduleType)).
		SetTimezone(timezone)

	// Never "clear then set" the same column in one UPDATE (Postgres rejects multiple assignments).
	if schedule == nil || strings.TrimSpace(*schedule) == "" {
		update.ClearSchedule()
	} else {
		trimmed := strings.TrimSpace(*schedule)
		update.SetSchedule(trimmed)
	}

	if runAt == nil {
		update.ClearRunAt()
	} else {
		update.SetRunAt(*runAt)
	}

	if nextRunAt == nil {
		update.ClearNextRunAt()
	} else {
		update.SetNextRunAt(*nextRunAt)
	}

	if shouldReactivateAgentJobAfterScheduleSave(existing.Status, nextRunAt) {
		update = update.SetStatus(agentjob.StatusActive).ClearLastError()
	}

	affected, err := update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "agent job schedule"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if affected == 0 {
		tx.Rollback()
		return nil, ErrAgentJobNotFound
	}

	entJob, err := tx.AgentJob.Query().
		Where(agentjob.ID(id)).
		WithOwner().
		WithChat().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.relationships_load_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return toAgentJobModel(entJob), nil
}

// UpdateAgentJobStatus updates the status of an AgentJob.
func (d *Datastore) UpdateAgentJobStatus(ctx context.Context, userID, id uuid.UUID, status models.AgentJobStatus, lastError string) (*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	update := tx.AgentJob.Update().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID))).
		SetStatus(agentjob.Status(status))

	if strings.TrimSpace(lastError) == "" {
		update.ClearLastError()
	} else {
		update.SetLastError(lastError)
	}

	affected, err := update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "agent job status"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if affected == 0 {
		tx.Rollback()
		return nil, ErrAgentJobNotFound
	}

	entJob, err := tx.AgentJob.Query().
		Where(agentjob.ID(id)).
		WithOwner().
		WithChat().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.relationships_load_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return toAgentJobModel(entJob), nil
}

// UpdateAgentJobTitle updates the title of an AgentJob (or clears it when empty/nil).
func (d *Datastore) UpdateAgentJobTitle(ctx context.Context, userID, id uuid.UUID, title *string) (*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	update := tx.AgentJob.Update().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID)))
	if title == nil || strings.TrimSpace(*title) == "" {
		update.ClearTitle()
	} else {
		trimmed := strings.TrimSpace(*title)
		update.SetTitle(trimmed)
	}

	affected, err := update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "agent job title"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if affected == 0 {
		tx.Rollback()
		return nil, ErrAgentJobNotFound
	}

	entJob, err := tx.AgentJob.Query().
		Where(agentjob.ID(id)).
		WithOwner().
		WithChat().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.relationships_load_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return toAgentJobModel(entJob), nil
}

// UpdateAgentJobPrompt updates the prompt of an AgentJob.
func (d *Datastore) UpdateAgentJobPrompt(ctx context.Context, userID, id uuid.UUID, prompt string) (*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	update := tx.AgentJob.Update().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID))).
		SetPrompt(prompt)

	affected, err := update.Save(ctx)
	if err != nil {
		d.logger.Error("failed to update agent job prompt", zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if affected == 0 {
		tx.Rollback()
		return nil, ErrAgentJobNotFound
	}

	entJob, err := tx.AgentJob.Query().
		Where(agentjob.ID(id)).
		WithOwner().
		WithChat().
		Only(ctx)
	if err != nil {
		d.logger.Error("failed to load agent job relationships", zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return toAgentJobModel(entJob), nil
}

// SetAgentJobChat sets (or clears) the associated chat for an AgentJob.
func (d *Datastore) SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	update := tx.AgentJob.Update().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID)))
	if chatID == nil {
		update.ClearChat()
	} else {
		chatExists, err := tx.Chat.Query().
			Where(chat.ID(*chatID), chat.HasOwnerWith(user.ID(userID))).
			Exist(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
			tx.Rollback()
			return nil, normalizeAgentJobDatastoreUnexpectedError(err)
		}
		if !chatExists {
			tx.Rollback()
			return nil, ErrChatNotFound
		}
		update.SetChatID(*chatID)
	}

	affected, err := update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.set_chat_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if affected == 0 {
		tx.Rollback()
		return nil, ErrAgentJobNotFound
	}

	entJob, err := tx.AgentJob.Query().
		Where(agentjob.ID(id)).
		WithOwner().
		WithChat().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.relationships_load_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return toAgentJobModel(entJob), nil
}

func validateAgentJobOverrideIDs(ctx context.Context, tx *ent.Tx, userID uuid.UUID, patch models.SetAgentJobOverridesPatch) error {
	if patch.UpdatePersonality && patch.PersonalityID != nil {
		ok, err := tx.Personality.Query().
			Where(
				personality.ID(*patch.PersonalityID),
				personality.HasUserWith(user.ID(userID)),
			).
			Exist(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidRequestBody
		}
	}
	if patch.UpdateModel && patch.ModelID != nil {
		ok, err := tx.Model.Query().
			Where(
				entmodel.ID(*patch.ModelID),
				entmodel.Deleted(false),
			).
			Exist(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidRequestBody
		}
		if err := assertUserCanUseModelTx(ctx, tx, userID, *patch.ModelID); err != nil {
			return err
		}
	}
	return nil
}

func (d *Datastore) SetAgentJobOverrides(ctx context.Context, userID, id uuid.UUID, patch models.SetAgentJobOverridesPatch) (*models.AgentJob, error) {
	if !patch.UpdatePersonality && !patch.UpdateModel {
		return nil, ErrInvalidRequestBody
	}

	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	if err := validateAgentJobOverrideIDs(ctx, tx, userID, patch); err != nil {
		if err == ErrInvalidRequestBody {
			tx.Rollback()
			return nil, err
		}
		d.logger.Error("failed to validate agent job overrides", zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	update := tx.AgentJob.Update().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID)))
	if patch.UpdatePersonality {
		update.SetNillablePersonalityID(patch.PersonalityID)
	}
	if patch.UpdateModel {
		update.SetNillableModelID(patch.ModelID)
	}

	affected, err := update.Save(ctx)
	if err != nil {
		d.logger.Error("failed to set agent job overrides", zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if affected == 0 {
		tx.Rollback()
		return nil, ErrAgentJobNotFound
	}

	entJob, err := tx.AgentJob.Query().
		Where(agentjob.ID(id)).
		WithOwner().
		WithChat().
		Only(ctx)
	if err != nil {
		d.logger.Error("failed to load agent job relationships", zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	return toAgentJobModel(entJob), nil
}

// DeleteAgentJob deletes an AgentJob by ID.
func (d *Datastore) DeleteAgentJob(ctx context.Context, userID, id uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	affected, err := tx.AgentJob.Delete().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID))).
		Exec(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "agent job"), zap.Error(err))
		tx.Rollback()
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if affected == 0 {
		tx.Rollback()
		return ErrAgentJobNotFound
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	return nil
}

// ListAgentJobsForScheduler returns all agent jobs (all users) for in-process scheduling.
// This is intended for internal scheduler reconciliation only (no user scoping).
func (d *Datastore) ListAgentJobsForScheduler(ctx context.Context) ([]*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	entJobs, err := tx.AgentJob.Query().
		WithOwner().
		WithChat().
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.query_for_scheduler_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	out := make([]*models.AgentJob, 0, len(entJobs))
	for _, e := range entJobs {
		out = append(out, toAgentJobModel(e))
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	return out, nil
}

// RecordAgentJobRun updates AgentJob run metadata after an execution attempt.
func (d *Datastore) RecordAgentJobRun(ctx context.Context, userID, id uuid.UUID, lastRunAt time.Time, nextRunAt *time.Time, runError string, newStatus *models.AgentJobStatus) (*models.AgentJob, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	update := tx.AgentJob.Update().
		Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID))).
		SetLastRunAt(lastRunAt).
		AddRunCount(1)

	// Never "clear then set" next_run_at in one UPDATE.
	if nextRunAt == nil {
		update.ClearNextRunAt()
	} else {
		update.SetNextRunAt(*nextRunAt)
	}

	if strings.TrimSpace(runError) == "" {
		update.ClearLastError()
	} else {
		update.SetLastError(runError)
	}

	if newStatus != nil {
		update.SetStatus(agentjob.Status(*newStatus))
	}

	affected, err := update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.record_run_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if affected == 0 {
		tx.Rollback()
		return nil, ErrAgentJobNotFound
	}

	entJob, err := tx.AgentJob.Query().
		Where(agentjob.ID(id)).
		WithOwner().
		WithChat().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("agentjob.relationships_load_failed"), zap.Error(err))
		tx.Rollback()
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return toAgentJobModel(entJob), nil
}

// AddAgentJobRitual attaches a ritual to an agent job. The job must belong to userID; the ritual
// must exist and belong to the same user (the M2M edge alone does not enforce ritual ownership).
func (d *Datastore) AddAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.AgentJob.Query().
		Where(agentjob.ID(jobID), agentjob.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		tx.Rollback()
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if !exists {
		tx.Rollback()
		return ErrAgentJobNotFound
	}

	ritualOwned, err := tx.Ritual.Query().
		Where(entritual.ID(ritualID), entritual.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		tx.Rollback()
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if !ritualOwned {
		tx.Rollback()
		return ErrRitualNotFound
	}

	if err := tx.AgentJob.UpdateOneID(jobID).AddRitualIDs(ritualID).Exec(ctx); err != nil {
		tx.Rollback()
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return tx.Commit()
}

// RemoveAgentJobRitual detaches a ritual from an agent job. The job must belong to userID; the
// ritual must exist and belong to the same user before the edge is removed.
func (d *Datastore) RemoveAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.AgentJob.Query().
		Where(agentjob.ID(jobID), agentjob.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		tx.Rollback()
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if !exists {
		tx.Rollback()
		return ErrAgentJobNotFound
	}

	ritualOwned, err := tx.Ritual.Query().
		Where(entritual.ID(ritualID), entritual.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		tx.Rollback()
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}
	if !ritualOwned {
		tx.Rollback()
		return ErrRitualNotFound
	}

	if err := tx.AgentJob.UpdateOneID(jobID).RemoveRitualIDs(ritualID).Exec(ctx); err != nil {
		tx.Rollback()
		return normalizeAgentJobDatastoreUnexpectedError(err)
	}

	return tx.Commit()
}
