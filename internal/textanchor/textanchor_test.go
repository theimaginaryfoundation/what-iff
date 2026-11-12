package textanchor

import (
	"strconv"
	"strings"
	"testing"
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

func TestTheDiffGivesUpAtItsWorkBudget(t *testing.T) {
	saved := maxSteps
	t.Cleanup(func() { maxSteps = saved })

	words := make([]string, 2000)
	for i := range words {
		words[i] = "w" + strconv.Itoa(i%7)
	}
	oldText := strings.Join(words, " ")
	for i := 0; i < len(words); i += 50 {
		words[i] = "edit"
	}
	newText := strings.Join(words, " ")
	vocab := vocabulary{}
	oldTokens, newTokens := tokenize(Units(oldText), vocab), tokenize(Units(newText), vocab)

	if _, ok := match(oldTokens, newTokens); !ok {
		t.Fatal("within the budget the diff finishes")
	}
	maxSteps = 100
	if _, ok := match(oldTokens, newTokens); ok {
		t.Fatal("past the budget the diff gives up")
	}
	// Past the budget a range is still found, by its own words.
	got := Remap(oldText, newText, []Range{{Start: 3, End: 5}})
	if got[0] == nil || Slice(Units(newText), *got[0]) != "w1" {
		t.Fatalf("got %+v", got[0])
	}
}

// BenchmarkRemapLargeFile is a 4 MB file of a few repeated words, edited all through within
// MaxEdits: the diff's worst case. Run with -bench to see it stay well under a second.
func BenchmarkRemapLargeFile(b *testing.B) {
	words := make([]string, 800_000)
	for i := range words {
		words[i] = "w" + strconv.Itoa(i%7)
	}
	oldText := strings.Join(words, " ")
	for i := 0; i < len(words); i += 1100 {
		words[i] = "edit"
	}
	newText := strings.Join(words, " ")
	for b.Loop() {
		if got := Remap(oldText, newText, []Range{{Start: 3, End: 5}}); got[0] == nil {
			b.Fatal("the range was lost")
		}
	}
}

func TestFindIsBoundedToo(t *testing.T) {
	// The work is the units compared: one per place for a quote whose first unit differs.
	saved := maxSteps
	t.Cleanup(func() { maxSteps = saved })
	units := Units(strings.Repeat("ab", 5000) + "target")
	if got := find("target", units, 0); got == nil || got.Start != 10000 {
		t.Fatalf("within the budget: %+v", got)
	}
	maxSteps = 5000
	if got := find("target", units, 0); got != nil {
		t.Fatalf("past the budget the search stops: %+v", got)
	}
}
