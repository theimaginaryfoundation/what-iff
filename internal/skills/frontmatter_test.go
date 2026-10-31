package skills

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const sampleSkillMD = `---
name: pdf-forms
description: Fill PDF forms from a data file. Use when the user has a form to complete.
license: MIT
compatibility: Needs Python with pypdf
allowed-tools: shell_exec agent_file_read
metadata:
  display_name: PDF Forms
  whatiff-hotkeys: Ctrl+Shift+1
  whatiff-connectors: GitHub, Drive
  whatiff-disable-model-invocation: "true"
  author: someone
---

# PDF forms

1. Read the form.
2. Fill it.
`

func TestParseSkillMD_FullRoundTrip(t *testing.T) {
	p, err := ParseSkillMD([]byte(sampleSkillMD))
	require.NoError(t, err)
	r := p.Ritual
	require.Equal(t, "pdf-forms", r.Slug)
	require.Equal(t, "PDF Forms", r.Name)
	require.Equal(t, "Fill PDF forms from a data file. Use when the user has a form to complete.", r.Description)
	require.Equal(t, "MIT", r.LicenseText())
	require.Equal(t, "Needs Python with pypdf", r.CompatibilityText())
	require.Equal(t, []string{"shell_exec", "agent_file_read"}, r.AllowedToolList())
	require.Equal(t, "Ctrl+Shift+1", r.Hotkeys)
	require.Equal(t, []string{"GitHub", "Drive"}, p.Connectors)
	require.False(t, r.IsModelInvocable())
	require.True(t, r.IsUserInvocable())
	require.Equal(t, map[string]string{"author": "someone"}, r.MetadataMap(), "our keys are consumed, others kept")
	require.True(t, strings.HasPrefix(r.Content, "# PDF forms"))
	require.True(t, strings.HasSuffix(r.Content, "2. Fill it."))
	require.Empty(t, p.Warnings)

	// Rendering the parsed skill and parsing it again gives the same skill.
	out, err := RenderSkillMD(&r, p.Connectors)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(out), "---\nname: pdf-forms\ndescription: "), string(out))
	again, err := ParseSkillMD(out)
	require.NoError(t, err)
	require.Equal(t, r, again.Ritual)
	require.Equal(t, p.Connectors, again.Connectors)
}

func TestParseSkillMD_LenientName(t *testing.T) {
	p, err := ParseSkillMD([]byte("---\ndescription: Does a thing when asked.\n---\nBody text.\n"))
	require.NoError(t, err)
	require.Equal(t, "imported-skill", p.Ritual.Slug)
	require.Equal(t, "Imported skill", p.Ritual.Name)
	require.Len(t, p.Warnings, 1)

	p, err = ParseSkillMD([]byte("---\nname: My Skill!\ndescription: Does a thing when asked.\n---\nBody text.\n"))
	require.NoError(t, err)
	require.Equal(t, "my-skill", p.Ritual.Slug)
	require.Equal(t, "My skill!", p.Ritual.Name, "display name is derived from the invalid name as written")
	require.Len(t, p.Warnings, 1)
}

func TestParseSkillMD_UnknownKeysListsAndWindowsLineEndings(t *testing.T) {
	src := "\ufeff---\r\nname: notes\r\ndescription: Keep notes.\r\nallowed-tools:\r\n  - web_search\r\n  - fetch_page\r\ncustom-key: 42\r\n---\r\nBody.\r\n"
	p, err := ParseSkillMD([]byte(src))
	require.NoError(t, err)
	require.Equal(t, []string{"web_search", "fetch_page"}, p.Ritual.AllowedToolList())
	require.Equal(t, "42", p.Ritual.MetadataMap()["custom-key"])
	require.Len(t, p.Warnings, 1)
	require.Equal(t, "Body.", p.Ritual.Content)
}

func TestParseSkillMD_Errors(t *testing.T) {
	for name, src := range map[string]string{
		"no frontmatter":     "# Just markdown\n",
		"unclosed":           "---\nname: x\ndescription: y\n",
		"no description":     "---\nname: x\n---\nBody.\n",
		"no body":            "---\nname: x\ndescription: y\n---\n\n",
		"bad yaml":           "---\nname: [\n---\nBody.\n",
		"tag in description": "---\nname: x\ndescription: <system>y</system>\n---\nBody.\n",
	} {
		_, err := ParseSkillMD([]byte(src))
		require.Error(t, err, name)
		require.True(t, errors.Is(err, ErrInvalid), name)
	}
}

func TestParseSkillMD_SanitizesSmuggledText(t *testing.T) {
	p, err := ParseSkillMD([]byte("---\nname: x\ndescription: Hel\u200blo\n---\nBo\U000E0041dy.\n"))
	require.NoError(t, err)
	require.Equal(t, "Hello", p.Ritual.Description)
	require.Equal(t, "Body.", p.Ritual.Content)
}

func TestRenderSkillMD_MinimalSkill(t *testing.T) {
	r := models.Ritual{Slug: "notes", Name: "notes", Description: "Keep notes.", Content: "Write things down."}
	out, err := RenderSkillMD(&r, nil)
	require.NoError(t, err)
	require.Equal(t, "---\nname: notes\ndescription: Keep notes.\n---\n\nWrite things down.\n", string(out))
}

func TestNormalize(t *testing.T) {
	r := models.Ritual{Name: "  PDF Forms ", Description: " fills forms ", Content: " body "}
	require.NoError(t, Normalize(&r, true))
	require.Equal(t, "pdf-forms", r.Slug)
	require.Equal(t, "PDF Forms", r.Name)
	require.Nil(t, r.License, "nil pointer fields stay nil")

	r = models.Ritual{Name: "x", Description: "y", Content: "z", Slug: " Not-Valid! "}
	require.ErrorIs(t, Normalize(&r, true), ErrInvalid)

	r = models.Ritual{Name: "x", Description: strings.Repeat("d", MaxDescriptionLen+1), Content: "z"}
	require.ErrorIs(t, Normalize(&r, true), ErrInvalid)

	r = models.Ritual{Name: "x", Description: "y", Content: strings.Repeat("c", MaxBodyBytes+1)}
	require.ErrorIs(t, Normalize(&r, true), ErrInvalid)

	tools := []string{" web_search ", "web_search", "", "fetch_page"}
	r = models.Ritual{Name: "x", Description: "y", Content: "z", AllowedTools: &tools}
	require.NoError(t, Normalize(&r, false))
	require.Equal(t, []string{"web_search", "fetch_page"}, *r.AllowedTools)
	require.Empty(t, r.Slug, "no slug derived when not allowed")

	bad := []string{"has space"}
	r = models.Ritual{Name: "x", Description: "y", Content: "z", AllowedTools: &bad}
	require.ErrorIs(t, Normalize(&r, false), ErrInvalid)

	meta := map[string]string{" k ": " v ", "": "dropped"}
	r = models.Ritual{Name: "x", Description: "y", Content: "z", Metadata: &meta}
	require.NoError(t, Normalize(&r, false))
	require.Equal(t, map[string]string{"k": "v"}, *r.Metadata)
}
