package textanchor

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// anchored is the text a range of the old text is on once the text is new.
func anchored(t *testing.T, oldText, quote, newText string) string {
	t.Helper()
	start := strings.Index(oldText, quote)
	if start < 0 {
		t.Fatalf("%q is not in %q", quote, oldText)
	}
	r := Range{Start: len(Units(oldText[:start])), End: len(Units(oldText[:start+len(quote)]))}
	got := Remap(oldText, newText, []Range{r})[0]
	if got == nil {
		return "<gone>"
	}
	return Slice(Units(newText), *got)
}

func TestRemapKeepsARangeOnItsWords(t *testing.T) {
	const line = "This is an inline comment in a text file"
	cases := []struct {
		name, oldText, quote, newText, want string
	}{
		{"an edit inside stays inside", line, line, "This is a modified inline comment in a text file", "This is a modified inline comment in a text file"},
		{"a changed start moves to the first word left", line, line, "I've updated this inline comment in a text file", "inline comment in a text file"},
		{"a changed end moves to the last word left", line, line, "This is an inline comment in Go source code", "This is an inline comment in"},
		{"text around it moves it", "a\nThis is an inline comment\nb", "inline comment", "header\n\na\nThis is an inline comment\nb", "inline comment"},
		{"inside a word: the part of the word is kept", "func computeTotal(x int)", "Total", "// sum\nfunc computeTotal(x int, y int)", "Total"},
		{"all its words gone", line, "inline comment", "This is a remark in a text file", "<gone>"},
		{"unchanged", line, "comment in a", line, "comment in a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := anchored(t, tc.oldText, tc.quote, tc.newText); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRemapCountsUTF16UnitsAndIgnoresAByteOrderMark(t *testing.T) {
	oldText := "\ufeff😀 naïve café — résumé"
	units := Units(oldText)
	start := len(Units("😀 naïve "))
	r := Range{Start: start, End: start + len(Units("café"))}
	if Slice(units, r) != "café" {
		t.Fatalf("slice %q", Slice(units, r))
	}
	got := Remap(oldText, "😀😀 a naïve café — résumé", []Range{r})[0]
	if got == nil || Slice(Units("😀😀 a naïve café — résumé"), *got) != "café" {
		t.Fatalf("got %+v", got)
	}
}

func TestRemapPastTheDiffLimitFindsTheWordsNearestWhereTheyWere(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < MaxEdits+10; i++ {
		oldB.WriteString("alpha ")
		newB.WriteString("beta ")
	}
	oldText := oldB.String() + "keep this phrase " + oldB.String()
	newText := newB.String() + "keep this phrase " + newB.String()
	if got := anchored(t, oldText, "keep this phrase", newText); got != "keep this phrase" {
		t.Fatalf("got %q", got)
	}
}

func TestRemapCodeEdits(t *testing.T) {
	oldText := "func main() {\n\tfmt.Println(\"hello\")\n\treturn\n}\n"
	newText := "package main\n\nfunc main() {\n\tlog.Println(\"hello, world\")\n\treturn\n}\n"
	if got := anchored(t, oldText, "fmt.Println(\"hello\")", newText); got != ".Println(\"hello, world\")" {
		t.Fatalf("got %q", got)
	}
}

func TestRemapOfALargeRepetitiveFileIsBounded(t *testing.T) {
	// 4 MB of words, a few of each repeated, edited all through within MaxEdits: the diff's worst case.
	words := make([]string, 800_000)
	for i := range words {
		words[i] = "w" + strconv.Itoa(i%7)
	}
	oldText := strings.Join(words, " ")
	for i := 0; i < len(words); i += 1100 {
		words[i] = "edit"
	}
	newText := strings.Join(words, " ")
	start := time.Now()
	got := Remap(oldText, newText, []Range{{Start: 10, End: 14}})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v", elapsed)
	}
	if got[0] == nil {
		t.Fatal("the range was lost")
	}
}
