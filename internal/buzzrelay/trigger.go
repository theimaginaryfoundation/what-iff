package buzzrelay

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
)

// Mentions reports whether ev p-tags pubkey (Buzz's mention: the composer adds a p tag for
// every @member it resolves).
func Mentions(ev nostr.Event, pubkey string) bool {
	return ev.Tags.Has("p", pubkey)
}

// FromAgent reports whether ev was published by an agent: it carries a NIP-OA owner
// attestation, as Buzz's own agents' events do.
func FromAgent(ev nostr.Event) bool {
	tag := ev.Tags.Find("auth")
	return len(tag) == 4
}

// MayConcern is the cheap check on a delivered event, before anything is looked up: a
// channel message by someone other than the persona that mentions it or replies in a thread
// (which may continue a conversation with it). Everything else is dropped on arrival.
func MayConcern(ev nostr.Event, personaPub string) bool {
	if ev.Kind != KindStreamMessage || ev.PubKey == personaPub {
		return false
	}
	return Mentions(ev, personaPub) || ThreadOf(ev).Root != ""
}

// IsChannelMessage reports whether ev is a message in channel (kind 9 with that h tag).
func IsChannelMessage(ev nostr.Event, channelID string) bool {
	return ev.Kind == KindStreamMessage && ev.Tags.Value("h") == channelID
}

// maxQuoteRunes bounds how much of a replied-to message is quoted to the model.
const maxQuoteRunes = 500

// PromptText is what the relay thread records for an inbound message, and so what the model
// reads:
//
//	alice (Buzz, #general): what do you think?
//
// The persona's own mention (@name, or a nostr:npub reference) is removed: the model knows it
// was addressed. quoteAuthor and quote, when set, are the message this one replies to (when it
// is not the persona's own, which is already the previous reply in the thread).
func PromptText(content, author, channelName, personaName, personaNPub, quoteAuthor, quote string) string {
	content = stripPersonaMention(content, personaName, personaNPub)
	if content == "" {
		content = "(no text)"
	}
	where := "Buzz"
	if channelName != "" {
		where += ", #" + strings.TrimPrefix(channelName, "#")
	}
	var b strings.Builder
	if quoted := strings.TrimSpace(quote); quoted != "" {
		if r := []rune(quoted); len(r) > maxQuoteRunes {
			quoted = string(r[:maxQuoteRunes]) + "…"
		}
		fmt.Fprintf(&b, "(replying to %s: %q)\n", relaykit.SpeakerLabel(quoteAuthor), quoted)
	}
	fmt.Fprintf(&b, "%s (%s): %s", relaykit.SpeakerLabel(author), where, content)
	return b.String()
}

// stripPersonaMention removes "@<persona name>" (at a word start, any case) and nostr:npub
// references to the persona, with the spaces after them. Everything else is left as written:
// line breaks and runs of spaces matter in code blocks, lists and tables, and other people's
// mentions are the sender's.
func stripPersonaMention(content, name, npub string) string {
	if npub != "" {
		content = regexp.MustCompile(`nostr:`+regexp.QuoteMeta(npub)+`[ \t]*`).ReplaceAllString(content, "")
	}
	if name = strings.TrimSpace(name); name != "" {
		pattern := regexp.MustCompile(`(?i)(^|\s)@` + regexp.QuoteMeta(name) + `\b[ \t]*`)
		content = pattern.ReplaceAllString(content, "$1")
	}
	return strings.TrimSpace(content)
}

// ShortNPub is an npub cut for display ("npub1abcd…wxyz"), the name used for an author who
// has no profile.
func ShortNPub(pubkeyHex string) string {
	n, err := nostr.EncodeNPub(pubkeyHex)
	if err != nil || len(n) < 16 {
		return "someone"
	}
	return n[:10] + "…" + n[len(n)-4:]
}
