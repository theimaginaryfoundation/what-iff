package datastore

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/agentjob"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessagecontextitem"
	"github.com/theimaginaryfoundation/what-iff/ent/mood"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	"github.com/theimaginaryfoundation/what-iff/ent/toolcall"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const accountBackupMaxJSONLine = 16 << 20 // 16 MiB

var errBackupEntryNotFound = errors.New("backup entry not found")

type accountBackupEntity string

const (
	accountBackupEntityMood                   accountBackupEntity = "mood"
	accountBackupEntityPersonality            accountBackupEntity = "personality"
	accountBackupEntityChat                   accountBackupEntity = "chat"
	accountBackupEntityChatMessage            accountBackupEntity = "chat_message"
	accountBackupEntityChatMessageContextItem accountBackupEntity = "chat_message_context_item"
	accountBackupEntityToolCall               accountBackupEntity = "tool_call"
	accountBackupEntityAgentJob               accountBackupEntity = "agent_job"
	accountBackupEntityRitual                 accountBackupEntity = "ritual"
)

// AdminImportAccountBackup imports an account backup into an existing target user.
//
// The restore is intentionally not wrapped in one database transaction. Backups can include
// many rows plus memory embedding regeneration, so a single long-running transaction would
// increase lock time and rollback risk. Partial imports are expected on failure; because
// source UUIDs are preserved and duplicates are skipped, operators can fix the underlying
// issue and safely re-import the same archive to continue the restore.
//
// JSONL sections are read one section at a time into memory. The admin upload cap bounds
// worst-case memory use for this internal, low-frequency path; if this grows into a
// user-facing or high-volume feature, convert section reads to callback/batch processing.
func (d *Datastore) AdminImportAccountBackup(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, createEmbedding func(context.Context, string) ([]float32, error)) (result models.AccountBackupImportResult, err error) {
	result = models.AccountBackupImportResult{
		FormatVersion: models.AccountBackupFormatVersion,
		Sections:      make(map[string]models.AccountBackupSectionResult),
	}
	subject := targetUserID
	defer func() {
		meta := map[string]any{
			"success":                              err == nil,
			"memories_imported":                    result.Memories.ImportedCount,
			"memories_duplicate":                   result.Memories.DuplicateCount,
			"memories_invalid_records":             result.Memories.InvalidRecordCount,
			"memories_skipped_missing_chat":        result.Memories.SkippedMissingChat,
			"memories_skipped_missing_personality": result.Memories.SkippedMissingPersonality,
		}
		if err != nil {
			meta["error"] = err.Error()
		}
		sectionSummary := make(map[string]map[string]int, len(result.Sections))
		for k, v := range result.Sections {
			sectionSummary[k] = map[string]int{
				"created": v.Created, "duplicate": v.Duplicate, "conflict": v.Conflict,
				"skipped": v.Skipped, "invalid": v.Invalid, "missing_ref": v.MissingRef,
			}
		}
		meta["sections"] = sectionSummary
		d.writeAuditLog(ctx, auditEntry{
			Category:      auditCategoryAccountBackup,
			Action:        "import",
			Message:       fmt.Sprintf("account backup import for user %s (success=%v)", targetUserID, err == nil),
			SubjectUserID: &subject,
			Metadata:      meta,
		})
	}()

	var exists bool
	exists, err = d.dbClient.User.Query().Where(user.ID(targetUserID)).Exist(ctx)
	if err != nil {
		return result, err
	}
	if !exists {
		return result, ErrUserNotFound
	}

	var manifest models.AccountBackupManifest
	manifest, err = readBackupManifest(zr)
	if err != nil {
		return result, err
	}
	if manifest.FormatVersion != models.AccountBackupFormatVersion {
		return result, fmt.Errorf("%w: %d", ErrUnsupportedAccountBackupVersion, manifest.FormatVersion)
	}
	result.FormatVersion = manifest.FormatVersion

	if err = d.importBackupMoods(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}
	if err = d.importBackupPersonalities(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}
	if err = d.importBackupPersonalityMoods(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}
	if err = d.importBackupRituals(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}
	if err = d.importBackupChats(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}
	if err = d.importBackupChatMessages(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}
	if err = d.importBackupChatMessageContextItems(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}
	if err = d.importBackupToolCalls(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}

	var memories models.MemoryImportResult
	memories, err = d.importMemories(ctx, targetUserID, zr, createEmbedding, false)
	result.Memories = memories
	if err != nil {
		return result, err
	}

	if err = d.importBackupAgentJobs(ctx, targetUserID, zr, result.Sections); err != nil {
		return result, err
	}

	return result, nil
}

func (d *Datastore) importBackupMoods(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupMood](zr, "moods.jsonl")
	if err != nil {
		return err
	}
	res := sections["moods"]
	res.Invalid += invalid
	for _, rec := range records {
		state, err := d.userOwnedIDState(ctx, accountBackupEntityMood, rec.ID, targetUserID)
		if err != nil {
			return err
		}
		if state == accountBackupIDDuplicate {
			res.Duplicate++
			continue
		}
		if state == accountBackupIDConflict {
			res.Conflict++
			continue
		}
		create := d.dbClient.Mood.Create().
			SetID(rec.ID).
			SetName(rec.Name).
			SetDescription(rec.Description).
			SetPromptSnippet(rec.PromptSnippet).
			SetRecommendedModel(rec.RecommendedModel).
			SetOwnerID(targetUserID).
			SetCreatedAt(rec.CreatedAt).
			SetUpdatedAt(rec.UpdatedAt)
		if _, err := create.Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["moods"] = res
	return nil
}

func (d *Datastore) importBackupPersonalities(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupPersonality](zr, "personalities.jsonl")
	if err != nil {
		return err
	}
	res := sections["personalities"]
	res.Invalid += invalid
	for _, rec := range records {
		state, err := d.userOwnedIDState(ctx, accountBackupEntityPersonality, rec.ID, targetUserID)
		if err != nil {
			return err
		}
		if state == accountBackupIDDuplicate {
			res.Duplicate++
			continue
		}
		if state == accountBackupIDConflict {
			res.Conflict++
			continue
		}
		if strings.TrimSpace(rec.Name) == "" || strings.TrimSpace(rec.SystemPrompt) == "" {
			res.Invalid++
			continue
		}
		_, err = d.dbClient.Personality.Create().
			SetID(rec.ID).
			SetName(rec.Name).
			SetSystemPrompt(rec.SystemPrompt).
			SetScratchpad(rec.Scratchpad).
			SetScratchpadHistory(append([]string(nil), rec.ScratchpadHistory...)).
			SetArchivalModel(rec.ArchivalModel).
			SetScratchpadUpdatePrompt(rec.ScratchpadUpdatePrompt).
			SetMemorySearchPrompt(rec.MemorySearchPrompt).
			SetMemoryWritePrompt(rec.MemoryWritePrompt).
			SetAutoPinMemories(rec.AutoPinMemories).
			SetUserID(targetUserID).
			SetCreatedAt(rec.CreatedAt).
			SetUpdatedAt(rec.UpdatedAt).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["personalities"] = res
	return nil
}

func (d *Datastore) importBackupPersonalityMoods(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupPersonalityMood](zr, "personality_moods.jsonl")
	if err != nil {
		return err
	}
	res := sections["personality_moods"]
	res.Invalid += invalid
	for _, rec := range records {
		personalityOK, err := d.personalityBelongsToUser(ctx, targetUserID, rec.PersonalityID)
		if err != nil {
			return err
		}
		moodOK, err := d.moodBelongsToUser(ctx, targetUserID, rec.MoodID)
		if err != nil {
			return err
		}
		if !personalityOK || !moodOK {
			res.MissingRef++
			continue
		}
		if err := d.dbClient.Personality.UpdateOneID(rec.PersonalityID).AddMoodIDs(rec.MoodID).Exec(ctx); err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["personality_moods"] = res
	return nil
}

func (d *Datastore) importBackupRituals(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupRitual](zr, "rituals.jsonl")
	if err != nil {
		return err
	}
	res := sections["rituals"]
	res.Invalid += invalid
	for _, rec := range records {
		name := strings.TrimSpace(rec.Name)
		desc := strings.TrimSpace(rec.Description)
		content := strings.TrimSpace(rec.Content)
		if name == "" || desc == "" || content == "" {
			res.Invalid++
			continue
		}
		state, err := d.userOwnedIDState(ctx, accountBackupEntityRitual, rec.ID, targetUserID)
		if err != nil {
			return err
		}
		if state == accountBackupIDDuplicate {
			res.Duplicate++
			continue
		}
		if state == accountBackupIDConflict {
			res.Conflict++
			continue
		}
		create := d.dbClient.Ritual.Create().
			SetID(rec.ID).
			SetOwnerID(targetUserID).
			SetName(name).
			SetDescription(desc).
			SetContent(content).
			SetHotkeys(rec.Hotkeys).
			SetCreatedAt(rec.CreatedAt).
			SetUpdatedAt(rec.UpdatedAt)
		if rec.PersonalityID != nil {
			if ok, err := d.personalityBelongsToUser(ctx, targetUserID, *rec.PersonalityID); err != nil {
				return err
			} else if ok {
				create.SetPersonalityID(*rec.PersonalityID)
			} else {
				res.MissingRef++
			}
		}
		if _, err := create.Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["rituals"] = res
	return nil
}

func (d *Datastore) importBackupChats(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupChat](zr, "chats.jsonl")
	if err != nil {
		return err
	}
	if len(records) == 0 {
		sections["chats"] = models.AccountBackupSectionResult{Invalid: invalid}
		return nil
	}
	defaultModelID, err := d.defaultModelIDForUser(ctx, targetUserID)
	if err != nil {
		return err
	}
	res := sections["chats"]
	res.Invalid += invalid
	for _, rec := range records {
		state, err := d.userOwnedIDState(ctx, accountBackupEntityChat, rec.ID, targetUserID)
		if err != nil {
			return err
		}
		if state == accountBackupIDDuplicate {
			res.Duplicate++
			continue
		}
		if state == accountBackupIDConflict {
			res.Conflict++
			continue
		}
		modelID := defaultModelID
		if rec.ModelName != "" {
			if m, err := d.GetModelByName(ctx, rec.ModelName); err == nil {
				modelID = m.ID
			} else if !errors.Is(err, ErrModelNotFound) {
				return err
			}
		}
		if modelID == uuid.Nil {
			res.MissingRef++
			continue
		}

		create := d.dbClient.Chat.Create().
			SetID(rec.ID).
			SetName(rec.Name).
			SetOwnerID(targetUserID).
			SetModelID(modelID).
			SetCheckpointSummary(rec.CheckpointSummary).
			SetCheckpointUserMessageCount(rec.CheckpointUserMessageCount).
			SetDisabledTools(append([]string(nil), rec.DisabledTools...)).
			SetIsAutoMood(rec.IsAutoMood).
			SetResponseID("").
			SetCreatedAt(rec.CreatedAt).
			SetUpdatedAt(rec.UpdatedAt)
		if rec.LastMessageTime != nil {
			create.SetLastMessageTime(*rec.LastMessageTime)
		}
		if rec.LastCheckpointAt != nil {
			create.SetLastCheckpointAt(*rec.LastCheckpointAt)
		}
		if rec.PersonalityID != nil {
			if ok, err := d.personalityBelongsToUser(ctx, targetUserID, *rec.PersonalityID); err != nil {
				return err
			} else if ok {
				create.SetPersonalityID(*rec.PersonalityID)
			} else {
				res.MissingRef++
			}
		}
		if rec.ActiveMoodID != nil {
			if ok, err := d.moodBelongsToUser(ctx, targetUserID, *rec.ActiveMoodID); err != nil {
				return err
			} else if ok {
				create.SetActiveMoodID(*rec.ActiveMoodID)
			} else {
				res.MissingRef++
			}
		}
		if _, err := create.Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["chats"] = res
	return nil
}

func (d *Datastore) importBackupChatMessages(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupChatMessage](zr, "chat_messages.jsonl")
	if err != nil {
		return err
	}
	res := sections["chat_messages"]
	res.Invalid += invalid
	for _, rec := range records {
		state, err := d.userOwnedIDState(ctx, accountBackupEntityChatMessage, rec.ID, targetUserID)
		if err != nil {
			return err
		}
		if state == accountBackupIDDuplicate {
			res.Duplicate++
			continue
		}
		if state == accountBackupIDConflict {
			res.Conflict++
			continue
		}
		if ok, err := d.chatBelongsToUser(ctx, targetUserID, rec.ChatID); err != nil {
			return err
		} else if !ok {
			res.MissingRef++
			continue
		}
		create := d.dbClient.ChatMessage.Create().
			SetID(rec.ID).
			SetMessage(rec.Message).
			SetOrigin(chatmessage.Origin(rec.Origin)).
			SetReadStatus(chatmessage.ReadStatus(rec.ReadStatus)).
			SetResponseID("").
			SetChatID(rec.ChatID).
			SetSentAt(rec.SentAt).
			SetTokens(rec.Tokens)
		if rec.GenerationModel != "" {
			create.SetGenerationModel(rec.GenerationModel)
		}
		if rec.GenerationPersonality != "" {
			create.SetGenerationPersonality(rec.GenerationPersonality)
		}
		if rec.GenerationMoodID != nil {
			if ok, err := d.moodBelongsToUser(ctx, targetUserID, *rec.GenerationMoodID); err != nil {
				return err
			} else if ok {
				create.SetGenerationMoodID(*rec.GenerationMoodID)
			} else {
				res.MissingRef++
			}
		}
		if _, err := create.Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["chat_messages"] = res
	return nil
}

func (d *Datastore) importBackupChatMessageContextItems(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupChatMessageContextItem](zr, "chat_message_context_items.jsonl")
	if err != nil {
		return err
	}
	res := sections["chat_message_context_items"]
	res.Invalid += invalid
	for _, rec := range records {
		state, err := d.userOwnedIDState(ctx, accountBackupEntityChatMessageContextItem, rec.ID, targetUserID)
		if err != nil {
			return err
		}
		if state == accountBackupIDDuplicate {
			res.Duplicate++
			continue
		}
		if state == accountBackupIDConflict {
			res.Conflict++
			continue
		}
		if ok, err := d.chatMessageBelongsToUser(ctx, targetUserID, rec.ChatMessageID); err != nil {
			return err
		} else if !ok {
			res.MissingRef++
			continue
		}
		if _, err := d.dbClient.ChatMessageContextItem.Create().
			SetID(rec.ID).
			SetType(rec.Type).
			SetContent(rec.Content).
			SetChatMessageID(rec.ChatMessageID).
			Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["chat_message_context_items"] = res
	return nil
}

func (d *Datastore) importBackupToolCalls(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupToolCall](zr, "tool_calls.jsonl")
	if err != nil {
		return err
	}
	res := sections["tool_calls"]
	res.Invalid += invalid
	for _, rec := range records {
		state, err := d.userOwnedIDState(ctx, accountBackupEntityToolCall, rec.ID, targetUserID)
		if err != nil {
			return err
		}
		if state == accountBackupIDDuplicate {
			res.Duplicate++
			continue
		}
		if state == accountBackupIDConflict {
			res.Conflict++
			continue
		}
		if ok, err := d.chatMessageBelongsToUser(ctx, targetUserID, rec.ChatMessageID); err != nil {
			return err
		} else if !ok {
			res.MissingRef++
			continue
		}
		create := d.dbClient.ToolCall.Create().
			SetID(rec.ID).
			SetToolName(rec.ToolName).
			SetChatMessageID(rec.ChatMessageID).
			SetCreatedAt(rec.CreatedAt).
			SetUpdatedAt(rec.UpdatedAt)
		if rec.ToolInput != "" {
			create.SetToolInput(rec.ToolInput)
		}
		if rec.ToolOutput != "" {
			create.SetToolOutput(rec.ToolOutput)
		}
		if rec.ToolError != "" {
			create.SetToolError(rec.ToolError)
		}
		if _, err := create.Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["tool_calls"] = res
	return nil
}

func (d *Datastore) importBackupAgentJobs(ctx context.Context, targetUserID uuid.UUID, zr *zip.Reader, sections map[string]models.AccountBackupSectionResult) error {
	records, invalid, err := readBackupJSONL[models.AccountBackupAgentJob](zr, "agent_jobs.jsonl")
	if err != nil {
		return err
	}
	res := sections["agent_jobs"]
	res.Invalid += invalid
	for _, rec := range records {
		state, err := d.userOwnedIDState(ctx, accountBackupEntityAgentJob, rec.ID, targetUserID)
		if err != nil {
			return err
		}
		if state == accountBackupIDDuplicate {
			res.Duplicate++
			continue
		}
		if state == accountBackupIDConflict {
			res.Conflict++
			continue
		}
		create := d.dbClient.AgentJob.Create().
			SetID(rec.ID).
			SetOwnerID(targetUserID).
			SetPrompt(rec.Prompt).
			SetScheduleInput(rec.ScheduleInput).
			SetScheduleType(agentjob.ScheduleType(rec.ScheduleType)).
			SetTimezone(rec.Timezone).
			SetStatus(agentjob.Status(rec.Status)).
			SetRunCount(rec.RunCount).
			SetCreatedAt(rec.CreatedAt).
			SetUpdatedAt(rec.UpdatedAt)
		if rec.Title != nil {
			create.SetNillableTitle(rec.Title)
		}
		if rec.Schedule != nil {
			create.SetNillableSchedule(rec.Schedule)
		}
		if rec.RunAt != nil {
			create.SetNillableRunAt(rec.RunAt)
		}
		if rec.NextRunAt != nil {
			create.SetNillableNextRunAt(rec.NextRunAt)
		}
		if rec.LastRunAt != nil {
			create.SetNillableLastRunAt(rec.LastRunAt)
		}
		if strings.TrimSpace(rec.LastError) != "" {
			create.SetLastError(rec.LastError)
		}
		if rec.ChatID != nil {
			if ok, err := d.chatBelongsToUser(ctx, targetUserID, *rec.ChatID); err != nil {
				return err
			} else if ok {
				create.SetChatID(*rec.ChatID)
			} else {
				res.MissingRef++
			}
		}
		if rec.PersonalityID != nil {
			if ok, err := d.personalityBelongsToUser(ctx, targetUserID, *rec.PersonalityID); err != nil {
				return err
			} else if ok {
				create.SetNillablePersonalityID(rec.PersonalityID)
			} else {
				res.MissingRef++
			}
		}
		if strings.TrimSpace(rec.ModelName) != "" {
			if m, err := d.GetModelByName(ctx, rec.ModelName); err == nil {
				create.SetNillableModelID(&m.ID)
			} else if !errors.Is(err, ErrModelNotFound) {
				return err
			}
		}
		if _, err := create.Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				res.Duplicate++
				continue
			}
			return err
		}
		res.Created++
	}
	sections["agent_jobs"] = res
	return nil
}

type accountBackupIDState int

const (
	accountBackupIDMissing accountBackupIDState = iota
	accountBackupIDDuplicate
	accountBackupIDConflict
)

func (d *Datastore) userOwnedIDState(ctx context.Context, entity accountBackupEntity, id, userID uuid.UUID) (accountBackupIDState, error) {
	if id == uuid.Nil {
		return accountBackupIDConflict, nil
	}

	// Check target ownership first: repeat imports are expected, so duplicates should
	// short-circuit with one query. Only non-owned IDs need the second existence check
	// to distinguish missing source rows from cross-user conflicts.
	belongs, err := d.entityBelongsToUser(ctx, entity, id, userID)
	if err != nil {
		return accountBackupIDMissing, err
	}
	if belongs {
		return accountBackupIDDuplicate, nil
	}

	exists, err := d.entityExists(ctx, entity, id)
	if err != nil {
		return accountBackupIDMissing, err
	}
	if exists {
		return accountBackupIDConflict, nil
	}
	return accountBackupIDMissing, nil
}

func (d *Datastore) entityExists(ctx context.Context, entity accountBackupEntity, id uuid.UUID) (bool, error) {
	switch entity {
	case accountBackupEntityMood:
		return d.dbClient.Mood.Query().Where(mood.ID(id)).Exist(ctx)
	case accountBackupEntityPersonality:
		return d.dbClient.Personality.Query().Where(personality.ID(id)).Exist(ctx)
	case accountBackupEntityChat:
		return d.dbClient.Chat.Query().Where(entchat.ID(id)).Exist(ctx)
	case accountBackupEntityChatMessage:
		return d.dbClient.ChatMessage.Query().Where(chatmessage.ID(id)).Exist(ctx)
	case accountBackupEntityChatMessageContextItem:
		return d.dbClient.ChatMessageContextItem.Query().Where(chatmessagecontextitem.ID(id)).Exist(ctx)
	case accountBackupEntityToolCall:
		return d.dbClient.ToolCall.Query().Where(toolcall.ID(id)).Exist(ctx)
	case accountBackupEntityAgentJob:
		return d.dbClient.AgentJob.Query().Where(agentjob.ID(id)).Exist(ctx)
	case accountBackupEntityRitual:
		return d.dbClient.Ritual.Query().Where(entritual.ID(id)).Exist(ctx)
	default:
		return false, fmt.Errorf("unknown account backup entity %q", entity)
	}
}

func (d *Datastore) entityBelongsToUser(ctx context.Context, entity accountBackupEntity, id, userID uuid.UUID) (bool, error) {
	switch entity {
	case accountBackupEntityMood:
		return d.moodBelongsToUser(ctx, userID, id)
	case accountBackupEntityPersonality:
		return d.personalityBelongsToUser(ctx, userID, id)
	case accountBackupEntityChat:
		return d.chatBelongsToUser(ctx, userID, id)
	case accountBackupEntityChatMessage:
		return d.chatMessageBelongsToUser(ctx, userID, id)
	case accountBackupEntityChatMessageContextItem:
		return d.contextItemBelongsToUser(ctx, userID, id)
	case accountBackupEntityToolCall:
		return d.toolCallBelongsToUser(ctx, userID, id)
	case accountBackupEntityAgentJob:
		return d.agentJobBelongsToUser(ctx, userID, id)
	case accountBackupEntityRitual:
		return d.ritualBelongsToUser(ctx, userID, id)
	default:
		return false, fmt.Errorf("unknown account backup entity %q", entity)
	}
}

func (d *Datastore) defaultModelIDForUser(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	prefs, err := d.dbClient.UserPreference.Query().
		Where(userpreference.HasUserWith(user.ID(userID))).
		WithModel().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return uuid.Nil, ErrAccountBackupDefaultModelMissing
		}
		return uuid.Nil, err
	}
	if prefs.Edges.Model == nil {
		return uuid.Nil, ErrAccountBackupDefaultModelMissing
	}
	return prefs.Edges.Model.ID, nil
}

func (d *Datastore) moodBelongsToUser(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	return d.dbClient.Mood.Query().Where(mood.ID(id), mood.HasOwnerWith(user.ID(userID))).Exist(ctx)
}

func (d *Datastore) personalityBelongsToUser(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	return d.dbClient.Personality.Query().Where(personality.ID(id), personality.HasUserWith(user.ID(userID))).Exist(ctx)
}

func (d *Datastore) chatBelongsToUser(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	return d.dbClient.Chat.Query().Where(entchat.ID(id), entchat.HasOwnerWith(user.ID(userID))).Exist(ctx)
}

func (d *Datastore) chatMessageBelongsToUser(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	return d.dbClient.ChatMessage.Query().Where(chatmessage.ID(id), chatmessage.HasChatWith(entchat.HasOwnerWith(user.ID(userID)))).Exist(ctx)
}

func (d *Datastore) contextItemBelongsToUser(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	return d.dbClient.ChatMessageContextItem.Query().
		Where(chatmessagecontextitem.ID(id), chatmessagecontextitem.HasChatMessageWith(chatmessage.HasChatWith(entchat.HasOwnerWith(user.ID(userID))))).
		Exist(ctx)
}

func (d *Datastore) toolCallBelongsToUser(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	return d.dbClient.ToolCall.Query().
		Where(toolcall.ID(id), toolcall.HasChatMessageWith(chatmessage.HasChatWith(entchat.HasOwnerWith(user.ID(userID))))).
		Exist(ctx)
}

func (d *Datastore) agentJobBelongsToUser(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	return d.dbClient.AgentJob.Query().Where(agentjob.ID(id), agentjob.HasOwnerWith(user.ID(userID))).Exist(ctx)
}

func (d *Datastore) ritualBelongsToUser(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	return d.dbClient.Ritual.Query().Where(entritual.ID(id), entritual.HasOwnerWith(user.ID(userID))).Exist(ctx)
}

func readBackupManifest(zr *zip.Reader) (models.AccountBackupManifest, error) {
	var manifest models.AccountBackupManifest
	rc, err := openBackupEntry(zr, "manifest.json")
	if err != nil {
		return manifest, err
	}
	defer rc.Close()
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode manifest.json: %w", err)
	}
	return manifest, nil
}

func readBackupJSONL[T any](zr *zip.Reader, name string) ([]T, int, error) {
	rc, err := openBackupEntry(zr, name)
	if err != nil {
		if errors.Is(err, errBackupEntryNotFound) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer rc.Close()

	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 64*1024), accountBackupMaxJSONLine)
	records := make([]T, 0)
	invalid := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record T
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			invalid++
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return records, invalid, fmt.Errorf("scan %s: JSONL record exceeds %d MiB: %w", name, accountBackupMaxJSONLine>>20, err)
		}
		return records, invalid, fmt.Errorf("scan %s: %w", name, err)
	}
	return records, invalid, nil
}

func openBackupEntry(zr *zip.Reader, name string) (io.ReadCloser, error) {
	cleanName := filepath.Clean(name)
	for _, zf := range zr.File {
		if filepath.Clean(zf.Name) == cleanName {
			rc, err := zf.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s: %w", name, err)
			}
			return rc, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", errBackupEntryNotFound, name)
}
