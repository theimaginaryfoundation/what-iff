package models

import (
	"time"

	"github.com/google/uuid"
)

// Slack relay models. See docs/adr/0x032-slack-relay.md.

// SlackAppStatus is whether a persona's Slack app is in use.
type SlackAppStatus string

const (
	SlackAppActive       SlackAppStatus = "active"
	SlackAppInvalidToken SlackAppStatus = "invalid_token"
	SlackAppDisabled     SlackAppStatus = "disabled"
)

// SlackApp is a persona's own Slack app in one workspace. The tokens are never part of it;
// see SlackAppCredentials.
type SlackApp struct {
	ID              uuid.UUID      `json:"id"`
	PersonalityID   uuid.UUID      `json:"personality_id"`
	TeamID          string         `json:"team_id"`
	TeamName        string         `json:"team_name"`
	TeamURL         string         `json:"team_url"`
	AppID           string         `json:"app_id"`
	BotUserID       string         `json:"bot_user_id"`
	BotID           string         `json:"bot_id"`
	BotName         string         `json:"bot_name"`
	Status          SlackAppStatus `json:"status"`
	LastError       *string        `json:"last_error,omitempty"`
	LastConnectedAt *time.Time     `json:"last_connected_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// SlackAppCreate is a new app, as checked with Slack.
type SlackAppCreate struct {
	PersonalityID uuid.UUID
	TeamID        string
	TeamName      string
	TeamURL       string
	AppID         string
	BotUserID     string
	BotID         string
	BotName       string
	BotToken      string
	AppToken      string
}

// SlackAppPatch changes an app; nil fields are left alone.
type SlackAppPatch struct {
	BotToken *string
	AppToken *string
	TeamName *string
	TeamURL  *string
	BotName  *string
	Status   *SlackAppStatus
	// LastError: a pointer to "" clears it.
	LastError *string
}

// SlackAppCredentials is what the relay needs to act as the persona's app. It carries the
// decrypted tokens, so it never leaves the server.
type SlackAppCredentials struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	PersonalityID uuid.UUID
	TeamID        string
	AppID         string
	BotUserID     string
	BotID         string
	BotToken      string `json:"-"`
	AppToken      string `json:"-"`
}

// SlackBindingStatus is the health of a binding.
type SlackBindingStatus string

const (
	SlackBindingActive SlackBindingStatus = "active"
	SlackBindingBroken SlackBindingStatus = "broken"
)

// SlackBinding ties one channel of an app's workspace to a relay thread.
type SlackBinding struct {
	ID    uuid.UUID `json:"id"`
	AppID uuid.UUID `json:"app_id"`
	// TeamName is the workspace's cached name, filled where the app is loaded with the binding
	// (listings).
	TeamName       string    `json:"team_name,omitempty"`
	ChatID         uuid.UUID `json:"chat_id"`
	ChannelID      string    `json:"channel_id"`
	ChannelName    string    `json:"channel_name"`
	IsPrivate      bool      `json:"is_private"`
	InboundEnabled bool      `json:"inbound_enabled"`
	AllowUserIDs   []string  `json:"allow_user_ids"`
	DenyUserIDs    []string  `json:"deny_user_ids"`
	AnswerAgents   bool      `json:"answer_agents"`
	// AllowUnrestricted is the owner's stored acknowledgement that the bound thread is not
	// sandboxed. Without it the relay answers only sandboxed threads.
	AllowUnrestricted bool `json:"allow_unrestricted"`
	// ChatSandboxed is whether the bound thread is sandboxed right now. Computed by the API
	// on read, never stored; nil when the thread could not be loaded.
	ChatSandboxed  *bool              `json:"chat_sandboxed,omitempty"`
	Status         SlackBindingStatus `json:"status"`
	LastError      *string            `json:"last_error,omitempty"`
	LastActivityAt *time.Time         `json:"last_activity_at,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// SlackBindingCreate is a new binding.
type SlackBindingCreate struct {
	ChatID            uuid.UUID
	ChannelID         string
	ChannelName       string
	IsPrivate         bool
	InboundEnabled    bool
	AllowUserIDs      []string
	DenyUserIDs       []string
	AnswerAgents      bool
	AllowUnrestricted bool
}

// SlackBindingPatch changes a binding; nil fields are left alone.
type SlackBindingPatch struct {
	ChatID            *uuid.UUID
	ChannelName       *string
	InboundEnabled    *bool
	AllowUserIDs      *[]string
	DenyUserIDs       *[]string
	AnswerAgents      *bool
	AllowUnrestricted *bool
	Status            *SlackBindingStatus
	// LastError: a pointer to "" clears it.
	LastError *string
}

// SlackBindingTarget is a binding resolved for the relay: the binding and the app it acts as.
type SlackBindingTarget struct {
	Binding SlackBinding
	App     SlackAppCredentials
}

// SlackLinkStatus is where a crossing message is.
type SlackLinkStatus string

const (
	SlackLinkReceived SlackLinkStatus = "received"
	SlackLinkPending  SlackLinkStatus = "pending"
	SlackLinkSent     SlackLinkStatus = "sent"
	SlackLinkFailed   SlackLinkStatus = "failed"
)

// SlackMessageLink is one message that crossed between a relay thread and its channel.
type SlackMessageLink struct {
	ID            uuid.UUID       `json:"id"`
	BindingID     uuid.UUID       `json:"binding_id"`
	Direction     string          `json:"direction"`
	ChatMessageID *uuid.UUID      `json:"chat_message_id,omitempty"`
	MessageTS     *string         `json:"message_ts,omitempty"`
	ThreadTS      string          `json:"thread_ts"`
	PostedTS      []string        `json:"posted_ts"`
	AuthorUserID  string          `json:"author_user_id,omitempty"`
	AuthorName    string          `json:"author_name,omitempty"`
	ReplyToLinkID *uuid.UUID      `json:"reply_to_link_id,omitempty"`
	Status        SlackLinkStatus `json:"status"`
	Error         *string         `json:"error,omitempty"`
	Attempts      int             `json:"attempts"`
	// AgentDepth (inbound): consecutive agent-started turns on the binding, this one
	// included; 0 when a person wrote it.
	AgentDepth int       `json:"agent_depth"`
	CreatedAt  time.Time `json:"created_at"`
}

// SlackInbound is a Slack message about to become a user message.
type SlackInbound struct {
	BindingID    uuid.UUID
	MessageTS    string
	ThreadTS     string
	AuthorUserID string
	AuthorName   string
	AgentDepth   int
}

// SlackPendingPostSource is what asked for the next reply to be posted.
type SlackPendingPostSource string

const (
	SlackPostFromComposer SlackPendingPostSource = "composer"
	SlackPostFromTool     SlackPendingPostSource = "tool"
)
