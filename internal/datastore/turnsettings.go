package datastore

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
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

// seedChatTurnSettings fills a new chat's unset settings from its personality, then the
// account default (ADR 0x026: seeded at creation, thread-local after).
//
// A sandboxed chat is not seeded: sandboxes are where other people can start turns on the
// owner's account (the Discord relay), so they run Standard at the model's default effort unless
// the chat is given a setting explicitly. Advanced and a high effort chosen for the owner's own
// threads must not reach them by default.
func seedChatTurnSettings(ctx context.Context, tx *ent.Tx, chat *models.Chat, personalityID uuid.UUID, pref *ent.UserPreference) error {
	if chat.ContextScope.OrDefault() == models.ContextScopeSandbox {
		chat.TurnSettingValues = chat.TurnSettingValues.Normalized()
		return nil
	}
	var fromPersonality models.TurnSettingValues
	if personalityID != uuid.Nil {
		p, err := tx.Personality.Query().
			Where(personality.ID(personalityID)).
			Select(personality.FieldReasoningEffort, personality.FieldAgentLoop).
			Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		if p != nil {
			fromPersonality = turnSettingsFromEnt(p.ReasoningEffort, p.AgentLoop)
		}
	}
	var fromAccount models.TurnSettingValues
	if pref != nil {
		fromAccount = turnSettingsFromEnt(pref.ReasoningEffort, pref.AgentLoop)
	}
	given := chat.TurnSettingValues.Normalized()
	chat.ReasoningEffort = models.FirstSetReasoningEffort(given.ReasoningEffort, fromPersonality.ReasoningEffort, fromAccount.ReasoningEffort)
	chat.AgentLoop = models.FirstSetAgentLoop(given.AgentLoop, fromPersonality.AgentLoop, fromAccount.AgentLoop)
	return nil
}
