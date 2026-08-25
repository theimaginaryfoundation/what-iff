package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecideCheckpoint(t *testing.T) {
	t.Parallel()

	policy := checkpointPolicy{
		MinAssistantMessagesSinceCheckpoint: 5,
		MaxLastInputTokens:                  20_000,
		MaxEstimatedContextTokens:           25_000,
	}

	tests := []struct {
		name string
		in   checkpointInputs
		want checkpointDecision
	}{
		{
			name: "no checkpoint early",
			in: checkpointInputs{
				TotalAssistantMessages:          3,
				CheckpointAssistantMessageCount: 0,
				LastInputTokens:                 5000,
				EstimatedContextTokens:          7000,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            false,
				Reason:                      "",
				AssistantMessagesSinceCheck: 3,
			},
		},
		{
			name: "checkpoint on message count threshold",
			in: checkpointInputs{
				TotalAssistantMessages:          10,
				CheckpointAssistantMessageCount: 5,
				LastInputTokens:                 1000,
				EstimatedContextTokens:          2000,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            true,
				Reason:                      "assistant_messages_since_checkpoint(5) >= 5",
				AssistantMessagesSinceCheck: 5,
			},
		},
		{
			name: "checkpoint on last input tokens threshold",
			in: checkpointInputs{
				TotalAssistantMessages:          1,
				CheckpointAssistantMessageCount: 0,
				LastInputTokens:                 20_000,
				EstimatedContextTokens:          1000,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            true,
				Reason:                      "last_input_tokens(20000) >= 20000",
				AssistantMessagesSinceCheck: 1,
			},
		},
		{
			name: "checkpoint on estimated context tokens threshold",
			in: checkpointInputs{
				TotalAssistantMessages:          1,
				CheckpointAssistantMessageCount: 0,
				LastInputTokens:                 10,
				EstimatedContextTokens:          25_000,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            true,
				Reason:                      "estimated_context_tokens(25000) >= 25000",
				AssistantMessagesSinceCheck: 1,
			},
		},
		{
			name: "defensive: negative inputs normalize to zero",
			in: checkpointInputs{
				TotalAssistantMessages:          -10,
				CheckpointAssistantMessageCount: -5,
				LastInputTokens:                 -1,
				EstimatedContextTokens:          -1,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            false,
				Reason:                      "",
				AssistantMessagesSinceCheck: 0,
			},
		},
		{
			name: "defensive: checkpoint count greater than total clamps at zero",
			in: checkpointInputs{
				TotalAssistantMessages:          3,
				CheckpointAssistantMessageCount: 100,
				LastInputTokens:                 10,
				EstimatedContextTokens:          10,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            false,
				Reason:                      "",
				AssistantMessagesSinceCheck: 0,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decideCheckpoint(policy, tc.in)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDecideCheckpoint_MinTurnsBetweenCheckpoints(t *testing.T) {
	t.Parallel()

	policy := checkpointPolicy{
		MinAssistantMessagesSinceCheckpoint: 20,
		MaxLastInputTokens:                  30_000,
		MaxEstimatedContextTokens:           32_000,
		MinTurnsBetweenCheckpoints:          5,
	}

	tests := []struct {
		name string
		in   checkpointInputs
		want checkpointDecision
	}{
		{
			name: "token trigger suppressed before min turns",
			in: checkpointInputs{
				TotalAssistantMessages:          2,
				CheckpointAssistantMessageCount: 0,
				LastInputTokens:                 80_000,
				EstimatedContextTokens:          90_000,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            false,
				Reason:                      "",
				AssistantMessagesSinceCheck: 2,
			},
		},
		{
			name: "token trigger fires once min turns reached",
			in: checkpointInputs{
				TotalAssistantMessages:          5,
				CheckpointAssistantMessageCount: 0,
				LastInputTokens:                 80_000,
				EstimatedContextTokens:          1000,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            true,
				Reason:                      "last_input_tokens(80000) >= 30000",
				AssistantMessagesSinceCheck: 5,
			},
		},
		{
			name: "scheduled turn-count trigger is exempt from throttle",
			in: checkpointInputs{
				TotalAssistantMessages:          20,
				CheckpointAssistantMessageCount: 0,
				LastInputTokens:                 10,
				EstimatedContextTokens:          10,
			},
			want: checkpointDecision{
				ShouldCheckpoint:            true,
				Reason:                      "assistant_messages_since_checkpoint(20) >= 20",
				AssistantMessagesSinceCheck: 20,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decideCheckpoint(policy, tc.in)
			require.Equal(t, tc.want, got)
		})
	}
}
