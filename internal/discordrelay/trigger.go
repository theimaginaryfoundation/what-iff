package discordrelay

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// InboundMessage is the part of a Discord message the relay needs. The gateway
// fills it from a MESSAGE_CREATE event; keeping it a plain struct keeps the
// trigger rules testable without Discord.
type InboundMessage struct {
	ID        string
	GuildID   string
	ChannelID string
	// ParentChannelID is set when the message is in a Discord thread: the
	// channel the thread belongs to. Bindings are made on channels, so a thread
	// inherits its parent's binding.
	ParentChannelID string
	AuthorID        string
	AuthorName      string
	AuthorIsBot     bool
	// WebhookID is set for messages posted by a webhook (never answered).
	WebhookID      string
	Content        string
	MentionUserIDs []string
	// Attachments are the files posted with the message.
	Attachments []Attachment
	// Referenced* describe the message this one replies to, if any.
	ReferencedMessageID  string
	ReferencedAuthorID   string
	ReferencedAuthorName string
	ReferencedContent    string
}

// TriggerReason says why a message should be answered.
type TriggerReason string

const (
	// TriggerMention: the message @mentions the bot.
	TriggerMention TriggerReason = "mention"
	// TriggerReply: the message is a Discord reply to one of the bot's messages.
	TriggerReply TriggerReason = "reply"
)

// Triggered reports whether m tags the bot (by @mention, or by replying to one of
// its messages). Messages from bots and webhooks, including the bot's own, never
// trigger, so a persona cannot answer itself or another bot in a loop.
func Triggered(m InboundMessage, botUserID string) (TriggerReason, bool) {
	if botUserID == "" || m.AuthorIsBot || m.WebhookID != "" || m.AuthorID == botUserID {
		return "", false
	}
	if slices.Contains(m.MentionUserIDs, botUserID) {
		return TriggerMention, true
	}
	if m.ReferencedMessageID != "" && m.ReferencedAuthorID == botUserID {
		return TriggerReply, true
	}
	return "", false
}

// Allowed applies a binding's allow and deny lists to a Discord user id. Deny
// always wins; an empty allow list means anyone who is not denied.
func Allowed(authorID string, allow, deny []string) bool {
	if slices.Contains(deny, authorID) {
		return false
	}
	return len(allow) == 0 || slices.Contains(allow, authorID)
}

// BindingChannelID is the channel a message is bound through: the thread's
// parent when the message is in a Discord thread, otherwise its own channel.
func (m InboundMessage) BindingChannelID() string {
	if m.ParentChannelID != "" {
		return m.ParentChannelID
	}
	return m.ChannelID
}

// maxQuoteRunes bounds how much of a replied-to message is quoted to the model.
const maxQuoteRunes = 500

// PromptText is what the relay thread records for an inbound message, and so what
// the model reads:
//
//	alice (Discord, #general): what do you think?
//
// The bot's own mention is removed (the model knows it was addressed). When the
// message replies to someone other than the bot, the replied-to message is quoted
// first, since it is not in the relay thread; a reply to the bot needs no quote,
// because the bot's message is already the previous reply in the thread.
func PromptText(m InboundMessage, botUserID, channelName string) string {
	content := stripMention(m.Content, botUserID)
	if content == "" {
		content = "(no text)"
	}
	where := "Discord"
	if channelName != "" {
		where += ", #" + strings.TrimPrefix(channelName, "#")
	}
	author := speakerLabel(m.AuthorName)
	var b strings.Builder
	if m.ReferencedMessageID != "" && m.ReferencedAuthorID != botUserID && strings.TrimSpace(m.ReferencedContent) != "" {
		quoted := strings.TrimSpace(m.ReferencedContent)
		if r := []rune(quoted); len(r) > maxQuoteRunes {
			quoted = string(r[:maxQuoteRunes]) + "…"
		}
		refAuthor := speakerLabel(m.ReferencedAuthorName)
		fmt.Fprintf(&b, "(replying to %s: %q)\n", refAuthor, quoted)
	}
	fmt.Fprintf(&b, "%s (%s): %s", author, where, content)
	return b.String()
}

// speakerLabel is a display name as it may appear at the start of a relayed line.
// Names are chosen by whoever posts, so the characters that make up the
// "name (Discord, #channel):" label are removed: a nickname cannot pose as another
// speaker's label or end its own early. An empty result reads "someone".
func speakerLabel(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '(', ')', ':', '[', ']', '"':
			return ' '
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 64 {
		name = string(r[:64])
	}
	if name == "" {
		return "someone"
	}
	return name
}

// stripMention removes <@id> and <@!id> mentions of the bot and tidies spaces.
func stripMention(content, botUserID string) string {
	if botUserID != "" {
		content = strings.ReplaceAll(content, "<@"+botUserID+">", "")
		content = strings.ReplaceAll(content, "<@!"+botUserID+">", "")
	}
	return strings.Join(strings.Fields(content), " ")
}
