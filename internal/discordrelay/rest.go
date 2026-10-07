package discordrelay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// Application flags that mean the Message Content intent is enabled (the
// unverified, under-100-servers form is the LIMITED one).
const (
	appFlagGatewayMessageContent        = 1 << 18
	appFlagGatewayMessageContentLimited = 1 << 19
)

// BotPermissions is what the invite link asks for: View Channel, Send Messages,
// Send Messages in Threads, Embed Links, Attach Files, Read Message History and
// Add Reactions.
const BotPermissions = discordgo.PermissionViewChannel |
	discordgo.PermissionSendMessages |
	discordgo.PermissionSendMessagesInThreads |
	discordgo.PermissionEmbedLinks |
	discordgo.PermissionAttachFiles |
	discordgo.PermissionReadMessageHistory |
	discordgo.PermissionAddReactions

// ErrInvalidToken is returned when Discord rejects a bot token.
var ErrInvalidToken = errors.New("discord rejected the bot token")

// BotIdentity is what a valid token says about its bot.
type BotIdentity struct {
	ApplicationID  string
	BotUserID      string
	Username       string
	MessageContent bool
}

// Guild is a server the bot is in.
type Guild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Channel is a channel the bot could be bound to.
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// File is an attachment to upload with a post.
type File struct {
	Name        string
	ContentType string
	Reader      io.Reader
}

// Discord is the REST surface the relay uses, one bot token at a time. The real
// implementation is REST; tests use a fake.
type Discord interface {
	Identify(ctx context.Context, token string) (BotIdentity, error)
	Guilds(ctx context.Context, token string) ([]Guild, error)
	Channels(ctx context.Context, token, guildID string) ([]Channel, error)
	// Post sends parts in order, the first as a reply to replyTo when set, with
	// files attached to the last part. It returns the posted message ids (as many
	// as succeeded, even on error). No mentions are ever parsed from the text
	// except the replied-to author.
	Post(ctx context.Context, token, channelID, replyTo string, parts []string, files []File) ([]string, error)
	Typing(ctx context.Context, token, channelID string) error
	// SetProfile changes the bot's username and/or avatar (a data URI); empty
	// values are left alone.
	SetProfile(ctx context.Context, token, username, avatarDataURI string) error
}

// InviteURL is the link that adds a bot to a server, with BotPermissions.
func InviteURL(applicationID string) string {
	return fmt.Sprintf("https://discord.com/oauth2/authorize?client_id=%s&scope=bot&permissions=%d", applicationID, BotPermissions)
}

// REST is the discordgo-backed Discord. Sessions are cached per token so each
// bot keeps its own rate-limit buckets.
type REST struct {
	mu       sync.Mutex
	sessions map[string]*discordgo.Session
}

// NewREST returns a REST client.
func NewREST() *REST { return &REST{sessions: map[string]*discordgo.Session{}} }

func (r *REST) session(token string) (*discordgo.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[token]; ok {
		return s, nil
	}
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	// Do not let one bot's 429 stall a request indefinitely; the caller retries.
	s.ShouldRetryOnRateLimit = true
	s.MaxRestRetries = 2
	r.sessions[token] = s
	return s, nil
}

// Forget drops the cached session for a token (after it was replaced or revoked).
func (r *REST) Forget(token string) {
	r.mu.Lock()
	delete(r.sessions, token)
	r.mu.Unlock()
}

func (r *REST) Identify(ctx context.Context, token string) (BotIdentity, error) {
	s, err := r.session(token)
	if err != nil {
		return BotIdentity{}, err
	}
	u, err := s.User("@me", discordgo.WithContext(ctx))
	if err != nil {
		r.Forget(token)
		return BotIdentity{}, classify(err)
	}
	if !u.Bot {
		return BotIdentity{}, ErrInvalidToken
	}
	app, err := s.Application("@me")
	if err != nil {
		return BotIdentity{}, classify(err)
	}
	return BotIdentity{
		ApplicationID:  app.ID,
		BotUserID:      u.ID,
		Username:       u.Username,
		MessageContent: app.Flags&(appFlagGatewayMessageContent|appFlagGatewayMessageContentLimited) != 0,
	}, nil
}

func (r *REST) Guilds(ctx context.Context, token string) ([]Guild, error) {
	s, err := r.session(token)
	if err != nil {
		return nil, err
	}
	gs, err := s.UserGuilds(200, "", "", false, discordgo.WithContext(ctx))
	if err != nil {
		return nil, classify(err)
	}
	out := make([]Guild, 0, len(gs))
	for _, g := range gs {
		out = append(out, Guild{ID: g.ID, Name: g.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *REST) Channels(ctx context.Context, token, guildID string) ([]Channel, error) {
	s, err := r.session(token)
	if err != nil {
		return nil, err
	}
	cs, err := s.GuildChannels(guildID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, classify(err)
	}
	var out []Channel
	for _, c := range cs {
		// Text and announcement channels; threads in them follow their parent.
		if c.Type == discordgo.ChannelTypeGuildText || c.Type == discordgo.ChannelTypeGuildNews {
			out = append(out, Channel{ID: c.ID, Name: c.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *REST) Post(ctx context.Context, token, channelID, replyTo string, parts []string, files []File) ([]string, error) {
	s, err := r.session(token)
	if err != nil {
		return nil, err
	}
	var ids []string
	for i, part := range parts {
		msg := &discordgo.MessageSend{
			Content: part,
			// Nothing in the model's text may ping anyone; only the person being
			// replied to is notified.
			AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}, RepliedUser: true},
		}
		if i == 0 && replyTo != "" {
			msg.Reference = &discordgo.MessageReference{MessageID: replyTo, ChannelID: channelID, FailIfNotExists: boolPtr(false)}
		}
		if i == len(parts)-1 {
			for _, f := range files {
				msg.Files = append(msg.Files, &discordgo.File{Name: f.Name, ContentType: f.ContentType, Reader: f.Reader})
			}
		}
		m, err := s.ChannelMessageSendComplex(channelID, msg, discordgo.WithContext(ctx))
		if err != nil {
			return ids, classify(err)
		}
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func (r *REST) Typing(ctx context.Context, token, channelID string) error {
	s, err := r.session(token)
	if err != nil {
		return err
	}
	return classify(s.ChannelTyping(channelID, discordgo.WithContext(ctx)))
}

func (r *REST) SetProfile(ctx context.Context, token, username, avatarDataURI string) error {
	s, err := r.session(token)
	if err != nil {
		return err
	}
	_, err = s.UserUpdate(username, avatarDataURI, "", discordgo.WithContext(ctx))
	return classify(err)
}

// ErrNoAccess is returned when the bot cannot see or post in a channel (it was
// removed from the server, or lacks permission).
var ErrNoAccess = errors.New("the bot cannot access that channel")

// classify turns Discord's auth failures into the relay's sentinel errors.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var rest *discordgo.RESTError
	if errors.As(err, &rest) && rest.Response != nil {
		switch rest.Response.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("%w: %v", ErrInvalidToken, err)
		case http.StatusForbidden, http.StatusNotFound:
			return fmt.Errorf("%w: %v", ErrNoAccess, err)
		}
	}
	return err
}

func boolPtr(b bool) *bool { return &b }
