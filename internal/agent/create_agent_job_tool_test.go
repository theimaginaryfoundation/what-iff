package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateAgentJobToolArgs_ParsesOptionalFields(t *testing.T) {
	t.Parallel()

	raw := `{
		"schedule_input": "in an hour",
		"prompt": "remind me",
		"model_override": "gpt-4",
		"use_current_thread": true
	}`

	var args createAgentJobToolArgs
	require.NoError(t, json.Unmarshal([]byte(raw), &args))
	require.Equal(t, "in an hour", args.ScheduleInput)
	require.Equal(t, "remind me", args.Prompt)
	require.Equal(t, "gpt-4", args.ModelOverride)
	require.NotNil(t, args.UseCurrentThread)
	require.True(t, *args.UseCurrentThread)
}

func TestCreateAgentJobToolArgs_UseCurrentThreadOmittedDefaultsNil(t *testing.T) {
	t.Parallel()

	raw := `{"schedule_input":"in an hour","prompt":"remind me"}`
	var args createAgentJobToolArgs
	require.NoError(t, json.Unmarshal([]byte(raw), &args))
	require.Nil(t, args.UseCurrentThread)
	require.Empty(t, args.ModelOverride)
}
