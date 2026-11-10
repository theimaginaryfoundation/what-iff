package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
