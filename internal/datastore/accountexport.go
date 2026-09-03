package datastore

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entjob "github.com/theimaginaryfoundation/what-iff/ent/job"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/exporter"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// accountExportBatchSize bounds per-query rows when walking a user's messages.
const accountExportBatchSize = 200

// HasRecentAccountExport reports whether this user has requested an export since the supplied time.
// It backs the in-process cooldown so separate API tasks also reject recently-created export jobs.
func (d *Datastore) HasRecentAccountExport(ctx context.Context, userID uuid.UUID, since time.Time) (bool, error) {
	return d.dbClient.Job.Query().
		Where(
			entjob.JobTypeEQ(models.JobTypeAccountExport),
			entjob.CreatedAtGTE(since),
			entjob.HasOwnerWith(user.ID(userID)),
		).
		Exist(ctx)
}

// ExportConversationInputs assembles a user's chats as clean, id-stripped exporter.ConversationInput
// values (the round-trippable conversations.json projection). Unlike the admin backup, this carries
// no Postgres identity beyond chat.ID (used only as the importer's dedup key), and no
// response_id / read_status / message ids.
func (d *Datastore) ExportConversationInputs(ctx context.Context, userID uuid.UUID) ([]exporter.ConversationInput, error) {
	chats, err := d.dbClient.Chat.Query().
		Where(entchat.HasOwnerWith(user.ID(userID))).
		WithPersonality().
		Order(ent.Asc(entchat.FieldCreatedAt), ent.Asc(entchat.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	convs := make([]exporter.ConversationInput, 0, len(chats))
	for _, c := range chats {
		var personalityID *uuid.UUID
		if c.Edges.Personality != nil {
			id := c.Edges.Personality.ID
			personalityID = &id
		}
		msgs := make([]exporter.MessageInput, 0)
		var lastSentAt time.Time
		var lastMessageID uuid.UUID
		hasCursor := false
		for {
			query := d.dbClient.ChatMessage.Query().
				Where(chatmessage.HasChatWith(entchat.ID(c.ID)))
			if hasCursor {
				// sent_at alone is not unique, so retain the ID tie-breaker used by the
				// chronological ordering. The schema requires sent_at to be non-null.
				// This avoids both skipped same-timestamp rows and increasingly
				// expensive OFFSET scans on long conversations.
				query = query.Where(
					chatmessage.Or(
						chatmessage.SentAtGT(lastSentAt),
						chatmessage.And(
							chatmessage.SentAtEQ(lastSentAt),
							chatmessage.IDGT(lastMessageID),
						),
					),
				)
			}
			batch, err := query.
				Order(ent.Asc(chatmessage.FieldSentAt), ent.Asc(chatmessage.FieldID)).
				Limit(accountExportBatchSize).
				All(ctx)
			if err != nil {
				return nil, err
			}
			if len(batch) == 0 {
				break
			}
			for _, m := range batch {
				msgs = append(msgs, exporter.MessageInput{
					Origin: models.MessageOrigin(m.Origin),
					Text:   m.Message,
					SentAt: m.SentAt,
				})
			}
			last := batch[len(batch)-1]
			lastSentAt = last.SentAt
			lastMessageID = last.ID
			hasCursor = true
			if len(batch) < accountExportBatchSize {
				break
			}
		}

		convs = append(convs, exporter.ConversationInput{
			ID:                         c.ID,
			Title:                      c.Name,
			CreatedAt:                  c.CreatedAt,
			Messages:                   msgs,
			PersonalityID:              personalityID,
			CheckpointSummary:          c.CheckpointSummary,
			CheckpointUserMessageCount: c.CheckpointUserMessageCount,
			LastCheckpointAt:           c.LastCheckpointAt,
			DisabledTools:              c.DisabledTools,
			Tags:                       c.Tags,
			IsFavorite:                 c.IsFavorite,
			IsAutoMood:                 c.IsAutoMood,
		})
	}

	return convs, nil
}

// ImportedChatIDsByHash returns destination chat IDs for the supplied stable import hashes. It
// includes chats created by this request and matching chats from an earlier additive import, which
// lets account-memory references be remapped safely in either case.
func (d *Datastore) ImportedChatIDsByHash(ctx context.Context, userID uuid.UUID, hashes []string) (map[string]uuid.UUID, error) {
	out := make(map[string]uuid.UUID, len(hashes))
	if len(hashes) == 0 {
		return out, nil
	}
	rows, err := d.dbClient.Chat.Query().
		Where(
			entchat.HasOwnerWith(user.ID(userID)),
			entchat.ImportHashIn(hashes...),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range rows {
		if c.ImportHash != "" {
			out[c.ImportHash] = c.ID
		}
	}
	return out, nil
}

// ExportPersonalityInputs returns the user's personalities as allowlisted projection inputs
// (current scratchpad included as its current state; no per-checkpoint history in the MVP).
func (d *Datastore) ExportPersonalityInputs(ctx context.Context, userID uuid.UUID) ([]exporter.PersonalityInput, error) {
	rows, err := d.dbClient.Personality.Query().
		Where(personality.HasUserWith(user.ID(userID))).
		Order(ent.Asc(personality.FieldCreatedAt), ent.Asc(personality.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]exporter.PersonalityInput, 0, len(rows))
	for _, p := range rows {
		out = append(out, exporter.PersonalityInput{
			ID:              p.ID,
			Name:            p.Name,
			SystemPrompt:    p.SystemPrompt,
			Scratchpad:      p.Scratchpad,
			AutoPinMemories: p.AutoPinMemories,
			CreatedAt:       p.CreatedAt,
			UpdatedAt:       p.UpdatedAt,
		})
	}
	return out, nil
}

// ExportMemoryRecords returns all of a user's memories as clean records (no embeddings). Reuses
// toMemoryRecord so the exported memory files match the memory-ZIP export shape.
func (d *Datastore) ExportMemoryRecords(ctx context.Context, userID uuid.UUID) ([]models.MemoryRecord, error) {
	rows, err := d.dbClient.Memory.Query().
		Where(entmemory.HasOwnerWith(user.ID(userID))).
		WithChat().
		Order(ent.Asc(entmemory.FieldCreatedAt), ent.Asc(entmemory.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.MemoryRecord, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMemoryRecord(m))
	}
	return out, nil
}

// ExportAccountIdentity returns the email and username for a user, used to stamp the export manifest
// and address the completion email.
func (d *Datastore) ExportAccountIdentity(ctx context.Context, userID uuid.UUID) (email, username string, err error) {
	u, err := d.dbClient.User.Query().Where(user.ID(userID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", "", ErrUserNotFound
		}
		return "", "", err
	}
	return u.Email, u.Username, nil
}
