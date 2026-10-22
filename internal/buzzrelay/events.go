package buzzrelay

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
)

// Event kinds the relay reads and writes (Buzz's registry: buzz-core/src/kind.rs).
const (
	KindProfile       = 0     // NIP-01 metadata
	KindStreamMessage = 9     // a channel message (NIP-29 group chat)
	KindJoinRequest   = 9021  // NIP-29 join request
	KindGroupMetadata = 39000 // relay-signed channel metadata, d = channel id
	KindGroupMembers  = 39002 // relay-signed channel roster, d = channel id, p = members
	KindTyping        = 20002 // ephemeral typing indicator
	KindMemberAdded   = 44100 // "you were added to a channel"
	KindMemberRemoved = 44101 // "you were removed from a channel"
	KindCreateGroup   = 9007  // NIP-29 create group (tests and rigs only)
	KindPutUser       = 9000  // NIP-29 add member (tests and rigs only)
	// MaxMessageBytes is the largest message Buzz accepts.
	MaxMessageBytes = 64 * 1024
)

// Profile is a kind 0 profile's content.
type Profile struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	About       string `json:"about,omitempty"`
	Picture     string `json:"picture,omitempty"`
	// Bot marks an automated account (NIP-24).
	Bot bool `json:"bot,omitempty"`
}

// Label is the name to show for a profile: display_name, then name.
func (p Profile) Label() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Name
}

// ParseProfile reads a kind 0 event's content; a malformed one reads as empty.
func ParseProfile(ev nostr.Event) Profile {
	var p Profile
	_ = json.Unmarshal([]byte(ev.Content), &p)
	return p
}

// sign signs an event as the persona, with its owner attestation (when it has one) on every
// event, as Buzz's own agents do.
func sign(s Signer, ev *nostr.Event) error {
	if s.Attestation != nil {
		ev.Tags = append(ev.Tags, s.Attestation.Tag())
	}
	return ev.Sign(s.Keys)
}

// ProfileEvent is the persona's kind 0 profile.
func ProfileEvent(s Signer, p Profile) (*nostr.Event, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	ev := &nostr.Event{Kind: KindProfile, Tags: nostr.Tags{}, Content: string(raw)}
	return ev, sign(s, ev)
}

// ThreadRef places a message in a thread (NIP-10): Root is the thread's first message, Parent
// the message replied to. A direct reply to the root has Root == Parent.
type ThreadRef struct {
	Root   string
	Parent string
}

// MessageEvent is a kind 9 channel message. mentions are pubkeys to p-tag (notify); media
// are imeta tags for attached files.
func MessageEvent(s Signer, channelID, content string, thread *ThreadRef, mentions []string, media [][]string) (*nostr.Event, error) {
	tags := nostr.Tags{{"h", channelID}}
	if thread != nil && thread.Parent != "" {
		if thread.Root == "" || thread.Root == thread.Parent {
			tags = append(tags, []string{"e", thread.Parent, "", "reply"})
		} else {
			tags = append(tags, []string{"e", thread.Root, "", "root"}, []string{"e", thread.Parent, "", "reply"})
		}
	}
	seen := map[string]bool{}
	for _, p := range mentions {
		if p != "" && !seen[p] {
			seen[p] = true
			tags = append(tags, []string{"p", p})
		}
	}
	tags = append(tags, media...)
	ev := &nostr.Event{Kind: KindStreamMessage, Tags: tags, Content: content}
	return ev, sign(s, ev)
}

// TypingEvent is the ephemeral "typing…" pulse in a channel (or a thread of it).
func TypingEvent(s Signer, channelID string, thread *ThreadRef) (*nostr.Event, error) {
	tags := nostr.Tags{{"h", channelID}}
	if thread != nil && thread.Parent != "" {
		if thread.Root != "" && thread.Root != thread.Parent {
			tags = append(tags, []string{"e", thread.Root, "", "root"})
		}
		tags = append(tags, []string{"e", thread.Parent, "", "reply"})
	}
	ev := &nostr.Event{Kind: KindTyping, Tags: tags}
	return ev, sign(s, ev)
}

// JoinEvent asks to join an open channel (NIP-29 kind 9021).
func JoinEvent(s Signer, channelID string) (*nostr.Event, error) {
	ev := &nostr.Event{Kind: KindJoinRequest, Tags: nostr.Tags{{"h", channelID}}}
	return ev, sign(s, ev)
}

// ThreadOf is where a message sits: its root and the message it replies to (NIP-10 markers,
// with the positional form as a fallback). A top-level message has neither.
func ThreadOf(ev nostr.Event) ThreadRef {
	var t ThreadRef
	var positional []string
	for _, tag := range ev.Tags {
		if len(tag) < 2 || tag[0] != "e" {
			continue
		}
		marker := ""
		if len(tag) > 3 {
			marker = tag[3]
		}
		switch marker {
		case "root":
			t.Root = tag[1]
		case "reply":
			t.Parent = tag[1]
		case "mention":
		default:
			positional = append(positional, tag[1])
		}
	}
	if t.Root == "" && t.Parent == "" && len(positional) > 0 {
		t.Root = positional[0]
		t.Parent = positional[len(positional)-1]
	}
	if t.Root == "" {
		t.Root = t.Parent
	}
	if t.Parent == "" {
		t.Parent = t.Root
	}
	return t
}

// ReplyThread is where a reply to ev goes: in ev's thread when it is in one, otherwise in a
// new thread under ev.
func ReplyThread(ev nostr.Event) ThreadRef {
	t := ThreadOf(ev)
	if t.Root == "" {
		return ThreadRef{Root: ev.ID, Parent: ev.ID}
	}
	return ThreadRef{Root: t.Root, Parent: ev.ID}
}

// SplitMessage splits text into parts of at most MaxMessageBytes, on line boundaries where it
// can, without breaking a UTF-8 sequence. Nearly every reply is one part.
func SplitMessage(text string) []string {
	if text == "" {
		return nil
	}
	var parts []string
	for len(text) > MaxMessageBytes {
		cut := MaxMessageBytes
		for i := cut; i > cut/2; i-- {
			if text[i-1] == '\n' {
				cut = i
				break
			}
		}
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		parts = append(parts, text[:cut])
		text = text[cut:]
	}
	return append(parts, text)
}
