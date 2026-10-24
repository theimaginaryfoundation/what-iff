package models

import (
	"time"

	"github.com/google/uuid"
)

// Buzz relay models. See docs/adr/0x028-buzz-relay.md.

// BuzzAgentStatus is whether a persona's Buzz identity is in use.
type BuzzAgentStatus string

const (
	BuzzAgentActive   BuzzAgentStatus = "active"
	BuzzAgentDisabled BuzzAgentStatus = "disabled"
)

// BuzzAgent is a persona's Buzz identity. The secret key is never part of it; see
// BuzzAgentCredentials.
type BuzzAgent struct {
	ID            uuid.UUID       `json:"id"`
	PersonalityID uuid.UUID       `json:"personality_id"`
	PubKey        string          `json:"pubkey"`
	NPub          string          `json:"npub"`
	Status        BuzzAgentStatus `json:"status"`
	LastError     *string         `json:"last_error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// BuzzAgentCreate is a new identity: the generated keypair.
type BuzzAgentCreate struct {
	PersonalityID uuid.UUID
	PubKey        string
	SecretKey     string
}

// BuzzAgentCredentials is what the relay needs to act as the persona. It carries the
// decrypted secret key, so it never leaves the server.
type BuzzAgentCredentials struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	PersonalityID uuid.UUID
	PubKey        string
	SecretKey     string `json:"-"`
}

// BuzzCommunityStatus is how a persona's membership of a community stands.
type BuzzCommunityStatus string

const (
	BuzzCommunityActive      BuzzCommunityStatus = "active"
	BuzzCommunityNeedsAccess BuzzCommunityStatus = "needs_access"
	BuzzCommunityError       BuzzCommunityStatus = "error"
	BuzzCommunityDisabled    BuzzCommunityStatus = "disabled"
)

// BuzzCommunity is one community a persona is in, or is trying to get into.
type BuzzCommunity struct {
	ID          uuid.UUID `json:"id"`
	AgentID     uuid.UUID `json:"agent_id"`
	RelayURL    string    `json:"relay_url"`
	Name        string    `json:"name"`
	RelayPubKey string    `json:"relay_pubkey"`
	// Attested is whether the owner gave an attestation for the persona's key; OwnerPubKey
	// is whose. The attestation itself is not returned.
	Attested        bool                `json:"attested"`
	OwnerPubKey     *string             `json:"owner_pubkey,omitempty"`
	PolicyVersion   *string             `json:"policy_version,omitempty"`
	Status          BuzzCommunityStatus `json:"status"`
	LastError       *string             `json:"last_error,omitempty"`
	LastConnectedAt *time.Time          `json:"last_connected_at,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// BuzzCommunityCreate is a new community for an agent.
type BuzzCommunityCreate struct {
	RelayURL      string
	Name          string
	RelayPubKey   string
	AuthTag       *string
	OwnerPubKey   *string
	PolicyVersion *string
	Status        BuzzCommunityStatus
	LastError     *string
}

// BuzzCommunityPatch changes a community; nil fields are left alone.
type BuzzCommunityPatch struct {
	Name        *string
	RelayPubKey *string
	// AuthTag and OwnerPubKey: a pointer to "" clears them.
	AuthTag       *string
	OwnerPubKey   *string
	PolicyVersion *string
	Status        *BuzzCommunityStatus
	// LastError: a pointer to "" clears it.
	LastError    *string
	ConnectedNow bool
}

// BuzzConnection is everything the gateway needs to hold one connection: the persona's
// credentials, the community, its attestation, and the channels to listen to.
type BuzzConnection struct {
	Agent     BuzzAgentCredentials
	Community BuzzCommunity
	AuthTag   string `json:"-"`
	// Channels are the bound channels with inbound turned on, and when each was bound
	// (events older than their binding are never answered).
	Channels []BuzzListenChannel
}

// BuzzListenChannel is one channel a connection subscribes to.
type BuzzListenChannel struct {
	BindingID uuid.UUID
	ChannelID string
	BoundAt   time.Time
}

// BuzzBindingStatus is the health of a binding.
type BuzzBindingStatus string

const (
	BuzzBindingActive BuzzBindingStatus = "active"
	BuzzBindingBroken BuzzBindingStatus = "broken"
)

// BuzzBinding ties one channel of a community to a relay thread.
type BuzzBinding struct {
	ID          uuid.UUID `json:"id"`
	CommunityID uuid.UUID `json:"community_id"`
	// CommunityName is the community's cached name, filled where the community is loaded with
	// the binding (listings).
	CommunityName  string    `json:"community_name,omitempty"`
	ChatID         uuid.UUID `json:"chat_id"`
	ChannelID      string    `json:"channel_id"`
	ChannelName    string    `json:"channel_name"`
	InboundEnabled bool      `json:"inbound_enabled"`
	AllowPubKeys   []string  `json:"allow_pubkeys"`
	DenyPubKeys    []string  `json:"deny_pubkeys"`
	AnswerAgents   bool      `json:"answer_agents"`
	// AllowUnrestricted is the owner's stored acknowledgement that the bound thread is not
	// sandboxed. Without it the relay answers only sandboxed threads.
	AllowUnrestricted bool `json:"allow_unrestricted"`
	// ChatSandboxed is whether the bound thread is sandboxed right now. Computed by the API
	// on read, never stored; nil when the thread could not be loaded.
	ChatSandboxed  *bool             `json:"chat_sandboxed,omitempty"`
	Status         BuzzBindingStatus `json:"status"`
	LastError      *string           `json:"last_error,omitempty"`
	LastActivityAt *time.Time        `json:"last_activity_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// BuzzBindingCreate is a new binding.
type BuzzBindingCreate struct {
	ChatID            uuid.UUID
	ChannelID         string
	ChannelName       string
	InboundEnabled    bool
	AllowPubKeys      []string
	DenyPubKeys       []string
	AnswerAgents      bool
	AllowUnrestricted bool
}

// BuzzBindingPatch changes a binding; nil fields are left alone.
type BuzzBindingPatch struct {
	ChatID            *uuid.UUID
	ChannelName       *string
	InboundEnabled    *bool
	AllowPubKeys      *[]string
	DenyPubKeys       *[]string
	AnswerAgents      *bool
	AllowUnrestricted *bool
	Status            *BuzzBindingStatus
	// LastError: a pointer to "" clears it.
	LastError *string
}

// BuzzBindingTarget is a binding resolved for the relay: the binding, its community, and
// the persona it acts as.
type BuzzBindingTarget struct {
	Binding   BuzzBinding
	Community BuzzCommunity
	Agent     BuzzAgentCredentials
	AuthTag   string `json:"-"`
}

// BuzzLinkStatus is where a crossing message is.
type BuzzLinkStatus string

const (
	BuzzLinkReceived BuzzLinkStatus = "received"
	BuzzLinkPending  BuzzLinkStatus = "pending"
	BuzzLinkSent     BuzzLinkStatus = "sent"
	BuzzLinkFailed   BuzzLinkStatus = "failed"
)

// BuzzMessageLink is one message that crossed between a relay thread and its channel.
type BuzzMessageLink struct {
	ID             uuid.UUID      `json:"id"`
	BindingID      uuid.UUID      `json:"binding_id"`
	Direction      string         `json:"direction"`
	ChatMessageID  *uuid.UUID     `json:"chat_message_id,omitempty"`
	EventID        *string        `json:"event_id,omitempty"`
	ThreadRoot     string         `json:"thread_root"`
	PostedEventIDs []string       `json:"posted_event_ids"`
	AuthorPubKey   string         `json:"author_pubkey,omitempty"`
	AuthorName     string         `json:"author_name,omitempty"`
	ReplyToLinkID  *uuid.UUID     `json:"reply_to_link_id,omitempty"`
	Status         BuzzLinkStatus `json:"status"`
	Error          *string        `json:"error,omitempty"`
	Attempts       int            `json:"attempts"`
	CreatedAt      time.Time      `json:"created_at"`
}

// BuzzInbound is a Buzz event about to become a user message.
type BuzzInbound struct {
	BindingID    uuid.UUID
	EventID      string
	ThreadRoot   string
	AuthorPubKey string
	AuthorName   string
}

// BuzzPendingPostSource is what asked for the next reply to be posted.
type BuzzPendingPostSource string

const (
	BuzzPostFromComposer BuzzPendingPostSource = "composer"
	BuzzPostFromTool     BuzzPendingPostSource = "tool"
)
