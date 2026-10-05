package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeFolder(t *testing.T) {
	for raw, want := range map[string]string{
		"":                      "",
		"/":                     "",
		"   ":                   "",
		"charts":                "charts",
		"Charts":                "charts",
		"  Charts / Oura  ":     "charts/oura",
		"/charts//oura/":        "charts/oura",
		`charts\oura`:           "charts/oura",
		"Daily Graphs/2026-10":  "daily graphs/2026-10",
		"résumé/Ünïcode":        "résumé/ünïcode",
		"a/b/c/d/e/f/g/h":       "a/b/c/d/e/f/g/h",
		strings.Repeat("a", 64): strings.Repeat("a", 64),
	} {
		got, err := NormalizeFolder(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}

func TestNormalizeFolderRefusesWhatCannotBeAFolder(t *testing.T) {
	for name, raw := range map[string]string{
		"dot":           "charts/./oura",
		"dotdot":        "../secrets",
		"control":       "charts/\x00oura",
		"newline":       "charts/ou\nra",
		"long segment":  strings.Repeat("a", 65),
		"too deep":      "a/b/c/d/e/f/g/h/i",
		"too long path": strings.Repeat(strings.Repeat("a", 60)+"/", 5),
	} {
		_, err := NormalizeFolder(raw)
		assert.ErrorIs(t, err, ErrInvalidFolder, name)
	}
}

func TestFolderIsWithin(t *testing.T) {
	assert.True(t, FolderIsWithin("charts", "charts"))
	assert.True(t, FolderIsWithin("charts/oura", "charts"))
	assert.True(t, FolderIsWithin("anything", ""))
	assert.True(t, FolderIsWithin("", ""))
	assert.False(t, FolderIsWithin("charts-old", "charts"), "a sibling that shares a prefix is not inside")
	assert.False(t, FolderIsWithin("chart", "charts"))
	assert.False(t, FolderIsWithin("", "charts"))
}
