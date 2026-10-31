package datastore

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entchatskillstate "github.com/theimaginaryfoundation/what-iff/ent/chatskillstate"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Loaded skills (ADR 0x035 §4): the skills the agent loaded in a chat with load_skill, kept in
// chat_skill_states until unload_skill removes them or a checkpoint finds them unused. Only
// load_skill writes here; message, mode and job activations are active for their turn alone.

// chatOwnedBy reports whether chatID belongs to userID, as ErrChatNotFound when it does not.
func (d *Datastore) chatOwnedBy(ctx context.Context, client *ent.Client, userID, chatID uuid.UUID) error {
	exists, err := client.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrChatNotFound
	}
	return nil
}

// ListChatLoadedSkillIDs returns the ritual ids loaded in the chat, oldest load first.
func (d *Datastore) ListChatLoadedSkillIDs(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error) {
	if err := d.chatOwnedBy(ctx, d.dbClient, userID, chatID); err != nil {
		return nil, err
	}
	rows, err := d.dbClient.ChatSkillState.Query().
		Where(entchatskillstate.ChatID(chatID)).
		Order(ent.Asc(entchatskillstate.FieldLoadedAt), ent.Asc(entchatskillstate.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.RitualID)
	}
	return out, nil
}

// AddChatLoadedSkills records ritualIDs as loaded in the chat and returns the ones that were not
// loaded before. The caller has already checked that the skills exist and are in the chat's scope.
func (d *Datastore) AddChatLoadedSkills(ctx context.Context, userID, chatID uuid.UUID, ritualIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(ritualIDs) == 0 {
		return nil, nil
	}
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()
	if err := d.chatOwnedBy(ctx, tx.Client(), userID, chatID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	existing, err := tx.ChatSkillState.Query().
		Where(entchatskillstate.ChatID(chatID), entchatskillstate.RitualIDIn(ritualIDs...)).
		Select(entchatskillstate.FieldRitualID).
		All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	have := make(map[uuid.UUID]struct{}, len(existing))
	for _, row := range existing {
		have[row.RitualID] = struct{}{}
	}
	var added []uuid.UUID
	for _, id := range ritualIDs {
		if _, ok := have[id]; ok {
			continue
		}
		have[id] = struct{}{}
		if err := tx.ChatSkillState.Create().SetChatID(chatID).SetRitualID(id).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		added = append(added, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return added, nil
}

// RemoveChatLoadedSkills forgets ritualIDs as loaded in the chat and returns the ones that were
// loaded. It is not scope-gated, so stale rows can always be cleared.
func (d *Datastore) RemoveChatLoadedSkills(ctx context.Context, userID, chatID uuid.UUID, ritualIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(ritualIDs) == 0 {
		return nil, nil
	}
	if err := d.chatOwnedBy(ctx, d.dbClient, userID, chatID); err != nil {
		return nil, err
	}
	rows, err := d.dbClient.ChatSkillState.Query().
		Where(entchatskillstate.ChatID(chatID), entchatskillstate.RitualIDIn(ritualIDs...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	removed := make([]uuid.UUID, 0, len(rows))
	rowIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		removed = append(removed, row.RitualID)
		rowIDs = append(rowIDs, row.ID)
	}
	if _, err := d.dbClient.ChatSkillState.Delete().Where(entchatskillstate.IDIn(rowIDs...)).Exec(ctx); err != nil {
		return nil, err
	}
	return removed, nil
}

// RetainChatLoadedSkills unloads every loaded skill of the chat not in keep and returns the
// unloaded ritual ids; the checkpoint's skill step.
func (d *Datastore) RetainChatLoadedSkills(ctx context.Context, userID, chatID uuid.UUID, keep map[uuid.UUID]struct{}) ([]uuid.UUID, error) {
	if err := d.chatOwnedBy(ctx, d.dbClient, userID, chatID); err != nil {
		return nil, err
	}
	rows, err := d.dbClient.ChatSkillState.Query().
		Where(entchatskillstate.ChatID(chatID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	var unloaded, rowIDs []uuid.UUID
	for _, row := range rows {
		if _, ok := keep[row.RitualID]; ok {
			continue
		}
		unloaded = append(unloaded, row.RitualID)
		rowIDs = append(rowIDs, row.ID)
	}
	if len(rowIDs) == 0 {
		return nil, nil
	}
	if _, err := d.dbClient.ChatSkillState.Delete().Where(entchatskillstate.IDIn(rowIDs...)).Exec(ctx); err != nil {
		return nil, err
	}
	sort.Slice(unloaded, func(i, j int) bool { return unloaded[i].String() < unloaded[j].String() })
	return unloaded, nil
}

// ListChatMessageRitualIDs returns the ids of every skill a message of the chat invoked (/skill,
// hotkey or the API), so the skills index can mark skills used earlier in the thread.
func (d *Datastore) ListChatMessageRitualIDs(ctx context.Context, userID, chatID uuid.UUID) (map[uuid.UUID]struct{}, error) {
	ids, err := d.dbClient.Ritual.Query().
		Where(entritual.HasChatMessagesWith(chatmessage.HasChatWith(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))))).
		IDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

// ListRitualsInChatScope returns the owner's skills a chat with the given personality can use:
// those bound to that personality and the global ones (no personality). Sorted by slug, capped at
// maxRitualsInScope. The caller filters by invocation flags.
func (d *Datastore) ListRitualsInChatScope(ctx context.Context, userID, personalityID uuid.UUID) ([]*models.Ritual, error) {
	query := d.dbClient.Ritual.Query().
		Where(entritual.HasOwnerWith(user.ID(userID))).
		WithPersonality().
		WithMcpServers()
	if personalityID != uuid.Nil {
		query = query.Where(entritual.Or(
			entritual.HasPersonalityWith(personality.ID(personalityID)),
			entritual.Not(entritual.HasPersonality()),
		))
	} else {
		query = query.Where(entritual.Not(entritual.HasPersonality()))
	}
	rows, err := query.
		Order(ent.Asc(entritual.FieldSlug), ent.Asc(entritual.FieldName)).
		Limit(maxRitualsInScope).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*models.Ritual, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRitualModel(row))
	}
	return out, nil
}

// maxRitualsInScope bounds the skills a turn considers; the index budget trims the listing further.
const maxRitualsInScope = 500
