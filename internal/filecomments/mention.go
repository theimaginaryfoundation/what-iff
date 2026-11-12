package filecomments

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The context a comment that mentions the agent sends with it (ADR 0x046): the file, what the
// thread is on and the thread before the comment, which is itself the message. The client sends
// what its comments panel shows, for every format alike, so the agent sees what the user saw.

const (
	maxMentionQuote    = 500
	maxMentionComment  = 2000
	maxMentionComments = 30
)

// MentionComment is one comment of the thread before the one that was sent.
type MentionComment struct {
	AuthorName string `json:"author_name"`
	Text       string `json:"text"`
}

// MentionContext renders the context of a comment sent to the agent from fileName's thread.
// Earlier comments beyond the last maxMentionComments are left out, with a note.
func MentionContext(fileName, fileID, threadID, quote string, earlier []MentionComment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This message is a comment on the file %q (file id %s, thread id %s).\n", fileName, fileID, threadID)
	if q := clip(oneLine(quote), maxMentionQuote); q != "" {
		fmt.Fprintf(&b, "The thread is on: %q\n", q)
	} else {
		b.WriteString("The thread is on the whole file.\n")
	}
	if len(earlier) > maxMentionComments {
		fmt.Fprintf(&b, "(%d earlier comments are not shown.)\n", len(earlier)-maxMentionComments)
		earlier = earlier[len(earlier)-maxMentionComments:]
	}
	if len(earlier) > 0 {
		b.WriteString("The thread before this comment:\n")
		for _, c := range earlier {
			fmt.Fprintf(&b, "- %s: %s\n", clip(oneLine(c.AuthorName), 100), clip(oneLine(c.Text), maxMentionComment))
		}
	}
	return strings.TrimSpace(b.String())
}

// ShortQuote is what a thread is on, as a link to it shows it: on one line, and short.
func ShortQuote(quote string) string {
	return clip(oneLine(quote), 160)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
