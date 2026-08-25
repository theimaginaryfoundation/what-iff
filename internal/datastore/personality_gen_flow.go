package datastore

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/ent/personalitygenflow"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// toFlowModel converts an Ent PersonalityGenFlow to the API model.
func toFlowModel(e *ent.PersonalityGenFlow) *models.PersonalityGenFlow {
	if e == nil {
		return nil
	}

	m := &models.PersonalityGenFlow{
		ID:               e.ID,
		Status:           string(e.Status),
		CurrentStep:      e.CurrentStep,
		Answers:          e.Answers,
		GeneratedPrompt:  e.GeneratedPrompt,
		GeneratedAboutMe: e.GeneratedAboutMe,
		GeneratedNames:   e.GeneratedNames,
		ImageStyle:       e.ImageStyle,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
	}

	if e.Edges.Personality != nil {
		pid := e.Edges.Personality.ID
		m.PersonalityID = &pid
	}

	if e.Edges.ReferenceImage != nil {
		refID := e.Edges.ReferenceImage.ID
		m.ReferenceImageID = &refID
		m.ReferenceImageURL = personalityExpressionImageURL(&refID)
	}

	return m
}

// GetOrCreateActiveFlow returns the most recently updated active flow (in_progress/generated), or creates one.
func (d *Datastore) GetOrCreateActiveFlow(ctx context.Context, userID uuid.UUID) (*models.PersonalityGenFlow, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Look for an existing active flow.
	existing, err := tx.PersonalityGenFlow.Query().
		Where(
			personalitygenflow.StatusIn(
				personalitygenflow.StatusInProgress,
				personalitygenflow.StatusGenerated,
			),
			personalitygenflow.HasUserWith(user.ID(userID)),
		).
		WithPersonality().
		WithReferenceImage().
		Order(ent.Desc(personalitygenflow.FieldUpdatedAt)).
		First(ctx)

	if err != nil && !ent.IsNotFound(err) {
		d.logger.Error(i18n.T1("query.failed", "Entity", "active gen flow"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if existing != nil {
		if err := tx.Commit(); err != nil {
			d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
			return nil, err
		}
		return toFlowModel(existing), nil
	}

	// No active flow — create a fresh one.
	created, err := tx.PersonalityGenFlow.Create().
		SetUserID(userID).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "gen flow"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toFlowModel(created), nil
}

// GetFlow returns a specific flow by ID, scoped to the user.
func (d *Datastore) GetFlow(ctx context.Context, userID, flowID uuid.UUID) (*models.PersonalityGenFlow, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	flow, err := tx.PersonalityGenFlow.Query().
		Where(
			personalitygenflow.ID(flowID),
			personalitygenflow.HasUserWith(user.ID(userID)),
		).
		WithPersonality().
		WithReferenceImage().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrFlowNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "gen flow"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}
	return toFlowModel(flow), nil
}

// UpdateFlow saves partial wizard progress (answers + current_step).
func (d *Datastore) UpdateFlow(ctx context.Context, userID uuid.UUID, flowID uuid.UUID, req models.UpdateFlowRequest) (*models.PersonalityGenFlow, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Fetch the current flow (also verifies ownership).
	current, err := tx.PersonalityGenFlow.Query().
		Where(
			personalitygenflow.ID(flowID),
			personalitygenflow.HasUserWith(user.ID(userID)),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrFlowNotFound
		}
		d.logger.Error(i18n.T("personality.flow.update_query_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	mutation := tx.PersonalityGenFlow.UpdateOneID(flowID).
		SetCurrentStep(req.CurrentStep).
		SetAnswers(req.Answers).
		SetImageStyle(req.ImageStyle)

	if req.ReferenceImageID != nil {
		mutation = mutation.SetReferenceImageID(*req.ReferenceImageID)
	} else {
		mutation = mutation.ClearReferenceImage()
	}

	// If the flow was already generated, editing answers invalidates that — reset to in_progress.
	if current.Status == personalitygenflow.StatusGenerated {
		mutation = mutation.
			SetStatus(personalitygenflow.StatusInProgress).
			SetGeneratedPrompt("").
			SetGeneratedAboutMe("").
			SetGeneratedNames([]string{})
	}

	// Save returns only the bare row (no edges); we re-query below with
	// WithPersonality + WithReferenceImage to populate the response model.
	_, err = mutation.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "gen flow"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Reload with edges — ent's UpdateOneID.Save returns only the bare row
	// without any eager-loaded edges. We need WithReferenceImage and
	// WithPersonality in the response model, so a second read is required.
	updated, err := tx.PersonalityGenFlow.Query().
		Where(personalitygenflow.ID(flowID)).
		WithPersonality().
		WithReferenceImage().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "gen flow after update"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}
	return toFlowModel(updated), nil
}

// ResetFlow archives the given flow and creates a fresh in_progress flow.
func (d *Datastore) ResetFlow(ctx context.Context, userID, flowID uuid.UUID) (*models.PersonalityGenFlow, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.PersonalityGenFlow.Query().
		Where(
			personalitygenflow.ID(flowID),
			personalitygenflow.HasUserWith(user.ID(userID)),
		).
		Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T("personality.flow.ownership_check_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if !exists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrFlowNotFound
	}

	activeGenerationJobExists, err := tx.Job.Query().
		Where(
			job.HasOwnerWith(user.ID(userID)),
			job.JobTypeEQ("personality_generation"),
			job.ReferenceEQ(flowID.String()),
			job.StatusNotIn(job.StatusComplete, job.StatusCancelled, job.StatusFailed),
		).
		Exist(ctx)
	if err != nil {
		d.logger.Error("personality flow reset: active generation job lookup failed", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if activeGenerationJobExists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrFlowGenerationJobAlreadyActive
	}

	_, err = tx.PersonalityGenFlow.UpdateOneID(flowID).
		SetStatus(personalitygenflow.StatusAccepted).
		SetCurrentStep(0).
		SetAnswers(map[string]string{}).
		SetGeneratedPrompt("").
		SetGeneratedAboutMe("").
		SetGeneratedNames([]string{}).
		SetImageStyle("auto").
		ClearReferenceImage().
		ClearPersonality().
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("personality.flow.reset_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	created, err := tx.PersonalityGenFlow.Create().
		SetUserID(userID).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "gen flow"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toFlowModel(created), nil
}

// SetFlowGenerated stores the OpenAI output and transitions the flow to "generated".
func (d *Datastore) SetFlowGenerated(ctx context.Context, userID, flowID uuid.UUID, prompt, aboutMe string, names []string) (*models.PersonalityGenFlow, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.PersonalityGenFlow.Query().
		Where(
			personalitygenflow.ID(flowID),
			personalitygenflow.HasUserWith(user.ID(userID)),
			personalitygenflow.StatusEQ(personalitygenflow.StatusInProgress),
		).
		Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T("personality.flow.ownership_check_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if !exists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrFlowNotFound
	}

	updated, err := tx.PersonalityGenFlow.UpdateOneID(flowID).
		SetStatus(personalitygenflow.StatusGenerated).
		SetGeneratedPrompt(prompt).
		SetGeneratedAboutMe(aboutMe).
		SetGeneratedNames(names).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("personality.flow.set_generated_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}
	return toFlowModel(updated), nil
}

// AcceptFlow links the flow to a created personality and marks it as accepted.
func (d *Datastore) AcceptFlow(ctx context.Context, userID, flowID, personalityID uuid.UUID) (*models.PersonalityGenFlow, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.PersonalityGenFlow.Query().
		Where(
			personalitygenflow.ID(flowID),
			personalitygenflow.HasUserWith(user.ID(userID)),
		).
		Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T("personality.flow.ownership_check_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if !exists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrFlowNotFound
	}

	updated, err := tx.PersonalityGenFlow.UpdateOneID(flowID).
		SetStatus(personalitygenflow.StatusAccepted).
		SetPersonalityID(personalityID).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("personality.flow.accept_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}
	return toFlowModel(updated), nil
}
