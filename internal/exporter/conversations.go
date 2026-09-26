package exporter

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// conversationsFile is the repo path of the round-trippable conversation export. It uses the
// Anthropic (Claude) conversations.json shape, which our own importer parses in-house
// (internal/handlers/chat/anthropic_import.go) — so an exported bundle re-imports cleanly.
const conversationsFile = "conversations.json"

// ConversationInput is a clean, id-stripped conversation ready for projection. The only identifier
// carried is ID, and it is emitted solely as the export "uuid" the importer hashes for dedup
// (sha256(uuid) → the (owner_id, import_hash) unique index). It is never persisted as a DB key on
// import, so an export loads into any account without collision. No message/context/tool-call
// Postgres ids, no response_id, no read_status.
type ConversationInput struct {
	ID                         uuid.UUID
	Title                      string
	CreatedAt                  time.Time
	Messages                   []MessageInput
	PersonalityID              *uuid.UUID
	CheckpointSummary          string
	CheckpointUserMessageCount int
	LastCheckpointAt           *time.Time
	DisabledTools              []string
	Tags                       []string
	IsFavorite                 bool
	IsAutoMood                 bool
}

// MessageInput is one user/assistant message. Non-chat roles (system/tool) are filtered upstream.
type MessageInput struct {
	Origin models.MessageOrigin
	Text   string
	SentAt time.Time
}

// exportConversation mirrors an Anthropic conversations.json entry (the subset our importer reads).
type exportConversation struct {
	UUID                            string              `json:"uuid"`
	Name                            string              `json:"name"`
	CreatedAt                       time.Time           `json:"created_at"`
	ChatMessages                    []exportChatMessage `json:"chat_messages"`
	WhatiffPersonalityID            *uuid.UUID          `json:"whatiff_personality_id,omitempty"`
	WhatiffCheckpointSummary        string              `json:"whatiff_checkpoint_summary,omitempty"`
	WhatiffCheckpointUserMessageCnt int                 `json:"whatiff_checkpoint_user_message_count,omitempty"`
	WhatiffLastCheckpointAt         *time.Time          `json:"whatiff_last_checkpoint_at,omitempty"`
	WhatiffDisabledTools            []string            `json:"whatiff_disabled_tools,omitempty"`
	WhatiffTags                     []string            `json:"whatiff_tags,omitempty"`
	WhatiffIsFavorite               bool                `json:"whatiff_is_favorite,omitempty"`
	WhatiffIsAutoMood               bool                `json:"whatiff_is_auto_mood"`
}

type exportChatMessage struct {
	Sender    string    `json:"sender"` // "human" | "assistant"
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// senderForOrigin maps our MessageOrigin to the Anthropic export sender vocabulary. ok=false drops
// the message from the export (mirrors the importer's role filter, keeping export/import symmetric).
func senderForOrigin(o models.MessageOrigin) (sender string, ok bool) {
	switch o {
	case models.MessageOriginUser:
		return "human", true
	case models.MessageOriginAssistant:
		return "assistant", true
	default:
		return "", false
	}
}

// WriteConversations projects convs into conversations.json on the tree. Output is deterministic:
// conversations are ordered by (CreatedAt, ID) and messages by (SentAt) as given, so identical
// account state yields byte-identical output and clean incremental commits. Empty-text and
// non-user/assistant messages are dropped; a conversation with no surviving messages is omitted.
func WriteConversations(tree Tree, convs []ConversationInput) error {
	b, err := BuildConversationsJSON(convs)
	if err != nil {
		return err
	}
	return tree.Write(conversationsFile, b)
}

// ParsedConversation is a decoded conversations.json entry (import side). It mirrors the emitted
// Anthropic shape without pulling in the datastore's import model, so the exporter package stays
// dependency-light and owns both sides of the format.
type ParsedConversation struct {
	UUID                            string
	Name                            string
	CreatedAt                       time.Time
	Messages                        []ParsedMessage
	WhatiffPersonalityID            *uuid.UUID
	WhatiffCheckpointSummary        string
	WhatiffCheckpointUserMessageCnt int
	WhatiffLastCheckpointAt         *time.Time
	WhatiffDisabledTools            []string
	WhatiffTags                     []string
	WhatiffIsFavorite               bool
	WhatiffIsAutoMood               bool
}

// ParsedMessage is one decoded message; Sender is "human" or "assistant".
type ParsedMessage struct {
	Sender    string
	Text      string
	CreatedAt time.Time
}

// ParseConversations decodes conversations.json (the exporter's own output) for re-import.
func ParseConversations(data []byte) ([]ParsedConversation, error) {
	var raw []exportConversation
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]ParsedConversation, 0, len(raw))
	for _, c := range raw {
		msgs := make([]ParsedMessage, 0, len(c.ChatMessages))
		for _, m := range c.ChatMessages {
			msgs = append(msgs, ParsedMessage{Sender: m.Sender, Text: m.Text, CreatedAt: m.CreatedAt})
		}
		out = append(out, ParsedConversation{
			UUID:                            c.UUID,
			Name:                            c.Name,
			CreatedAt:                       c.CreatedAt,
			Messages:                        msgs,
			WhatiffPersonalityID:            c.WhatiffPersonalityID,
			WhatiffCheckpointSummary:        c.WhatiffCheckpointSummary,
			WhatiffCheckpointUserMessageCnt: c.WhatiffCheckpointUserMessageCnt,
			WhatiffLastCheckpointAt:         c.WhatiffLastCheckpointAt,
			WhatiffDisabledTools:            c.WhatiffDisabledTools,
			WhatiffTags:                     c.WhatiffTags,
			WhatiffIsFavorite:               c.WhatiffIsFavorite,
			WhatiffIsAutoMood:               c.WhatiffIsAutoMood,
		})
	}
	return out, nil
}

// BuildConversationsJSON renders convs to the Anthropic conversations.json bytes. Exported so the
// projection and its round-trip test can share one code path.
func BuildConversationsJSON(convs []ConversationInput) ([]byte, error) {
	ordered := make([]ConversationInput, len(convs))
	copy(ordered, convs)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID.String() < ordered[j].ID.String()
	})

	out := make([]exportConversation, 0, len(ordered))
	for _, c := range ordered {
		msgs := make([]exportChatMessage, 0, len(c.Messages))
		for _, m := range c.Messages {
			sender, ok := senderForOrigin(m.Origin)
			if !ok || m.Text == "" {
				continue
			}
			msgs = append(msgs, exportChatMessage{
				Sender:    sender,
				Text:      m.Text,
				CreatedAt: m.SentAt.UTC(),
			})
		}
		if len(msgs) == 0 {
			continue
		}
		out = append(out, exportConversation{
			UUID:                            c.ID.String(),
			Name:                            c.Title,
			CreatedAt:                       c.CreatedAt.UTC(),
			ChatMessages:                    msgs,
			WhatiffPersonalityID:            c.PersonalityID,
			WhatiffCheckpointSummary:        c.CheckpointSummary,
			WhatiffCheckpointUserMessageCnt: c.CheckpointUserMessageCount,
			WhatiffLastCheckpointAt:         c.LastCheckpointAt,
			WhatiffDisabledTools:            c.DisabledTools,
			WhatiffTags:                     c.Tags,
			WhatiffIsFavorite:               c.IsFavorite,
			WhatiffIsAutoMood:               c.IsAutoMood,
		})
	}

	// Indented + HTML-escaping disabled: the bundle is meant to be read and diffed by humans and
	// coding agents, and message text routinely contains <, >, & that must not become \u escapes.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
