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

func TestCreateAgentJobToolArgs_ParsesThreadID(t *testing.T) {
	t.Parallel()

	raw := `{"schedule_input":"every day at 8am","prompt":"check in","thread_id":"current"}`
	var args createAgentJobToolArgs
	require.NoError(t, json.Unmarshal([]byte(raw), &args))
	require.Equal(t, "current", args.ThreadID)
	require.Nil(t, args.UseCurrentThread)
}

func TestCreateAgentJobToolArgs_FollowUserTimezoneIsOffUnlessPassed(t *testing.T) {
	t.Parallel()

	var args createAgentJobToolArgs
	require.NoError(t, json.Unmarshal([]byte(`{"schedule_input":"every day at 8am","prompt":"check in"}`), &args))
	require.False(t, args.FollowUserTimezone)

	require.NoError(t, json.Unmarshal([]byte(`{"schedule_input":"every day at 8am","prompt":"check in","follow_user_timezone":true}`), &args))
	require.True(t, args.FollowUserTimezone)
}
