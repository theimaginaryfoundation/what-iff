package skills

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"PDF Forms":                    "pdf-forms",
		"  Source   Analysis!!  ":      "source-analysis",
		"café menu":                    "caf-menu",
		"---":                          "skill",
		"":                             "skill",
		"already-a-slug":               "already-a-slug",
		"Weekly_Report (v2)":           "weekly-report-v2",
		strings.Repeat("a", 70):        strings.Repeat("a", MaxSlugLen),
		strings.Repeat("a", 63) + "-b": strings.Repeat("a", 63),
	}
	for in, want := range cases {
		got := Slugify(in)
		require.Equal(t, want, got, "Slugify(%q)", in)
		require.NoError(t, ValidateSlug(got), "Slugify(%q) must validate", in)
	}
}

func TestValidateSlug(t *testing.T) {
	for _, ok := range []string{"a", "pdf-forms", "x1-2-3", strings.Repeat("z", MaxSlugLen)} {
		require.NoError(t, ValidateSlug(ok), ok)
	}
	for _, bad := range []string{"", "-lead", "trail-", "dou--ble", "Caps", "under_score", "spa ce", "ünïcode", strings.Repeat("z", MaxSlugLen+1)} {
		err := ValidateSlug(bad)
		require.Error(t, err, bad)
		require.True(t, errors.Is(err, ErrInvalid), bad)
	}
}

func TestUniqueSlug(t *testing.T) {
	taken := map[string]bool{"pdf-forms": true, "pdf-forms-2": true}
	isTaken := func(s string) (bool, error) { return taken[s], nil }

	got, err := UniqueSlug("pdf-forms", isTaken)
	require.NoError(t, err)
	require.Equal(t, "pdf-forms-3", got)

	got, err = UniqueSlug("fresh", isTaken)
	require.NoError(t, err)
	require.Equal(t, "fresh", got)

	long := strings.Repeat("a", MaxSlugLen)
	taken[long] = true
	got, err = UniqueSlug(long, isTaken)
	require.NoError(t, err)
	require.Len(t, got, MaxSlugLen)
	require.True(t, strings.HasSuffix(got, "-2"))
	require.NoError(t, ValidateSlug(got))

	got, err = UniqueSlug("", isTaken)
	require.NoError(t, err)
	require.Equal(t, "skill", got)
}
