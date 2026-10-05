package schema

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
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

// The fold-rewrite / exact-undo fields round-trip through the custom UnmarshalJSON, and a legacy
// row (only the original three keys) decodes with every new field at its zero value.
func TestMemoryMergeUndoSnapshotRoundTripsUndoFields(t *testing.T) {
	absorbedID := uuid.New()
	in := MemoryMergeUndoSnapshot{
		PriorConfidence:          0.45,
		PriorChainMetadataWasNil: true,
		Version:                  MemoryMergeUndoSnapshotVersion,
		CanonicalContent:         "Prefers dark mode in every editor",
		ContentRewritten:         true,
		PriorContent:             "Prefers dark mode",
		PriorEmbedding:           []float32{0.125, -1, 3.5e-7},
		AbsorbedMembers:          []MemoryMergeAbsorbedMember{{MemoryID: absorbedID, PriorStatus: "active"}},
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)

	var out MemoryMergeUndoSnapshot
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, in, out)

	var legacy MemoryMergeUndoSnapshot
	require.NoError(t, json.Unmarshal([]byte(`{"prior_confidence":"high","prior_chain_metadata_was_nil":true}`), &legacy))
	require.Equal(t, MemoryMergeUndoSnapshot{PriorConfidence: 0.9, PriorChainMetadataWasNil: true}, legacy)
}
