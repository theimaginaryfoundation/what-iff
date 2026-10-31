package skills

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		if strings.HasSuffix(name, "/") {
			_, err := zw.Create(name)
			require.NoError(t, err)
			continue
		}
		f, err := zw.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(data))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func readZip(t *testing.T, data []byte) *Bundle {
	t.Helper()
	b, err := ReadBundle(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	return b
}

func TestReadBundle_RootAndEnclosingFolder(t *testing.T) {
	atRoot := readZip(t, zipOf(t, map[string]string{
		"SKILL.md":       "---\nname: x\n---\nbody",
		"scripts/run.py": "print(1)",
		"__MACOSX/._x":   "junk",
		".DS_Store":      "junk",
	}))
	require.Equal(t, "", atRoot.Root)
	require.Equal(t, "---\nname: x\n---\nbody", string(atRoot.SkillMD))
	require.Equal(t, []File{{Path: "scripts/run.py", Data: []byte("print(1)")}}, atRoot.Files)

	enclosed := readZip(t, zipOf(t, map[string]string{
		"pdf-forms/":                    "",
		"pdf-forms/SKILL.md":            "front",
		"pdf-forms/references/FORMS.md": "ref",
		"README.txt":                    "outside the skill folder, skipped",
	}))
	require.Equal(t, "pdf-forms", enclosed.Root)
	require.Equal(t, "front", string(enclosed.SkillMD))
	require.Equal(t, []File{{Path: "references/FORMS.md", Data: []byte("ref")}}, enclosed.Files)
}

func TestReadBundle_Errors(t *testing.T) {
	cases := map[string]map[string]string{
		"no SKILL.md": {"readme.md": "x"},
		"two skills":  {"a/SKILL.md": "x", "b/SKILL.md": "y"},
		"traversal":   {"SKILL.md": "x", "../etc/passwd": "y"},
		"absolute":    {"SKILL.md": "x", "/etc/passwd": "y"},
		"nested only": {"a/b/SKILL.md": "x"},
	}
	for name, entries := range cases {
		data := zipOf(t, entries)
		_, err := ReadBundle(bytes.NewReader(data), int64(len(data)))
		require.ErrorIs(t, err, ErrInvalid, name)
	}
	_, err := ReadBundle(bytes.NewReader([]byte("not a zip")), 9)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestReadBundle_TooManyFiles(t *testing.T) {
	entries := map[string]string{"SKILL.md": "x"}
	for i := 0; i <= MaxBundleFiles; i++ {
		entries["assets/"+strings.Repeat("f", 3)+string(rune('a'+i%26))+strings.Repeat("x", i/26+1)] = "y"
	}
	data := zipOf(t, entries)
	_, err := ReadBundle(bytes.NewReader(data), int64(len(data)))
	require.ErrorIs(t, err, ErrInvalid)
}

func TestBundleFromFilesAndWriteBundle(t *testing.T) {
	b, err := BundleFromFiles([]File{
		{Path: "my-skill\\SKILL.md", Data: []byte("front")},
		{Path: "my-skill/scripts/go.sh", Data: []byte("echo")},
	})
	require.NoError(t, err)
	require.Equal(t, "my-skill", b.Root)
	require.Equal(t, "front", string(b.SkillMD))
	require.Equal(t, "scripts/go.sh", b.Files[0].Path)

	var out bytes.Buffer
	require.NoError(t, WriteBundle(&out, "my-skill", b.SkillMD, b.Files))
	again := readZip(t, out.Bytes())
	require.Equal(t, "my-skill", again.Root)
	require.Equal(t, "front", string(again.SkillMD))
	require.Equal(t, b.Files, again.Files)
}
