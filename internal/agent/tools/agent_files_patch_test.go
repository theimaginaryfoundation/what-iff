package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const patchBase = `# Journal

## Monday
Walked the dog.
Rained.

## Tuesday
Worked on Project Y.
`

func TestApplyUnifiedDiff(t *testing.T) {
	cases := []struct {
		name    string
		content string
		diff    string
		want    string
		wantErr string
	}{
		{
			name:    "replace a line with file headers and exact numbers",
			content: patchBase,
			diff: `--- a/journal.md
+++ b/journal.md
@@ -4,2 +4,2 @@
 Walked the dog.
-Rained.
+Sunny.
`,
			want: "# Journal\n\n## Monday\nWalked the dog.\nSunny.\n\n## Tuesday\nWorked on Project Y.\n",
		},
		{
			name:    "wrong line numbers still apply by context",
			content: patchBase,
			diff: `@@ -40,3 +40,4 @@
 ## Tuesday
 Worked on Project Y.
+Called Chris.
`,
			want: patchBase + "Called Chris.\n",
		},
		{
			name:    "header without numbers",
			content: patchBase,
			diff:    "@@ @@\n-# Journal\n+# Diary\n",
			want:    "# Diary\n" + patchBase[len("# Journal\n"):],
		},
		{
			name:    "blank lines between hunks are separators, not context",
			content: "a\nb\nc\nd\ne\nf\n",
			diff:    "@@ -1,2 +1,2 @@\n a\n-b\n+B\n\n\n@@ -5,2 +5,2 @@\n e\n-f\n+F\n",
			want:    "a\nB\nc\nd\ne\nF\n",
		},
		{
			name:    "two hunks keep their offsets",
			content: "a\nb\nc\nd\ne\nf\n",
			diff:    "@@ -1,2 +1,3 @@\n a\n+a2\n b\n@@ -5,2 +6,1 @@\n e\n-f\n",
			want:    "a\na2\nb\nc\nd\ne\n",
		},
		{
			name:    "blank context line without its leading space",
			content: patchBase,
			diff:    "@@ -5,3 +5,3 @@\n Rained.\n\n-## Tuesday\n+## Wednesday\n",
			want:    "# Journal\n\n## Monday\nWalked the dog.\nRained.\n\n## Wednesday\nWorked on Project Y.\n",
		},
		{
			name:    "trailing whitespace tolerated",
			content: "x   \ny\n",
			diff:    "@@ -1,2 +1,2 @@\n x\n-y\n+z\n",
			want:    "x   \nz\n",
		},
		{
			name:    "pure insertion by line number",
			content: "a\nb\n",
			diff:    "@@ -1,0 +2,1 @@\n+between\n",
			want:    "a\nbetween\nb\n",
		},
		{
			name:    "insert into an empty file",
			content: "",
			diff:    "@@ -0,0 +1,2 @@\n+first\n+second\n",
			want:    "first\nsecond\n",
		},
		{
			name:    "nearest match to the header wins",
			content: "x\ny\nx\ny\n",
			diff:    "@@ -3,2 +3,2 @@\n x\n-y\n+z\n",
			want:    "x\ny\nx\nz\n",
		},
		{
			name:    "ambiguous without a number",
			content: "x\ny\nx\ny\n",
			diff:    "@@ @@\n x\n-y\n+z\n",
			wantErr: "more than one place",
		},
		{
			name:    "no match is refused",
			content: patchBase,
			diff:    "@@ -1,1 +1,1 @@\n-# Nope\n+# Yes\n",
			wantErr: "does not match",
		},
		{
			name:    "a failing second hunk leaves nothing applied",
			content: "a\nb\n",
			diff:    "@@ -1 +1 @@\n-a\n+A\n@@ -2 +2 @@\n-zzz\n+Z\n",
			wantErr: "hunk 2",
		},
		{
			name:    "two files refused",
			content: "a\n",
			diff:    "@@ -1 +1 @@\n-a\n+b\n--- a/other\n+++ b/other\n",
			wantErr: "only one file",
		},
		{
			name:    "no hunks",
			content: "a\n",
			diff:    "-a\n+b\n",
			wantErr: "no hunks",
		},
		{
			name:    "insertion without number or context",
			content: "a\n",
			diff:    "@@ @@\n+b\n",
			wantErr: "context",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyUnifiedDiff(tc.content, tc.diff)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
