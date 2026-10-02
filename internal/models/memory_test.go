package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryConfidence_Float(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   MemoryConfidence
		want float64
	}{
		{name: "low", in: MemoryConfidenceLow, want: 0.3},
		{name: "medium", in: MemoryConfidenceMedium, want: DefaultMemoryConfidence},
		{name: "high", in: MemoryConfidenceHigh, want: 0.9},
		{name: "empty defaults to medium anchor", in: "", want: DefaultMemoryConfidence},
		{name: "unknown defaults to medium anchor", in: MemoryConfidence("bogus"), want: DefaultMemoryConfidence},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.in.Float())
		})
	}
}

func TestMemoryConfidenceFromFloat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want MemoryConfidence
	}{
		{name: "zero is low", in: 0, want: MemoryConfidenceLow},
		{name: "just below low/medium boundary is low", in: 0.4499, want: MemoryConfidenceLow},
		{name: "low/medium boundary is medium (inclusive)", in: 0.45, want: MemoryConfidenceMedium},
		{name: "mid-range is medium", in: 0.6, want: MemoryConfidenceMedium},
		{name: "just below medium/high boundary is medium", in: 0.7499, want: MemoryConfidenceMedium},
		{name: "medium/high boundary is high (inclusive)", in: 0.75, want: MemoryConfidenceHigh},
		{name: "one is high", in: 1, want: MemoryConfidenceHigh},
		{name: "negative treated as low", in: -0.5, want: MemoryConfidenceLow},
		{name: "above one treated as high", in: 1.5, want: MemoryConfidenceHigh},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, MemoryConfidenceFromFloat(tc.in))
		})
	}
}

func TestMemoryConfidence_FloatAndFromFloat_RoundTripBuckets(t *testing.T) {
	t.Parallel()

	// Each bucket's stored anchor must map back to the same bucket.
	for _, bucket := range []MemoryConfidence{MemoryConfidenceLow, MemoryConfidenceMedium, MemoryConfidenceHigh} {
		require.Equal(t, bucket, MemoryConfidenceFromFloat(bucket.Float()))
	}
}

func TestClampConfidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{name: "within range is unchanged", in: 0.5, want: 0.5},
		{name: "zero is unchanged", in: 0, want: 0},
		{name: "one is unchanged", in: 1, want: 1},
		{name: "negative clamps to zero", in: -0.1, want: 0},
		{name: "above one clamps to one", in: 1.1, want: 1},
		{name: "large negative clamps to zero", in: -1000, want: 0},
		{name: "large positive clamps to one", in: 1000, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ClampConfidence(tc.in))
		})
	}
}
