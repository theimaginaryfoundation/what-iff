package datastore

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/agentjob"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/mood"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createAccountBackupTestSchema creates the tables account backup import touches that
// createMemoryImportTestSchema does not already provide: chat_messages (parent of
// chat_message_context_items and tool_calls), moods, rituals, models, user_preferences,
// tool_calls, agent_jobs, chat_message_context_items, personality_moods, and audit_logs.
//
// Must be composed after createMemoryImportTestSchema (users/chats/personalities are FK
// parents here) via newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema).
func createAccountBackupTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	statements := []string{
		`CREATE TABLE moods (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			description text NOT NULL DEFAULT '',
			prompt_snippet text NOT NULL DEFAULT '',
			thumbnail_data blob,
			recommended_model text DEFAULT '',
			user_moods uuid NOT NULL
		)`,
		`CREATE TABLE models (
			id uuid PRIMARY KEY,
			name text NOT NULL,
			display_name text NOT NULL,
			description text NOT NULL,
			provider text NOT NULL DEFAULT 'openai',
			tool_support bool NOT NULL DEFAULT false,
			base_credits_per_slab integer NOT NULL DEFAULT 1,
			subscription_tier text NOT NULL DEFAULT 'high',
			deleted bool NOT NULL DEFAULT false,
			is_default bool NOT NULL DEFAULT false
		)`,
		`CREATE TABLE user_preferences (
			id uuid PRIMARY KEY,
			theme text NOT NULL DEFAULT 'dark',
			last_seen_announcement text DEFAULT '',
			experimental_memory_dedupe_chain bool NOT NULL DEFAULT false,
			favorite_model_ids json NOT NULL DEFAULT '[]',
			user_preferences uuid NOT NULL UNIQUE,
			default_model uuid NOT NULL,
			default_personality uuid
		)`,
		`CREATE TABLE rituals (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			name text NOT NULL,
			description text NOT NULL,
			content text NOT NULL,
			hotkeys text NOT NULL,
			agent_job_rituals uuid,
			mood_rituals uuid,
			ritual_personality uuid,
			user_rituals uuid NOT NULL
		)`,
		`CREATE TABLE chat_messages (
			id uuid PRIMARY KEY,
			message text NOT NULL,
			origin text NOT NULL,
			read_status text NOT NULL DEFAULT 'read',
			response_id text,
			sent_at datetime NOT NULL,
			tokens integer,
			generation_model text,
			generation_personality text,
			generation_expression_reasoning text,
			last_error_message text,
			checkpoint_completed_at datetime,
			context_breakdown json,
			chat_messages uuid NOT NULL,
			chat_message_generation_mood uuid,
			chat_message_generation_expression uuid
		)`,
		`CREATE TABLE chat_message_context_items (
			id uuid PRIMARY KEY,
			type text NOT NULL,
			content text NOT NULL,
			memory_id uuid,
			scope text,
			chat_message_context_items uuid NOT NULL
		)`,
		`CREATE TABLE tool_calls (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			tool_name text NOT NULL,
			tool_input text,
			tool_output text,
			tool_error text,
			chat_message_tool_calls uuid NOT NULL
		)`,
		`CREATE TABLE agent_jobs (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			title text,
			prompt text NOT NULL,
			schedule_input text NOT NULL,
			schedule_type text NOT NULL,
			schedule text,
			run_at datetime,
			timezone text NOT NULL DEFAULT 'UTC',
			status text NOT NULL DEFAULT 'active',
			next_run_at datetime,
			last_run_at datetime,
			last_error text,
			run_count integer NOT NULL DEFAULT 0,
			personality_id uuid,
			model_id uuid,
			chat_agent_jobs uuid,
			user_agent_jobs uuid NOT NULL
		)`,
		`CREATE TABLE personality_moods (
			personality_id uuid NOT NULL,
			mood_id uuid NOT NULL,
			PRIMARY KEY (personality_id, mood_id)
		)`,
		`CREATE TABLE audit_logs (
			id uuid PRIMARY KEY,
			occurred_at datetime NOT NULL,
			category text NOT NULL,
			action text NOT NULL,
			message text NOT NULL,
			actor_user_id uuid,
			subject_user_id uuid
		)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newAccountBackupTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema)
}

func TestAdminImportAccountBackup_UserNotFound(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
	})

	_, err := ds.AdminImportAccountBackup(context.Background(), uuid.New(), zr, nil)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func createAccountBackupTestUser(t *testing.T, ds *Datastore, userID uuid.UUID) {
	t.Helper()
	_, err := ds.dbClient.User.Create().
		SetID(userID).
		SetUsername("backup-" + userID.String()[:8]).
		SetEmail("backup-" + userID.String()[:8] + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
}

func TestAdminImportAccountBackup_UnsupportedVersion(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":99}`,
	})

	_, err := ds.AdminImportAccountBackup(context.Background(), userID, zr, nil)
	require.ErrorIs(t, err, ErrUnsupportedAccountBackupVersion)
}

func TestAdminImportAccountBackup_MissingManifest(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)

	zr := buildZipReaderForTest(t, map[string]string{
		"moods.jsonl": "",
	})

	_, err := ds.AdminImportAccountBackup(context.Background(), userID, zr, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, errBackupEntryNotFound)
}

// createAccountBackupTestModelAndPreference creates a model row and a user_preferences row
// pointing the given user's default model at it, so defaultModelIDForUser resolves.
func createAccountBackupTestModelAndPreference(t *testing.T, ds *Datastore, userID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	m, err := ds.dbClient.Model.Create().
		SetName("gpt-test").
		SetDisplayName("GPT Test").
		SetDescription("test model").
		Save(ctx)
	require.NoError(t, err)

	_, err = ds.dbClient.UserPreference.Create().
		SetUserID(userID).
		SetModelID(m.ID).
		Save(ctx)
	require.NoError(t, err)
	return m.ID
}

func accountBackupJSONL[T any](t *testing.T, records ...T) string {
	t.Helper()
	lines := make([]string, len(records))
	for i, rec := range records {
		b, err := json.Marshal(rec)
		require.NoError(t, err)
		lines[i] = string(b)
	}
	return strings.Join(lines, "\n")
}

func TestAdminImportAccountBackup_HappyPath(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	createAccountBackupTestModelAndPreference(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)
	moodID := uuid.New()
	personalityID := uuid.New()
	ritualID := uuid.New()
	chatID := uuid.New()
	chatMessageID := uuid.New()
	contextItemID := uuid.New()
	toolCallID := uuid.New()
	agentJobID := uuid.New()

	entries := map[string]string{
		"manifest.json": `{"format_version":1}`,
		"moods.jsonl": accountBackupJSONL(t, models.AccountBackupMood{
			ID: moodID, Name: "Curious", Description: "d", PromptSnippet: "p",
			CreatedAt: now, UpdatedAt: now,
		}),
		"personalities.jsonl": accountBackupJSONL(t, models.AccountBackupPersonality{
			ID: personalityID, Name: "Vix", SystemPrompt: "system prompt",
			CreatedAt: now, UpdatedAt: now,
		}),
		"personality_moods.jsonl": accountBackupJSONL(t, models.AccountBackupPersonalityMood{
			PersonalityID: personalityID, MoodID: moodID,
		}),
		"rituals.jsonl": accountBackupJSONL(t, models.AccountBackupRitual{
			ID: ritualID, Name: "Morning", Description: "desc", Content: "content", Hotkeys: "",
			CreatedAt: now, UpdatedAt: now,
		}),
		"chats.jsonl": accountBackupJSONL(t, models.AccountBackupChat{
			ID: chatID, Name: "Chat 1", PersonalityID: &personalityID, ActiveMoodID: &moodID,
			CreatedAt: now, UpdatedAt: now,
		}),
		"chat_messages.jsonl": accountBackupJSONL(t, models.AccountBackupChatMessage{
			ID: chatMessageID, ChatID: chatID, Message: "hello", Origin: models.MessageOriginUser,
			ReadStatus: "read", SentAt: now,
		}),
		"chat_message_context_items.jsonl": accountBackupJSONL(t, models.AccountBackupChatMessageContextItem{
			ID: contextItemID, ChatMessageID: chatMessageID, Type: "note", Content: "ctx",
		}),
		"tool_calls.jsonl": accountBackupJSONL(t, models.AccountBackupToolCall{
			ID: toolCallID, ChatMessageID: chatMessageID, ToolName: "search",
			CreatedAt: now, UpdatedAt: now,
		}),
		"agent_jobs.jsonl": accountBackupJSONL(t, models.AccountBackupAgentJob{
			ID: agentJobID, Prompt: "do things", ScheduleInput: "daily",
			ScheduleType: models.AgentJobScheduleTypeCron, Timezone: "UTC",
			Status: models.AgentJobStatusActive, CreatedAt: now, UpdatedAt: now,
		}),
	}
	zr := buildZipReaderForTest(t, entries)

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, func(context.Context, string) ([]float32, error) {
		return []float32{0.1}, nil
	})
	require.NoError(t, err)

	require.Equal(t, models.AccountBackupFormatVersion, result.FormatVersion)
	require.Equal(t, 1, result.Sections["moods"].Created)
	require.Equal(t, 1, result.Sections["personalities"].Created)
	require.Equal(t, 1, result.Sections["personality_moods"].Created)
	require.Equal(t, 1, result.Sections["rituals"].Created)
	require.Equal(t, 1, result.Sections["chats"].Created)
	require.Equal(t, 1, result.Sections["chat_messages"].Created)
	require.Equal(t, 1, result.Sections["chat_message_context_items"].Created)
	require.Equal(t, 1, result.Sections["tool_calls"].Created)
	require.Equal(t, 1, result.Sections["agent_jobs"].Created)

	moodOK, err := ds.moodBelongsToUser(ctx, userID, moodID)
	require.NoError(t, err)
	require.True(t, moodOK)
	personalityOK, err := ds.personalityBelongsToUser(ctx, userID, personalityID)
	require.NoError(t, err)
	require.True(t, personalityOK)
	ritualOK, err := ds.ritualBelongsToUser(ctx, userID, ritualID)
	require.NoError(t, err)
	require.True(t, ritualOK)
	chatOK, err := ds.chatBelongsToUser(ctx, userID, chatID)
	require.NoError(t, err)
	require.True(t, chatOK)
	chatMessageOK, err := ds.chatMessageBelongsToUser(ctx, userID, chatMessageID)
	require.NoError(t, err)
	require.True(t, chatMessageOK)
	contextItemOK, err := ds.contextItemBelongsToUser(ctx, userID, contextItemID)
	require.NoError(t, err)
	require.True(t, contextItemOK)
	toolCallOK, err := ds.toolCallBelongsToUser(ctx, userID, toolCallID)
	require.NoError(t, err)
	require.True(t, toolCallOK)
	agentJobOK, err := ds.agentJobBelongsToUser(ctx, userID, agentJobID)
	require.NoError(t, err)
	require.True(t, agentJobOK)

	linked, err := ds.dbClient.Personality.Query().
		Where(personality.ID(personalityID), personality.HasMoodsWith(mood.ID(moodID))).
		Exist(ctx)
	require.NoError(t, err)
	require.True(t, linked, "personality_moods join row should exist")
}

func TestAdminImportAccountBackup_MoodDuplicateAndConflict(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	otherUserID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	createAccountBackupTestUser(t, ds, otherUserID)
	createAccountBackupTestModelAndPreference(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)

	// Owned by target user already -> re-import is a duplicate no-op.
	existingOwnID := uuid.New()
	_, err := ds.dbClient.Mood.Create().
		SetID(existingOwnID).SetName("Existing").SetOwnerID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	// Owned by a different user -> conflict.
	conflictID := uuid.New()
	_, err = ds.dbClient.Mood.Create().
		SetID(conflictID).SetName("Other's").SetOwnerID(otherUserID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	newID := uuid.New()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"moods.jsonl": accountBackupJSONL(t,
			models.AccountBackupMood{ID: existingOwnID, Name: "Existing", CreatedAt: now, UpdatedAt: now},
			models.AccountBackupMood{ID: conflictID, Name: "Other's", CreatedAt: now, UpdatedAt: now},
			models.AccountBackupMood{ID: newID, Name: "New", CreatedAt: now, UpdatedAt: now},
		),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, models.AccountBackupSectionResult{Created: 1, Duplicate: 1, Conflict: 1}, result.Sections["moods"])

	ok, err := ds.moodBelongsToUser(ctx, userID, newID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdminImportAccountBackup_ChatDuplicateAndConflict(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	otherUserID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	createAccountBackupTestUser(t, ds, otherUserID)
	modelID := createAccountBackupTestModelAndPreference(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)

	existingOwnID := uuid.New()
	_, err := ds.dbClient.Chat.Create().
		SetID(existingOwnID).SetName("Existing Chat").SetOwnerID(userID).SetModelID(modelID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	conflictID := uuid.New()
	_, err = ds.dbClient.Chat.Create().
		SetID(conflictID).SetName("Other's Chat").SetOwnerID(otherUserID).SetModelID(modelID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	newID := uuid.New()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"chats.jsonl": accountBackupJSONL(t,
			models.AccountBackupChat{ID: existingOwnID, Name: "Existing Chat", CreatedAt: now, UpdatedAt: now},
			models.AccountBackupChat{ID: conflictID, Name: "Other's Chat", CreatedAt: now, UpdatedAt: now},
			models.AccountBackupChat{ID: newID, Name: "New Chat", CreatedAt: now, UpdatedAt: now},
		),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sections["chats"].Created)
	require.Equal(t, 1, result.Sections["chats"].Duplicate)
	require.Equal(t, 1, result.Sections["chats"].Conflict)

	ok, err := ds.chatBelongsToUser(ctx, userID, newID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdminImportAccountBackup_ChatMessageMissingRefChat(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	createAccountBackupTestModelAndPreference(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)
	missingChatID := uuid.New()
	msgID := uuid.New()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"chat_messages.jsonl": accountBackupJSONL(t, models.AccountBackupChatMessage{
			ID: msgID, ChatID: missingChatID, Message: "orphan", Origin: models.MessageOriginUser,
			ReadStatus: "read", SentAt: now,
		}),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, models.AccountBackupSectionResult{MissingRef: 1}, result.Sections["chat_messages"])

	exists, err := ds.dbClient.ChatMessage.Query().Where(chatmessage.ID(msgID)).Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestAdminImportAccountBackup_PersonalityMoodMissingRef(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)
	personalityID := uuid.New()
	_, err := ds.dbClient.Personality.Create().
		SetID(personalityID).SetName("Vix").SetSystemPrompt("sp").SetUserID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	missingMoodID := uuid.New()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"personality_moods.jsonl": accountBackupJSONL(t, models.AccountBackupPersonalityMood{
			PersonalityID: personalityID, MoodID: missingMoodID,
		}),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, models.AccountBackupSectionResult{MissingRef: 1}, result.Sections["personality_moods"])
}

func TestAdminImportAccountBackup_ChatMissingPersonalityAndMoodRefsStillCreatesChat(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	createAccountBackupTestModelAndPreference(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)
	chatID := uuid.New()
	missingPersonalityID := uuid.New()
	missingMoodID := uuid.New()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"chats.jsonl": accountBackupJSONL(t, models.AccountBackupChat{
			ID: chatID, Name: "Chat", PersonalityID: &missingPersonalityID, ActiveMoodID: &missingMoodID,
			CreatedAt: now, UpdatedAt: now,
		}),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sections["chats"].Created)
	require.Equal(t, 2, result.Sections["chats"].MissingRef)

	ok, err := ds.chatBelongsToUser(ctx, userID, chatID)
	require.NoError(t, err)
	require.True(t, ok, "chat should still be created despite missing refs")
}

func TestAdminImportAccountBackup_InvalidRitualAndPersonality(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"personalities.jsonl": accountBackupJSONL(t, models.AccountBackupPersonality{
			ID: uuid.New(), Name: "  ", SystemPrompt: "sp", CreatedAt: now, UpdatedAt: now,
		}),
		"rituals.jsonl": accountBackupJSONL(t, models.AccountBackupRitual{
			ID: uuid.New(), Name: "ok", Description: "", Content: "content", CreatedAt: now, UpdatedAt: now,
		}),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, models.AccountBackupSectionResult{Invalid: 1}, result.Sections["personalities"])
	require.Equal(t, models.AccountBackupSectionResult{Invalid: 1}, result.Sections["rituals"])
}

func TestReadBackupJSONL_SkipsMalformedLinesAndCountsInvalid(t *testing.T) {
	validID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	content := strings.Join([]string{
		accountBackupJSONL(t, models.AccountBackupMood{ID: validID, Name: "ok", CreatedAt: now, UpdatedAt: now}),
		`{"id":`,
		"",
		"   ",
	}, "\n")

	zr := buildZipReaderForTest(t, map[string]string{"moods.jsonl": content})

	records, invalid, err := readBackupJSONL[models.AccountBackupMood](zr, "moods.jsonl")
	require.NoError(t, err)
	require.Equal(t, 1, invalid)
	require.Len(t, records, 1)
	require.Equal(t, validID, records[0].ID)
}

func TestReadBackupJSONL_MissingFileReturnsNoError(t *testing.T) {
	zr := buildZipReaderForTest(t, map[string]string{"other.jsonl": ""})

	records, invalid, err := readBackupJSONL[models.AccountBackupMood](zr, "moods.jsonl")
	require.NoError(t, err)
	require.Zero(t, invalid)
	require.Nil(t, records)
}

func TestDefaultModelIDForUser(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)

	_, err := ds.defaultModelIDForUser(ctx, userID)
	require.ErrorIs(t, err, ErrAccountBackupDefaultModelMissing)

	modelID := createAccountBackupTestModelAndPreference(t, ds, userID)

	got, err := ds.defaultModelIDForUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, modelID, got)
}

func TestOpenBackupEntry_NotFound(t *testing.T) {
	zr := buildZipReaderForTest(t, map[string]string{"other.json": "{}"})

	_, err := openBackupEntry(zr, "manifest.json")
	require.Error(t, err)
	require.ErrorIs(t, err, errBackupEntryNotFound)
}

func TestReadBackupManifest_MissingFileWrapsErrBackupEntryNotFound(t *testing.T) {
	zr := buildZipReaderForTest(t, map[string]string{"other.json": "{}"})

	_, err := readBackupManifest(zr)
	require.Error(t, err)
	require.ErrorIs(t, err, errBackupEntryNotFound)
}

func TestUserOwnedIDState(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	otherUserID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	createAccountBackupTestUser(t, ds, otherUserID)

	now := time.Now().UTC().Truncate(time.Second)
	ownedID := uuid.New()
	_, err := ds.dbClient.Mood.Create().
		SetID(ownedID).SetName("mine").SetOwnerID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	conflictID := uuid.New()
	_, err = ds.dbClient.Mood.Create().
		SetID(conflictID).SetName("theirs").SetOwnerID(otherUserID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	tests := []struct {
		name string
		id   uuid.UUID
		want accountBackupIDState
	}{
		{name: "nil id is a conflict", id: uuid.Nil, want: accountBackupIDConflict},
		{name: "owned by target user is a duplicate", id: ownedID, want: accountBackupIDDuplicate},
		{name: "owned by another user is a conflict", id: conflictID, want: accountBackupIDConflict},
		{name: "unknown id is missing", id: uuid.New(), want: accountBackupIDMissing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ds.userOwnedIDState(ctx, accountBackupEntityMood, tc.id, userID)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEntityExistsAndBelongsToUser_UnknownEntity(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := ds.entityExists(ctx, accountBackupEntity("bogus"), uuid.New())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown account backup entity")

	_, err = ds.entityBelongsToUser(ctx, accountBackupEntity("bogus"), uuid.New(), uuid.New())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown account backup entity")
}

func TestEntityExistsAndBelongsToUser_PerEntity(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	now := time.Now().UTC().Truncate(time.Second)

	ritualID := uuid.New()
	_, err := ds.dbClient.Ritual.Create().
		SetID(ritualID).SetOwnerID(userID).SetName("r").SetDescription("d").SetContent("c").SetHotkeys("").
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	agentJobID := uuid.New()
	_, err = ds.dbClient.AgentJob.Create().
		SetID(agentJobID).SetOwnerID(userID).SetPrompt("p").SetScheduleInput("daily").
		SetScheduleType(agentjob.ScheduleTypeCron).SetStatus(agentjob.StatusActive).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	for _, entity := range []accountBackupEntity{accountBackupEntityRitual, accountBackupEntityAgentJob} {
		var id uuid.UUID
		switch entity {
		case accountBackupEntityRitual:
			id = ritualID
		case accountBackupEntityAgentJob:
			id = agentJobID
		}
		exists, err := ds.entityExists(ctx, entity, id)
		require.NoError(t, err)
		require.True(t, exists, "entityExists should find %s", entity)

		belongs, err := ds.entityBelongsToUser(ctx, entity, id, userID)
		require.NoError(t, err)
		require.True(t, belongs, "entityBelongsToUser should find %s owned by user", entity)

		exists, err = ds.entityExists(ctx, entity, uuid.New())
		require.NoError(t, err)
		require.False(t, exists)
	}
}

func TestAdminImportAccountBackup_AgentJobFullFieldsAndDuplicate(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	modelID := createAccountBackupTestModelAndPreference(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)
	personalityID := uuid.New()
	_, err := ds.dbClient.Personality.Create().
		SetID(personalityID).SetName("Vix").SetSystemPrompt("sp").SetUserID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	chatID := uuid.New()
	_, err = ds.dbClient.Chat.Create().
		SetID(chatID).SetName("Chat").SetOwnerID(userID).SetModelID(modelID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	m, err := ds.dbClient.Model.Query().Only(ctx)
	require.NoError(t, err)

	title := "Weekly digest"
	schedule := "0 9 * * MON"
	agentJobID := uuid.New()
	missingChatID := uuid.New()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"agent_jobs.jsonl": accountBackupJSONL(t,
			models.AccountBackupAgentJob{
				ID: agentJobID, ChatID: &chatID, PersonalityID: &personalityID, ModelName: m.Name,
				Title: &title, Prompt: "summarize", ScheduleInput: "weekly",
				ScheduleType: models.AgentJobScheduleTypeCron, Schedule: &schedule, Timezone: "UTC",
				Status: models.AgentJobStatusActive, RunCount: 3, LastError: "previous failure",
				CreatedAt: now, UpdatedAt: now,
			},
			models.AccountBackupAgentJob{
				ID: uuid.New(), ChatID: &missingChatID, Prompt: "orphan", ScheduleInput: "once",
				ScheduleType: models.AgentJobScheduleTypeAt, Timezone: "UTC",
				Status: models.AgentJobStatusPaused, CreatedAt: now, UpdatedAt: now,
			},
			models.AccountBackupAgentJob{
				ID: agentJobID, Prompt: "summarize", ScheduleInput: "weekly",
				ScheduleType: models.AgentJobScheduleTypeCron, Timezone: "UTC",
				Status: models.AgentJobStatusActive, CreatedAt: now, UpdatedAt: now,
			},
		),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, 2, result.Sections["agent_jobs"].Created)
	require.Equal(t, 1, result.Sections["agent_jobs"].MissingRef)
	require.Equal(t, 1, result.Sections["agent_jobs"].Duplicate)

	ok, err := ds.agentJobBelongsToUser(ctx, userID, agentJobID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdminImportAccountBackup_RitualWithPersonalityRef(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)
	personalityID := uuid.New()
	_, err := ds.dbClient.Personality.Create().
		SetID(personalityID).SetName("Vix").SetSystemPrompt("sp").SetUserID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	ritualWithRefID := uuid.New()
	ritualMissingRefID := uuid.New()
	missingPersonalityID := uuid.New()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"rituals.jsonl": accountBackupJSONL(t,
			models.AccountBackupRitual{
				ID: ritualWithRefID, Name: "Linked", Description: "d", Content: "c",
				PersonalityID: &personalityID, CreatedAt: now, UpdatedAt: now,
			},
			models.AccountBackupRitual{
				ID: ritualMissingRefID, Name: "Orphan", Description: "d", Content: "c",
				PersonalityID: &missingPersonalityID, CreatedAt: now, UpdatedAt: now,
			},
		),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, 2, result.Sections["rituals"].Created)
	require.Equal(t, 1, result.Sections["rituals"].MissingRef)

	ok, err := ds.ritualBelongsToUser(ctx, userID, ritualWithRefID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdminImportAccountBackup_ChatMessageDuplicateAndContextItemToolCallGenerationMood(t *testing.T) {
	ds, cleanup := newAccountBackupTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := uuid.New()
	createAccountBackupTestUser(t, ds, userID)
	modelID := createAccountBackupTestModelAndPreference(t, ds, userID)

	now := time.Now().UTC().Truncate(time.Second)
	chatID := uuid.New()
	_, err := ds.dbClient.Chat.Create().
		SetID(chatID).SetName("Chat").SetOwnerID(userID).SetModelID(modelID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	moodID := uuid.New()
	_, err = ds.dbClient.Mood.Create().
		SetID(moodID).SetName("Curious").SetOwnerID(userID).
		SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	require.NoError(t, err)

	existingMsgID := uuid.New()
	_, err = ds.dbClient.ChatMessage.Create().
		SetID(existingMsgID).SetMessage("hi").SetOrigin(chatmessage.OriginUser).
		SetReadStatus(chatmessage.ReadStatusRead).SetChatID(chatID).SetSentAt(now).Save(ctx)
	require.NoError(t, err)

	newMsgID := uuid.New()
	missingMoodID := uuid.New()

	zr := buildZipReaderForTest(t, map[string]string{
		"manifest.json": `{"format_version":1}`,
		"chat_messages.jsonl": accountBackupJSONL(t,
			models.AccountBackupChatMessage{
				ID: existingMsgID, ChatID: chatID, Message: "hi", Origin: models.MessageOriginUser,
				ReadStatus: "read", SentAt: now,
			},
			models.AccountBackupChatMessage{
				ID: newMsgID, ChatID: chatID, Message: "with mood", Origin: models.MessageOriginAssistant,
				ReadStatus: "read", GenerationModel: "gpt-test", GenerationPersonality: "Vix",
				GenerationMoodID: &moodID, SentAt: now, Tokens: 42,
			},
			models.AccountBackupChatMessage{
				ID: uuid.New(), ChatID: chatID, Message: "missing mood", Origin: models.MessageOriginAssistant,
				ReadStatus: "read", GenerationMoodID: &missingMoodID, SentAt: now,
			},
		),
		"chat_message_context_items.jsonl": accountBackupJSONL(t, models.AccountBackupChatMessageContextItem{
			ID: uuid.New(), ChatMessageID: newMsgID, Type: "note", Content: "ctx",
		}),
		"tool_calls.jsonl": accountBackupJSONL(t, models.AccountBackupToolCall{
			ID: uuid.New(), ChatMessageID: newMsgID, ToolName: "search",
			ToolInput: "q", ToolOutput: "result", ToolError: "",
			CreatedAt: now, UpdatedAt: now,
		}),
	})

	result, err := ds.AdminImportAccountBackup(ctx, userID, zr, nil)
	require.NoError(t, err)
	require.Equal(t, 2, result.Sections["chat_messages"].Created)
	require.Equal(t, 1, result.Sections["chat_messages"].Duplicate)
	require.Equal(t, 1, result.Sections["chat_messages"].MissingRef)
	require.Equal(t, 1, result.Sections["chat_message_context_items"].Created)
	require.Equal(t, 1, result.Sections["tool_calls"].Created)
}

func TestReadBackupJSONLAllowsLargeRecords(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("chat_messages.jsonl")
	require.NoError(t, err)

	message := strings.Repeat("x", 256*1024)
	_, err = w.Write([]byte(`{"message":"` + message + `"}` + "\n"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	records, invalid, err := readBackupJSONL[models.AccountBackupChatMessage](zr, "chat_messages.jsonl")
	require.NoError(t, err)
	require.Zero(t, invalid)
	require.Len(t, records, 1)
	require.Equal(t, message, records[0].Message)
}

func TestReadBackupJSONLMissingSectionIsEmpty(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	_, err := zw.Create("manifest.json")
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	records, invalid, err := readBackupJSONL[models.AccountBackupMood](zr, "moods.jsonl")
	require.NoError(t, err)
	require.Zero(t, invalid)
	require.Empty(t, records)
}

func TestReadBackupJSONLRituals(t *testing.T) {
	pid := uuid.New()
	rec := models.AccountBackupRitual{
		ID:            uuid.New(),
		Name:          "Skill",
		Description:   "desc",
		Content:       "body",
		Hotkeys:       "meta+k",
		PersonalityID: &pid,
		CreatedAt:     time.Unix(1700000000, 0).UTC(),
		UpdatedAt:     time.Unix(1700000001, 0).UTC(),
	}
	line, err := json.Marshal(rec)
	require.NoError(t, err)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("rituals.jsonl")
	require.NoError(t, err)
	_, err = w.Write(append(line, '\n'))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	got, invalid, err := readBackupJSONL[models.AccountBackupRitual](zr, "rituals.jsonl")
	require.NoError(t, err)
	require.Zero(t, invalid)
	require.Len(t, got, 1)
	require.Equal(t, rec.ID, got[0].ID)
	require.Equal(t, rec.Name, got[0].Name)
	require.Equal(t, rec.PersonalityID, got[0].PersonalityID)
}
