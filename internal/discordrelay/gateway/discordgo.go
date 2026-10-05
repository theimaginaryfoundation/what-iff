package gateway

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/theimaginaryfoundation/what-iff/internal/discordrelay"
	"go.uber.org/zap"
)

// baseIntents are what every bot connection asks for: guild metadata (so thread
// parents resolve from the state cache) and guild messages. Without the
// privileged Message Content intent Discord still sends the content of messages
// that mention the bot, which is the main trigger.
const baseIntents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages

// DiscordgoDialer opens real Gateway connections with discordgo.
func DiscordgoDialer(logger *zap.Logger) Dialer {
	return func(_ context.Context, bot Bot, onMessage func(discordrelay.InboundMessage)) (Conn, error) {
		session, err := discordgo.New("Bot " + bot.Token)
		if err != nil {
			return nil, fmt.Errorf("discord session: %w", err)
		}
		session.Identify.Intents = baseIntents
		if bot.MessageContent {
			session.Identify.Intents |= discordgo.IntentMessageContent
		}
		session.ShouldReconnectOnError = true
		session.StateEnabled = true
		session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
			if m == nil || m.Message == nil {
				return
			}
			onMessage(ToInbound(s, m.Message))
		})
		if err := session.Open(); err != nil {
			return nil, fmt.Errorf("discord gateway open: %w", err)
		}
		return session, nil
	}
}

// ToInbound converts a discordgo message. s may be nil (no thread lookup).
func ToInbound(s *discordgo.Session, m *discordgo.Message) discordrelay.InboundMessage {
	in := discordrelay.InboundMessage{
		ID:        m.ID,
		GuildID:   m.GuildID,
		ChannelID: m.ChannelID,
		WebhookID: m.WebhookID,
		Content:   m.Content,
	}
	if m.Author != nil {
		in.AuthorID = m.Author.ID
		in.AuthorName = m.Author.DisplayName()
		in.AuthorIsBot = m.Author.Bot
	}
	if m.Member != nil && m.Member.Nick != "" {
		in.AuthorName = m.Member.Nick
	}
	for _, a := range m.Attachments {
		if a != nil && a.URL != "" {
			in.Attachments = append(in.Attachments, discordrelay.Attachment{Filename: a.Filename, ContentType: a.ContentType, URL: a.URL, Size: a.Size})
		}
	}
	for _, u := range m.Mentions {
		if u != nil {
			in.MentionUserIDs = append(in.MentionUserIDs, u.ID)
		}
	}
	if ref := m.ReferencedMessage; ref != nil {
		in.ReferencedMessageID = ref.ID
		in.ReferencedContent = ref.Content
		if ref.Author != nil {
			in.ReferencedAuthorID = ref.Author.ID
			in.ReferencedAuthorName = ref.Author.DisplayName()
		}
	} else if m.MessageReference != nil {
		in.ReferencedMessageID = m.MessageReference.MessageID
	}
	if s != nil && s.State != nil {
		if ch, err := s.State.Channel(m.ChannelID); err == nil && ch != nil && ch.IsThread() {
			in.ParentChannelID = ch.ParentID
		}
	}
	return in
}
