package models

import (
	"time"

	"github.com/google/uuid"
)

// Discord relay models. See docs/adr/0x025-discord-relay.md.

// DiscordBotStatus is the health of a connected bot.
type DiscordBotStatus string

const (
	DiscordBotActive       DiscordBotStatus = "active"
	DiscordBotInvalidToken DiscordBotStatus = "invalid_token"
	DiscordBotDisabled     DiscordBotStatus = "disabled"
)

// DiscordBot is a user's own Discord bot, attached to one of their personas. The
// token is never part of it; see DiscordBotCredentials.
type DiscordBot struct {
	ID             uuid.UUID        `json:"id"`
	PersonalityID  uuid.UUID        `json:"personality_id"`
	ApplicationID  string           `json:"application_id"`
	BotUserID      string           `json:"bot_user_id"`
	BotUsername    string           `json:"bot_username"`
	MessageContent bool             `json:"message_content"`
	Status         DiscordBotStatus `json:"status"`
	LastError      *string          `json:"last_error,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// DiscordBotCreate is what is stored when a bot is added (after its token was
// validated against Discord).
type DiscordBotCreate struct {
	PersonalityID  uuid.UUID
	ApplicationID  string
	BotUserID      string
	BotUsername    string
	Token          string
	MessageContent bool
}

// DiscordBotPatch changes a bot; nil fields are left alone.
type DiscordBotPatch struct {
	Token          *string
	BotUsername    *string
	MessageContent *bool
	Status         *DiscordBotStatus
	// LastError: a pointer to "" clears it.
	LastError *string
}

// DiscordBotCredentials is what the relay needs to act as a bot. It carries the
// decrypted token, so it never leaves the server.
type DiscordBotCredentials struct {
	ID             uuid.UUID
	OwnerID        uuid.UUID
	PersonalityID  uuid.UUID
	BotUserID      string
	Token          string `json:"-"`
	MessageContent bool
}

// DiscordBindingStatus is the health of a binding.
type DiscordBindingStatus string

const (
	DiscordBindingActive DiscordBindingStatus = "active"
	DiscordBindingBroken DiscordBindingStatus = "broken"
)

// DiscordBinding ties one channel, seen by one bot, to a relay thread.
type DiscordBinding struct {
	ID             uuid.UUID `json:"id"`
	BotID          uuid.UUID `json:"bot_id"`
	ChatID         uuid.UUID `json:"chat_id"`
	GuildID        string    `json:"guild_id"`
	ChannelID      string    `json:"channel_id"`
	GuildName      string    `json:"guild_name"`
	ChannelName    string    `json:"channel_name"`
	InboundEnabled bool      `json:"inbound_enabled"`
	AllowUserIDs   []string  `json:"allow_user_ids"`
	DenyUserIDs    []string  `json:"deny_user_ids"`
	// AllowUnrestricted is the owner's stored acknowledgement that the bound thread
	// reads more than public memories (its Memory access is personal or sensitive).
	// Without it the relay answers only threads limited to public memories.
	AllowUnrestricted bool `json:"allow_unrestricted"`
	// ChatPublic is whether the bound thread is limited to public memories right now.
	// Computed by the API on read, never stored; nil when the thread could not be loaded.
	ChatPublic     *bool                `json:"chat_public,omitempty"`
	Status         DiscordBindingStatus `json:"status"`
	LastError      *string              `json:"last_error,omitempty"`
	LastActivityAt *time.Time           `json:"last_activity_at,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

// DiscordBindingCreate is a new binding.
type DiscordBindingCreate struct {
	ChatID         uuid.UUID
	GuildID        string
	ChannelID      string
	GuildName      string
	ChannelName    string
	InboundEnabled bool
	AllowUserIDs   []string
	DenyUserIDs    []string
	// AllowUnrestricted records the acknowledgement for a thread wider than public.
	AllowUnrestricted bool
}

// DiscordBindingPatch changes a binding; nil fields are left alone.
type DiscordBindingPatch struct {
	ChatID            *uuid.UUID
	ChannelName       *string
	GuildName         *string
	InboundEnabled    *bool
	AllowUserIDs      *[]string
	DenyUserIDs       *[]string
	AllowUnrestricted *bool
	Status            *DiscordBindingStatus
	// LastError: a pointer to "" clears it.
	LastError *string
}

// DiscordBindingTarget is a binding resolved for the relay: the binding and the
// bot and owner it acts as.
type DiscordBindingTarget struct {
	Binding DiscordBinding
	Bot     DiscordBotCredentials
}

// DiscordLinkStatus is where a crossing message is.
type DiscordLinkStatus string

const (
	DiscordLinkReceived DiscordLinkStatus = "received"
	DiscordLinkPending  DiscordLinkStatus = "pending"
	DiscordLinkSent     DiscordLinkStatus = "sent"
	DiscordLinkFailed   DiscordLinkStatus = "failed"
)

// DiscordMessageLink is one message that crossed between a relay thread and its
// channel.
type DiscordMessageLink struct {
	ID               uuid.UUID         `json:"id"`
	BindingID        uuid.UUID         `json:"binding_id"`
	Direction        string            `json:"direction"`
	ChatMessageID    *uuid.UUID        `json:"chat_message_id,omitempty"`
	DiscordMessageID *string           `json:"discord_message_id,omitempty"`
	DiscordChannelID string            `json:"discord_channel_id"`
	PostedMessageIDs []string          `json:"posted_message_ids"`
	AuthorID         string            `json:"author_id,omitempty"`
	AuthorName       string            `json:"author_name,omitempty"`
	ReplyToLinkID    *uuid.UUID        `json:"reply_to_link_id,omitempty"`
	Status           DiscordLinkStatus `json:"status"`
	Error            *string           `json:"error,omitempty"`
	Attempts         int               `json:"attempts"`
	CreatedAt        time.Time         `json:"created_at"`
}

// DiscordInbound is a Discord message about to become a user message.
type DiscordInbound struct {
	BindingID        uuid.UUID
	DiscordMessageID string
	DiscordChannelID string
	AuthorID         string
	AuthorName       string
}

// DiscordPendingPostSource is what asked for the next reply to be posted.
type DiscordPendingPostSource string

const (
	DiscordPostFromComposer DiscordPendingPostSource = "composer"
	DiscordPostFromTool     DiscordPendingPostSource = "tool"
)
