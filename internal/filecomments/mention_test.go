package filecomments

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestMentionContext_TheFileWhatTheThreadIsOnAndTheThreadSoFar(t *testing.T) {
	got := MentionContext("plan.md", "f1", "t1", "the  major\nfeatures", []MentionComment{
		{AuthorName: "Ada", Text: "Is this\nright?"},
		{AuthorName: "Scribe", Text: "Yes."},
	})
	require.Equal(t, `This message is a comment on the file "plan.md" (file id f1, thread id t1).
The thread is on: "the major features"
The thread before this comment:
- Ada: Is this right?
- Scribe: Yes.`, got)
}

func TestMentionContext_OnTheWholeFileWithNoEarlierComments(t *testing.T) {
	got := MentionContext("photo.png", "f1", "t1", "  \n", nil)
	require.Equal(t, `This message is a comment on the file "photo.png" (file id f1, thread id t1).
The thread is on the whole file.`, got)
}

func TestMentionContext_KeepsTheLastCommentsAndClipsLongOnes(t *testing.T) {
	var earlier []MentionComment
	for i := 1; i <= maxMentionComments+2; i++ {
		earlier = append(earlier, MentionComment{AuthorName: "Ada", Text: fmt.Sprintf("comment %d", i)})
	}
	earlier[len(earlier)-1].Text = strings.Repeat("é", maxMentionComment+10)

	got := MentionContext("plan.md", "f1", "t1", strings.Repeat("q", maxMentionQuote+10), earlier)
	require.Contains(t, got, "(2 earlier comments are not shown.)")
	require.NotContains(t, got, "- Ada: comment 2\n")
	require.Contains(t, got, "- Ada: comment 3\n")
	require.Contains(t, got, `"`+strings.Repeat("q", maxMentionQuote-1)+`…"`)
	last := got[strings.LastIndex(got, "- Ada: ")+len("- Ada: "):]
	require.Equal(t, maxMentionComment, utf8.RuneCountInString(last), "clipped by runes, with the ellipsis")
	require.True(t, strings.HasSuffix(last, "é…"))
}

func TestShortQuote_OneShortLine(t *testing.T) {
	require.Equal(t, "major features", ShortQuote(" major\n\tfeatures "))
	long := ShortQuote(strings.Repeat("a", 200))
	require.Equal(t, 160, utf8.RuneCountInString(long))
	require.True(t, strings.HasSuffix(long, "…"))
}
