package modeltypes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRestoreGuidance(t *testing.T) {
	got, err := NormalizeRestoreGuidance(" \nKeep decisions. Omit abandoned plans.\t")
	require.NoError(t, err)
	require.Equal(t, "Keep decisions. Omit abandoned plans.", got)
	_, err = NormalizeRestoreGuidance(strings.Repeat("界", MaxRestoreGuidanceRunes))
	require.NoError(t, err, "the limit counts Unicode characters, not bytes")
	_, err = NormalizeRestoreGuidance(strings.Repeat("a", MaxRestoreGuidanceRunes+1))
	require.Error(t, err)
	_, err = NormalizeRestoreGuidance(string([]byte{0xff}))
	require.Error(t, err)
}
