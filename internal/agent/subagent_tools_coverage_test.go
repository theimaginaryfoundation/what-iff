package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// --- parseSkillIDs ---

func TestParseSkillIDs(t *testing.T) {
	t.Parallel()
	valid := uuid.New()
	got := parseSkillIDs([]string{valid.String(), "  ", "not-a-uuid", ""})
	require.Equal(t, []uuid.UUID{valid}, got)
}

func TestParseSkillIDs_EmptyInputReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	got := parseSkillIDs(nil)
	require.Empty(t, got)
}

// --- optionalUUIDString ---

func TestOptionalUUIDString(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", optionalUUIDString(uuid.Nil))
	id := uuid.New()
	require.Equal(t, id.String(), optionalUUIDString(id))
}

// --- marshalSubagentToolResult ---

func TestMarshalSubagentToolResult_MarshalErrorIsReturned(t *testing.T) {
	t.Parallel()
	_, err := marshalSubagentToolResult(make(chan int))
	require.Error(t, err)
}

// --- findModelByID ---

func TestFindModelByID_NilDatastoreReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	_, err := a.findModelByID(context.Background(), uuid.New())
	require.Error(t, err)
}

// --- callSubagentModel ---

func TestCallSubagentModel_OpenAIChatCompletionsAPIUnsupported(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	// Gemini models route through the OpenAI-compatible chat-completions API,
	// which subagent calls do not support.
	out, err := a.callSubagentModel(context.Background(), uuid.New(), "gemini-3.5-flash", buildSubagentModelContext("", "", "hi"), nil)
	require.Error(t, err)
	require.Nil(t, out)
	require.Contains(t, err.Error(), "not yet supported")
}

func TestCallSubagentModel_ZAIProviderMissing(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	out, err := a.callSubagentModel(context.Background(), uuid.New(), "glm-5.2", buildSubagentModelContext("", "", "hi"), nil)
	require.Error(t, err)
	require.Nil(t, out)
	require.Contains(t, err.Error(), "ZAI_API_KEY")
}

// --- runSubagentTool ---

func TestRunSubagentTool_InvalidArgsJSONReturnsErrorResult(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	chatCtx := &chatContext{chat: &models.Chat{}}
	out, err := a.runSubagentTool(context.Background(), chatCtx, []byte("not json"))
	require.NoError(t, err)
	var result runSubagentToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "invalid arguments")
}

func TestRunSubagentTool_InvalidPersonalityIDReturnsErrorResult(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	chatCtx := &chatContext{model: "gpt-5-mini", chat: &models.Chat{}}
	out, err := a.runSubagentTool(context.Background(), chatCtx, []byte(`{"message":"hi","personality_id":"not-a-uuid"}`))
	require.NoError(t, err)
	var result runSubagentToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Equal(t, "personality_id must be a valid UUID", result.Error)
}

func TestRunSubagentTool_PersonalityLookupErrorReturnsErrorResult(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	chatCtx := &chatContext{model: "gpt-5-mini", chat: &models.Chat{UserID: uuid.New()}}
	out, err := a.runSubagentTool(context.Background(), chatCtx, []byte(`{"message":"hi","personality_id":"`+uuid.New().String()+`"}`))
	require.NoError(t, err)
	var result runSubagentToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "failed to get personality")
	require.NoError(t, mock.ExpectationsWereMet())
}
