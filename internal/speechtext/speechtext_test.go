package speechtext

import (
	"strings"
	"testing"
	"unicode/utf8"

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

func TestNextSpeakable(t *testing.T) {
	t.Parallel()
	piece, n := NextSpeakable("It will rain on Tuesday. Take a co", true, 600)
	require.Equal(t, "It will rain on Tuesday.", piece)
	require.Equal(t, "Take a co", "It will rain on Tuesday. Take a co"[n:])

	piece, _ = NextSpeakable("Take a coat", false, 600)
	require.Empty(t, piece, "no sentence is complete yet")
	piece, _ = NextSpeakable("Ends here.", false, 600)
	require.Empty(t, piece, "complete only once space follows")

	text := "One. Two.\n\nThree. Four"
	piece, n = NextSpeakable(text, false, 600)
	require.Equal(t, "One. Two.", piece, "to the end of a paragraph")
	require.Equal(t, "Three. Four", text[n:])

	long := strings.Repeat("A short sentence here. ", 30)
	piece, _ = NextSpeakable(long, false, 300)
	require.LessOrEqual(t, len([]rune(piece)), 300)
	require.GreaterOrEqual(t, len([]rune(piece)), 100, "about a third of the limit within a long paragraph")

	piece, _ = NextSpeakable("Ask Dr. Lee tomorrow", true, 600)
	require.Empty(t, piece, "an abbreviation doesn't end a sentence")
}

// Segments are runs of the prepared text, in order, so a track can be resumed from the
// text it has read rather than a segment count.
func TestSegment_PiecesAreRunsOfTheText(t *testing.T) {
	for name, text := range map[string]string{
		"english":   strings.Repeat("This is a sentence that goes on. And another one here! ", 60),
		"cjk":       strings.Repeat("これは長い文章です。", 300),
		"unspaced":  strings.Repeat("あ", 2500),
		"paragraph": "First. Second one.\n\n" + strings.Repeat("Word ", 400),
	} {
		prepared := Prepare(text)
		segs := Segment(prepared, 200)
		require.NotEmpty(t, segs, name)
		for _, s := range segs {
			require.LessOrEqual(t, utf8.RuneCountInString(s), 200, name)
		}
		rest, ok := Remaining(prepared, segs)
		require.True(t, ok, name)
		require.Empty(t, rest, name)
		rest, ok = Remaining(prepared, segs[:1])
		require.True(t, ok, name)
		require.NotEmpty(t, rest, name)
	}
}

func TestSegment_CJKSentencesEndAtFullWidthStops(t *testing.T) {
	segs := Segment(Prepare(strings.Repeat("これは長い文章です。", 30)), 1000)
	require.Equal(t, "これは長い文章です。", segs[0], "the first sentence alone, so speech starts quickly")
}

func TestRemaining_RefusesWhatTheTextDoesntSay(t *testing.T) {
	_, ok := Remaining("Hello there. How are you?", []string{"Hello there.", "Goodbye."})
	require.False(t, ok)
	rest, ok := Remaining("Hello there. How are you?", []string{"Hello there."})
	require.True(t, ok)
	require.Equal(t, "How are you?", rest)
}

func TestSpoken_AnEmptyFinalRoundReadsTheReply(t *testing.T) {
	msg := "Here's your image."
	require.Equal(t, "Here's your image.", Spoken(msg, len(msg)), "a turn that ended on a tool")
	require.Equal(t, "Done.", Spoken("Let me look. Done.", len("Let me look. ")))
}

func TestPrepare_KeepsProseThatLooksLikeMarkup(t *testing.T) {
	require.Equal(t, "if a<b then c>d ok", Prepare("if a<b then c>d ok"))
	require.Equal(t, "Bold and gone.", Prepare(`<b>Bold</b> and <span class="x">gone</span>.`))
	require.Equal(t, "See. Done.", Prepare("See www.example.com/x. Done."), "the sentence keeps its stop")
}

// Streaming text is read only as far as its words are settled: what Prepare reads now
// is what it reads once the rest arrives.
func TestSettled_StopsBeforeOpenMarkup(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"**Note. Then more":          "",
		"I *think. So":               "I ",
		"Here [see this. And":        "Here ",
		"A [link](http://x":          "A ",
		"Use `go te":                 "Use ",
		"Done **well**. And _so_ on": "Done **well**. And _so_ on",
		"snake_case words. More":     "snake_case words. More",
		"Line one.\n**bold":          "Line one.\n",
		"Text\n```go":                "Text\n",
		"a < b, and <b":              "a < b, and ",
		"2 * 3 = 6. Next":            "2 * 3 = 6. Next",
	} {
		require.Equal(t, want, Settled(in), in)
	}
	// Whatever is settled reads as a prefix of the whole, however it ends.
	full := "**Note.** Then *more* [here](http://x). Done."
	for i := range len(full) {
		require.True(t, strings.HasPrefix(Prepare(full), Prepare(Settled(full[:i]))), full[:i])
	}
}

func TestResync(t *testing.T) {
	t.Parallel()
	pos, ok := Resync("Note. Then more.", "**Note.")
	require.True(t, ok)
	require.Equal(t, "Note.", "Note. Then more."[:pos])
	_, ok = Resync("Something else.", "Note.")
	require.False(t, ok)
}
