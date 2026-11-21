package speechtext

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepare(t *testing.T) {
	t.Parallel()
	md := "# Your week\n\n" +
		"Here's **what** I found on [the forecast](https://example.com/w) 🌦️. See https://example.com for more.\n\n" +
		"- Monday: _sunny_\n" +
		"- Tuesday: rain!\n\n" +
		"```go\nfmt.Println(\"skip me\")\n```\n\n" +
		"| Day | Temp |\n|---|---|\n| Mon | 20 |\n\n" +
		"> Pack an `umbrella`, <b>just</b> in case.\n\n" +
		"---\n\n" +
		"![chart](chart.png)Done."
	require.Equal(t,
		"Your week.\n\n"+
			"Here's what I found on the forecast. See for more.\n\n"+
			"Monday: sunny.\n\n"+
			"Tuesday: rain!\n\n"+
			"Pack an umbrella, just in case.\n\n"+
			"Done.",
		Prepare(md))
}

func TestPrepare_KeepsUnderscoresInWordsAndUnclosedFences(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Set snake_case names, and a*b stays.", Prepare("Set snake_case names, and a*b stays."))
	require.Equal(t, "Before.", Prepare("Before.\n\n```\nnever closed"))
}

func TestSpoken_ReadsTheFinalRoundOnly(t *testing.T) {
	t.Parallel()
	msg := "Let me check the forecast.\n\nIt will rain on Tuesday."
	require.Equal(t, "It will rain on Tuesday.", Spoken(msg, strings.Index(msg, "It")))
	require.Equal(t, "Let me check the forecast.\n\nIt will rain on Tuesday.", Spoken(msg, 0))
	require.Equal(t, Spoken(msg, 0), Spoken(msg, 9999), "an offset past the text reads it all")
}

func TestSegment(t *testing.T) {
	t.Parallel()
	text := "Hello there. This is the rest of the first paragraph, which goes on.\n\nSecond paragraph.\n\nThird."
	require.Equal(t, []string{
		"Hello there.",
		"This is the rest of the first paragraph, which goes on.\n\nSecond paragraph.\n\nThird.",
	}, Segment(text, 200), "the first sentence alone, then paragraphs packed")

	got := Segment(text, 60)
	require.Equal(t, "Hello there.", got[0])
	for _, s := range got {
		require.LessOrEqual(t, len([]rune(s)), 60)
	}
	require.Equal(t, strings.Join(strings.Fields(text), " "), strings.Join(strings.Fields(strings.Join(got, " ")), " "), "nothing lost")

	require.Equal(t, []string{"Sure, done!"}, Segment("Sure, done!", 600), "a one-sentence reply is one segment")
	require.Nil(t, Segment("  \n\n ", 600))
}

func TestSegment_LongSentencesSplitBetweenWords(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("word ", 50) + "end."
	got := Segment(long, 40)
	require.Greater(t, len(got), 1)
	for _, s := range got {
		require.LessOrEqual(t, len([]rune(s)), 40)
	}
}

func TestSentences_Abbreviations(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"Ask Dr. Lee, e.g. tomorrow.", "Then rest."}, sentences("Ask Dr. Lee, e.g. tomorrow. Then rest."))
	require.Equal(t, []string{`He said "go!"`, "So we did."}, sentences(`He said "go!" So we did.`))
}
