package discordrelay

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const botID = "111"

func TestTriggered(t *testing.T) {
	human := InboundMessage{ID: "m", AuthorID: "222", AuthorName: "alice", Content: "hi"}

	cases := map[string]struct {
		msg    InboundMessage
		reason TriggerReason
		ok     bool
	}{
		"plain message":          {human, "", false},
		"mention":                {with(human, func(m *InboundMessage) { m.MentionUserIDs = []string{"999", botID} }), TriggerMention, true},
		"reply to the bot":       {with(human, func(m *InboundMessage) { m.ReferencedMessageID = "r"; m.ReferencedAuthorID = botID }), TriggerReply, true},
		"reply to someone else":  {with(human, func(m *InboundMessage) { m.ReferencedMessageID = "r"; m.ReferencedAuthorID = "333" }), "", false},
		"another bot mentioning": {with(human, func(m *InboundMessage) { m.AuthorIsBot = true; m.MentionUserIDs = []string{botID} }), "", false},
		"webhook mentioning":     {with(human, func(m *InboundMessage) { m.WebhookID = "w"; m.MentionUserIDs = []string{botID} }), "", false},
		"the bot itself":         {with(human, func(m *InboundMessage) { m.AuthorID = botID; m.MentionUserIDs = []string{botID} }), "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reason, ok := Triggered(tc.msg, botID)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.reason, reason)
		})
	}

	_, ok := Triggered(with(human, func(m *InboundMessage) { m.MentionUserIDs = []string{""} }), "")
	assert.False(t, ok, "an unknown bot id never matches")
}

func TestAllowed(t *testing.T) {
	assert.True(t, Allowed("a", nil, nil), "empty allow list means anyone")
	assert.False(t, Allowed("a", nil, []string{"a"}), "deny applies with an empty allow list")
	assert.True(t, Allowed("a", []string{"a", "b"}, nil))
	assert.False(t, Allowed("c", []string{"a", "b"}, nil))
	assert.False(t, Allowed("a", []string{"a"}, []string{"a"}), "deny wins over allow")
}

func TestBindingChannelID(t *testing.T) {
	assert.Equal(t, "c", InboundMessage{ChannelID: "c"}.BindingChannelID())
	assert.Equal(t, "parent", InboundMessage{ChannelID: "thread", ParentChannelID: "parent"}.BindingChannelID())
}

func TestPromptTextNamesTheAuthorAndChannelAndDropsTheMention(t *testing.T) {
	m := InboundMessage{AuthorName: "alice", Content: "<@111>   what do you <@!111> think?"}
	assert.Equal(t, "alice (Discord, #general): what do you think?", PromptText(m, botID, "general"))
	assert.Equal(t, "alice (Discord): what do you think?", PromptText(m, botID, ""))
}

func TestPromptTextQuotesAReplyToSomeoneElseButNotToTheBot(t *testing.T) {
	m := InboundMessage{
		AuthorName: "alice", Content: "<@111> is this right?",
		ReferencedMessageID: "r", ReferencedAuthorID: "333", ReferencedAuthorName: "bob", ReferencedContent: "the sky is green",
	}
	assert.Equal(t, "(replying to bob: \"the sky is green\")\nalice (Discord, #general): is this right?", PromptText(m, botID, "#general"))

	m.ReferencedAuthorID = botID
	assert.Equal(t, "alice (Discord, #general): is this right?", PromptText(m, botID, "general"))
}

func TestPromptTextTruncatesLongQuotesAndHandlesEmptyContent(t *testing.T) {
	m := InboundMessage{
		Content:             "<@111>",
		ReferencedMessageID: "r", ReferencedAuthorID: "333", ReferencedContent: strings.Repeat("x", 600),
	}
	got := PromptText(m, botID, "general")
	assert.Contains(t, got, strings.Repeat("x", maxQuoteRunes)+"…")
	assert.NotContains(t, got, strings.Repeat("x", maxQuoteRunes+1))
	assert.True(t, strings.HasSuffix(got, "someone (Discord, #general): (no text)"))
}

func with(m InboundMessage, f func(*InboundMessage)) InboundMessage {
	f(&m)
	return m
}

// The owner's stated design: an empty allow list means anyone in the channel can
// talk to the persona (deny still wins). That is intentional, so what protects the
// owner's account is the bound thread being sandboxed, not the allow list. Pinned
// so a change to these semantics is deliberate; see also the UI note on the binding form.
func TestEmptyAllowListMeansAnyoneNotDenied(t *testing.T) {
	for _, allow := range [][]string{nil, {}} {
		assert.True(t, Allowed("anyone-at-all", allow, nil))
		assert.True(t, Allowed("anyone-at-all", allow, []string{}))
		assert.True(t, Allowed("anyone-at-all", allow, []string{"someone-else"}))
		assert.False(t, Allowed("denied", allow, []string{"denied"}))
	}
	assert.False(t, Allowed("stranger", []string{"owner"}, nil), "a non-empty allow list is exclusive")
}
