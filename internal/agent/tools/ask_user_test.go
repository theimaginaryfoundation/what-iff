package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestParseAskUserArgs_KeepsOptional(t *testing.T) {
	payload, err := ParseAskUserArgs([]byte(`{"questions":[
		{"question":"Which format?","options":[{"label":"PDF"},{"label":"Markdown"}]},
		{"question":"Anything else I should know?","optional":true}
	]}`))
	require.NoError(t, err)
	require.Len(t, payload.Questions, 2)
	assert.False(t, payload.Questions[0].Optional)
	assert.True(t, payload.Questions[1].Optional)
	assert.Equal(t, "q2", payload.Questions[1].ID)
}

// Under OpenAI's strict function calling every property is required, so the half the model isn't
// using arrives filled in but empty. That must not read as sending both.
func TestParseAskUserArgs_EmptyOtherHalfIsIgnored(t *testing.T) {
	questions, err := ParseAskUserArgs([]byte(`{
		"questions":[{"question":"Which style?","header":"","options":[],"multi_select":false,"optional":false}],
		"approval":{"action":"","details":""}
	}`))
	require.NoError(t, err)
	assert.Equal(t, models.AgentPromptKindQuestions, questions.Kind)
	require.Len(t, questions.Questions, 1)

	approval, err := ParseAskUserArgs([]byte(`{
		"questions":[],
		"approval":{"action":"Generate the portrait","details":"Uses one image credit."}
	}`))
	require.NoError(t, err)
	assert.Equal(t, models.AgentPromptKindApproval, approval.Kind)
	assert.Equal(t, "Generate the portrait", approval.Title)

	blankQuestion, err := ParseAskUserArgs([]byte(`{
		"questions":[{"question":"  "}],
		"approval":{"action":"Delete the drafts"}
	}`))
	require.NoError(t, err)
	assert.Equal(t, models.AgentPromptKindApproval, blankQuestion.Kind)
}

func TestParseAskUserArgs_BothWithContentIsRefused(t *testing.T) {
	_, err := ParseAskUserArgs([]byte(`{
		"questions":[{"question":"Which style?"}],
		"approval":{"action":"Generate the portrait"}
	}`))
	assert.ErrorContains(t, err, "not both")
}

// With neither half filled in, the error names what's missing.
func TestParseAskUserArgs_NothingToAskIsRefused(t *testing.T) {
	_, err := ParseAskUserArgs([]byte(`{"questions":[],"approval":{"action":" "}}`))
	assert.ErrorContains(t, err, "approval.action is required")

	_, err = ParseAskUserArgs([]byte(`{}`))
	assert.ErrorContains(t, err, "at least one question")

	_, err = ParseAskUserArgs([]byte(`{"questions":[{"question":""}],"approval":{"action":""}}`))
	assert.ErrorContains(t, err, "question 1 is empty")
}
