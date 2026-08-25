package models

import (
	"time"

	"github.com/google/uuid"
)

const AccountBackupFormatVersion = 1

type AccountBackupManifest struct {
	FormatVersion int `json:"format_version"`
}

type AccountBackupPersonality struct {
	ID                     uuid.UUID `json:"id"`
	Name                   string    `json:"name"`
	SystemPrompt           string    `json:"system_prompt"`
	Scratchpad             string    `json:"scratchpad"`
	ScratchpadHistory      []string  `json:"scratchpad_history"`
	ArchivalModel          string    `json:"archival_model"`
	ScratchpadUpdatePrompt string    `json:"scratchpad_update_prompt"`
	MemorySearchPrompt     string    `json:"memory_search_prompt"`
	MemoryWritePrompt      string    `json:"memory_write_prompt"`
	AutoPinMemories        bool      `json:"auto_pin_memories"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type AccountBackupMood struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	PromptSnippet    string    `json:"prompt_snippet"`
	RecommendedModel string    `json:"recommended_model,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AccountBackupPersonalityMood struct {
	PersonalityID uuid.UUID `json:"personality_id"`
	MoodID        uuid.UUID `json:"mood_id"`
}

type AccountBackupChat struct {
	ID                         uuid.UUID  `json:"id"`
	Name                       string     `json:"name"`
	ModelName                  string     `json:"model_name,omitempty"`
	PersonalityID              *uuid.UUID `json:"personality_id,omitempty"`
	ActiveMoodID               *uuid.UUID `json:"active_mood_id,omitempty"`
	LastMessageTime            *time.Time `json:"last_message_time,omitempty"`
	CheckpointSummary          string     `json:"checkpoint_summary"`
	CheckpointUserMessageCount int        `json:"checkpoint_user_message_count"`
	LastCheckpointAt           *time.Time `json:"last_checkpoint_at,omitempty"`
	DisabledTools              []string   `json:"disabled_tools,omitempty"`
	IsAutoMood                 bool       `json:"is_auto_mood"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

type AccountBackupChatMessage struct {
	ID                    uuid.UUID     `json:"id"`
	ChatID                uuid.UUID     `json:"chat_id"`
	Message               string        `json:"message"`
	Origin                MessageOrigin `json:"origin"`
	ReadStatus            string        `json:"read_status"`
	GenerationModel       string        `json:"generation_model,omitempty"`
	GenerationPersonality string        `json:"generation_personality,omitempty"`
	GenerationMoodID      *uuid.UUID    `json:"generation_mood_id,omitempty"`
	SentAt                time.Time     `json:"sent_at"`
	Tokens                int64         `json:"tokens,omitempty"`
}

type AccountBackupChatMessageContextItem struct {
	ID            uuid.UUID `json:"id"`
	ChatMessageID uuid.UUID `json:"chat_message_id"`
	Type          string    `json:"type"`
	Content       string    `json:"content"`
}

type AccountBackupToolCall struct {
	ID            uuid.UUID `json:"id"`
	ChatMessageID uuid.UUID `json:"chat_message_id"`
	ToolName      string    `json:"tool_name"`
	ToolInput     string    `json:"tool_input,omitempty"`
	ToolOutput    string    `json:"tool_output,omitempty"`
	ToolError     string    `json:"tool_error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AccountBackupAgentJob struct {
	ID            uuid.UUID            `json:"id"`
	ChatID        *uuid.UUID           `json:"chat_id,omitempty"`
	PersonalityID *uuid.UUID           `json:"personality_id,omitempty"`
	ModelName     string               `json:"model_name,omitempty"`
	Title         *string              `json:"title,omitempty"`
	Prompt        string               `json:"prompt"`
	ScheduleInput string               `json:"schedule_input"`
	ScheduleType  AgentJobScheduleType `json:"schedule_type"`
	Schedule      *string              `json:"schedule,omitempty"`
	RunAt         *time.Time           `json:"run_at,omitempty"`
	Timezone      string               `json:"timezone"`
	Status        AgentJobStatus       `json:"status"`
	NextRunAt     *time.Time           `json:"next_run_at,omitempty"`
	LastRunAt     *time.Time           `json:"last_run_at,omitempty"`
	LastError     string               `json:"last_error,omitempty"`
	RunCount      int                  `json:"run_count"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

// AccountBackupRitual is a user-owned ritual (skill). Chat-message and MCP
// attachments are not exported in this pass; they can be re-linked in the UI.
type AccountBackupRitual struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Content       string     `json:"content"`
	Hotkeys       string     `json:"hotkeys"`
	PersonalityID *uuid.UUID `json:"personality_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type AccountBackupImportResult struct {
	FormatVersion int                                   `json:"format_version"`
	Sections      map[string]AccountBackupSectionResult `json:"sections"`
	Memories      MemoryImportResult                    `json:"memories"`
}

type AccountBackupSectionResult struct {
	Created    int `json:"created"`
	Duplicate  int `json:"duplicate"`
	Conflict   int `json:"conflict"`
	Skipped    int `json:"skipped"`
	Invalid    int `json:"invalid"`
	MissingRef int `json:"missing_ref"`
}
