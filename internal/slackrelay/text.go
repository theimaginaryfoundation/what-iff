package slackrelay

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
)

// MaxMessageRunes is how long one posted message may be. Slack accepts up to 40,000 characters
// but folds long messages behind "Show more" and advises keeping text under 4,000.
const MaxMessageRunes = 4000

// controlSeq is one of Slack's <…> sequences: a mention, a channel, a special mention, a link.
var controlSeq = regexp.MustCompile(`<([^<>]*)>`)

// PlainText turns a Slack message's text into plain text for the model: the persona's own
// mention is removed, other mentions become @name (names(userID); "" keeps the id), channels
// become #name, special mentions become @here and the like, links become "label (url)", and
// Slack's &amp;, &lt;, &gt; are unescaped. Line breaks are kept.
func PlainText(text, botUserID string, names func(userID string) string) string {
	text = controlSeq.ReplaceAllStringFunc(text, func(seq string) string {
		inner := seq[1 : len(seq)-1]
		target, label, hasLabel := strings.Cut(inner, "|")
		switch {
		case strings.HasPrefix(target, "@"):
			id := target[1:]
			if id == botUserID {
				return ""
			}
			if name := names(id); name != "" {
				return "@" + name
			}
			if hasLabel && label != "" {
				return "@" + strings.TrimPrefix(label, "@")
			}
			return "@" + id
		case strings.HasPrefix(target, "#"):
			if hasLabel && label != "" {
				return "#" + label
			}
			return "#channel"
		case strings.HasPrefix(target, "!"):
			if hasLabel && label != "" {
				return label // <!subteam^S1|@team>, <!date^…|fallback>
			}
			special, _, _ := strings.Cut(target[1:], "^")
			return "@" + special // <!here>, <!channel>, <!everyone>
		default:
			if hasLabel && label != "" && label != target && "mailto:"+label != target {
				return label + " (" + target + ")"
			}
			return strings.TrimPrefix(target, "mailto:")
		}
	})
	text = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(text)
	// Line ends are trimmed, nothing else: runs of spaces and indentation matter in code blocks
	// and nested lists.
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// maxQuoteRunes bounds how much of a thread's first message is quoted to the model.
const maxQuoteRunes = 500

// PromptText is what the relay thread records for an inbound message, and so what the model
// reads:
//
//	alice (Slack, #general): what do you think?
//
// text is already plain (PlainText). quoteAuthor and quote, when set, are the first message of
// the thread it was posted in, when that is someone else's (the persona's own is already in
// the relay thread).
func PromptText(text, author, channelName, quoteAuthor, quote string) string {
	if text == "" {
		text = "(no text)"
	}
	where := "Slack"
	if channelName != "" {
		where += ", #" + strings.TrimPrefix(channelName, "#")
	}
	var b strings.Builder
	if quoted := strings.TrimSpace(quote); quoted != "" {
		if r := []rune(quoted); len(r) > maxQuoteRunes {
			quoted = string(r[:maxQuoteRunes]) + "…"
		}
		fmt.Fprintf(&b, "(in a thread started by %s: %q)\n", relaykit.SpeakerLabel(quoteAuthor), quoted)
	}
	fmt.Fprintf(&b, "%s (%s): %s", relaykit.SpeakerLabel(author), where, text)
	return b.String()
}

// Markdown → mrkdwn.
var (
	mdHeading = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$`)
	mdBullet  = regexp.MustCompile(`^(\s*)[-*+]\s+`)
	mdQuote   = regexp.MustCompile(`^(\s*)&gt;\s?`)
	mdImage   = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	mdLink    = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	mdBold    = regexp.MustCompile(`\*\*([^*\n]+?)\*\*|__([^_\n]+?)__`)
	mdItalic  = regexp.MustCompile(`(^|[^*\w])\*([^*\s][^*\n]*?)\*([^*\w]|$)`)
	mdStrike  = regexp.MustCompile(`~~([^~\n]+?)~~`)
	// linkSafeURL is a security boundary: only http(s) and mailto targets become clickable
	// links, so a reply can never produce javascript: or Slack's own <!…>/<@…> forms.
	linkSafeURL = regexp.MustCompile(`^(https?://|mailto:)[^\s<>|]+$`)
)

// boldMark stands in for mrkdwn's * while italics are converted, so **bold** (now *bold*) is
// not read as italics.
const boldMark = "\x00"

// ToMrkdwn converts a reply's Markdown into Slack mrkdwn. &, < and > are escaped everywhere
// (a blockquote's leading > is kept), so model output can never form <!channel>, <!here>,
// <@U…> or a link Slack would parse; the only <…> sequences in the result are the links this
// function builds from Markdown links with http(s) or mailto URLs. Tables become code blocks;
// headings become bold lines; lists get bullets.
func ToMrkdwn(md string) string {
	md = relaykit.TablesToCodeBlocks(strings.ReplaceAll(md, "\r\n", "\n"))
	lines := strings.Split(md, "\n")
	inCode := false
	for i, line := range lines {
		if relaykit.IsFenceLine(line) {
			inCode = !inCode
			lines[i] = escapeMrkdwn(strings.TrimSpace(line)[:3]) // ``` (Slack ignores the language)
			continue
		}
		if inCode {
			lines[i] = escapeMrkdwn(line)
			continue
		}
		lines[i] = convertLine(line)
	}
	return strings.Join(lines, "\n")
}

func escapeMrkdwn(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// convertLine converts one line outside code blocks, leaving inline code spans as written.
func convertLine(line string) string {
	line = escapeMrkdwn(line)
	if m := mdHeading.FindStringSubmatch(line); m != nil {
		return "*" + strings.Trim(convertInline(m[1]), "*") + "*"
	}
	quote := ""
	if m := mdQuote.FindStringSubmatch(line); m != nil {
		quote, line = m[1]+"> ", line[len(m[0]):]
	}
	if m := mdBullet.FindStringSubmatch(line); m != nil {
		line = m[1] + "• " + line[len(m[0]):]
	}
	return quote + convertInline(line)
}

// convertInline converts emphasis and links outside `code` spans.
func convertInline(s string) string {
	parts := strings.Split(s, "`")
	for i := 0; i < len(parts); i += 2 {
		if i == len(parts)-1 && len(parts)%2 == 0 {
			break // an unclosed backtick: leave the rest as written
		}
		parts[i] = convertSpan(parts[i])
	}
	return strings.Join(parts, "`")
}

// convertSpan converts links, then emphasis, in text outside code spans. The links it builds are
// set aside while emphasis is converted, so a * or _ inside a URL is not turned into
// formatting (which would change where the link goes).
func convertSpan(s string) string {
	s = strings.ReplaceAll(s, linkMark, "")
	var links []string
	link := func(label, target string) string {
		// Escaped already: &amp; in a URL is how Slack expects it.
		raw := strings.ReplaceAll(target, "&amp;", "&")
		if !linkSafeURL.MatchString(raw) {
			return label + " (" + target + ")"
		}
		label = strings.NewReplacer("|", "¦", "&gt;", "›").Replace(emphasis(label))
		links = append(links, "<"+target+"|"+label+">")
		return linkMark + strconv.Itoa(len(links)-1) + linkMark
	}
	s = mdImage.ReplaceAllStringFunc(s, func(m string) string {
		g := mdImage.FindStringSubmatch(m)
		alt := g[1]
		if alt == "" {
			alt = "image"
		}
		return link(alt, g[2])
	})
	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		g := mdLink.FindStringSubmatch(m)
		return link(g[1], g[2])
	})
	s = emphasis(s)
	return linkSlot.ReplaceAllStringFunc(s, func(m string) string {
		n, err := strconv.Atoi(strings.Trim(m, linkMark))
		if err != nil || n >= len(links) {
			return ""
		}
		return links[n]
	})
}

// linkMark brackets the slot a built link holds while emphasis is converted (linkSlot).
const linkMark = "\x01"

var linkSlot = regexp.MustCompile(linkMark + `[0-9]+` + linkMark)

// emphasis converts Markdown bold, italic and strikethrough to mrkdwn.
func emphasis(s string) string {
	s = mdBold.ReplaceAllStringFunc(s, func(m string) string {
		g := mdBold.FindStringSubmatch(m)
		return boldMark + g[1] + g[2] + boldMark
	})
	for {
		next := mdItalic.ReplaceAllString(s, "${1}_${2}_${3}")
		if next == s {
			break
		}
		s = next
	}
	s = mdStrike.ReplaceAllString(s, "~${1}~")
	return strings.ReplaceAll(s, boldMark, "*")
}
