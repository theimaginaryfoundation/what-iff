package datastore

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Ent generates one enum type per entity for the shared reasoning_effort / agent_loop columns
// (ent/schema/turnsettings.go). These helpers convert between those and the models types, so
// each entity's mapper and writer stays a one-liner.

// entEffort converts a settings value to an entity's enum pointer; nil and invalid values are nil.
func entEffort[T ~string](v *models.ReasoningEffort) *T {
	if v == nil || !v.Valid() {
		return nil
	}
	out := T(*v)
	return &out
}

// entLoop is entEffort for the agent loop.
func entLoop[T ~string](v *models.AgentLoop) *T {
	if v == nil || !v.Valid() {
		return nil
	}
	out := T(*v)
	return &out
}

// turnSettingsFromEnt reads an entity's two enum columns into the API shape.
func turnSettingsFromEnt[E ~string, L ~string](effort *E, loop *L) models.TurnSettingValues {
	var out models.TurnSettingValues
	if effort != nil {
		e := models.ReasoningEffort(*effort)
		if e.Valid() {
			out.ReasoningEffort = &e
		}
	}
	if loop != nil {
		l := models.AgentLoop(*loop)
		if l.Valid() {
			out.AgentLoop = &l
		}
	}
	return out
}

// turnSettingsSetter is the slice of an Ent update builder that writes the two columns.
type turnSettingsSetter[E ~string, L ~string, B any] interface {
	SetNillableReasoningEffort(*E) B
	ClearReasoningEffort() B
	SetNillableAgentLoop(*L) B
	ClearAgentLoop() B
}

// applyTurnSettings replaces both columns on an update builder (nil clears).
func applyTurnSettings[E ~string, L ~string, B any](u turnSettingsSetter[E, L, B], v models.TurnSettingValues) {
	setReasoningEffort[E, L, B](u, v.ReasoningEffort)
	setAgentLoop[E, L, B](u, v.AgentLoop)
}

func setReasoningEffort[E ~string, L ~string, B any](u turnSettingsSetter[E, L, B], v *models.ReasoningEffort) {
	if e := entEffort[E](v); e != nil {
		u.SetNillableReasoningEffort(e)
	} else {
		u.ClearReasoningEffort()
	}
}

func setAgentLoop[E ~string, L ~string, B any](u turnSettingsSetter[E, L, B], v *models.AgentLoop) {
	if l := entLoop[L](v); l != nil {
		u.SetNillableAgentLoop(l)
	} else {
		u.ClearAgentLoop()
	}
}

// InheritedTurnSettings returns what a thread inherits for the settings it does not set itself
// (ADR 0x026): its personality's values and the account default. They are read at each turn,
// not copied onto the thread, so the thread stores only explicit choices and later changes to the
// personality or the default reach every thread that has no value of its own. A missing
// personality or preference row reads as unset. Only the two columns are selected.
func (d *Datastore) InheritedTurnSettings(ctx context.Context, userID, personalityID uuid.UUID) (fromPersonality, fromAccount models.TurnSettingValues, err error) {
	if personalityID != uuid.Nil {
		p, err := d.dbClient.Personality.Query().
			Where(personality.ID(personalityID), personality.HasUserWith(user.ID(userID))).
			Select(personality.FieldReasoningEffort, personality.FieldAgentLoop).
			Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return models.TurnSettingValues{}, models.TurnSettingValues{}, err
		}
		if p != nil {
			fromPersonality = turnSettingsFromEnt(p.ReasoningEffort, p.AgentLoop)
		}
	}
	pref, err := d.dbClient.UserPreference.Query().
		Where(userpreference.HasUserWith(user.ID(userID))).
		Select(userpreference.FieldReasoningEffort, userpreference.FieldAgentLoop).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return models.TurnSettingValues{}, models.TurnSettingValues{}, err
	}
	if pref != nil {
		fromAccount = turnSettingsFromEnt(pref.ReasoningEffort, pref.AgentLoop)
	}
	return fromPersonality, fromAccount, nil
}
