package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeFileNameKeepsTheCurrentExtensionWhenTheNewNameHasNone(t *testing.T) {
	for raw, want := range map[string]string{
		"  self-portrait  ":      "self-portrait.png",
		"self-portrait.webp":     "self-portrait.webp",
		"v1.2 notes":             "v1.2 notes.png", // ".2 notes" is not an extension
		"Vix, March 2025":        "Vix, March 2025.png",
		"archive.tar.gz":         "archive.tar.gz",
		".hidden":                ".hidden.png",
		"portrait.":              "portrait..png",
		"portrait.thisistoolong": "portrait.thisistoolong.png",
	} {
		got, err := NormalizeFileName(raw, "3f2a9c1e-uuid.png")
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}

	got, err := NormalizeFileName("notes", "")
	require.NoError(t, err)
	assert.Equal(t, "notes", got, "nothing to keep when the current name has no extension")
}

func TestNormalizeFileNameRefusesBadNames(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":     "   ",
		"slash":     "charts/hrv.png",
		"backslash": `charts\hrv.png`,
		"control":   "a\x00b.png",
		"too long":  strings.Repeat("n", MaxFileNameRunes+1),
	} {
		_, err := NormalizeFileName(raw, "x.png")
		assert.ErrorIs(t, err, ErrInvalidFileName, name)
	}

	_, err := NormalizeFileName(strings.Repeat("n", MaxFileNameRunes-2), "x.png")
	require.ErrorIs(t, err, ErrInvalidFileName)
	assert.Contains(t, err.Error(), ".png extension it keeps", "says why a name that looked short enough is too long")
}
