package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryMergeUndoSnapshotUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name            string
		snapshot        string
		wantConfidence  float64
		wantLegacyError bool
	}{
		{
			name:           "numeric confidence",
			snapshot:       `{"prior_confidence":0.6}`,
			wantConfidence: 0.6,
		},
		{
			name:           "legacy medium confidence",
			snapshot:       `{"prior_confidence":"medium"}`,
			wantConfidence: 0.6,
		},
		{
			name:           "legacy low confidence",
			snapshot:       `{"prior_confidence":"low"}`,
			wantConfidence: 0.3,
		},
		{
			name:            "invalid legacy confidence",
			snapshot:        `{"prior_confidence":"unknown"}`,
			wantLegacyError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var snapshot MemoryMergeUndoSnapshot
			err := json.Unmarshal([]byte(tt.snapshot), &snapshot)

			if tt.wantLegacyError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantConfidence, snapshot.PriorConfidence)
		})
	}
}
