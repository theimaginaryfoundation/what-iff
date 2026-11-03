package slackrelay

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// EventCallback is an Events API payload (what a Socket Mode "events_api" envelope carries).
type EventCallback struct {
	Type    string          `json:"type"`
	TeamID  string          `json:"team_id"`
	AppID   string          `json:"api_app_id"`
	EventID string          `json:"event_id"`
	Event   json.RawMessage `json:"event"`
}

// EventType reads the inner event's type ("message", "channel_rename", …).
func (c EventCallback) EventType() string {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(c.Event, &head)
	return head.Type
}

// Message is a message event (and a message as conversations.replies returns it).
type Message struct {
	Type        string      `json:"type"`
	Subtype     string      `json:"subtype,omitempty"`
	Channel     string      `json:"channel,omitempty"`
	ChannelType string      `json:"channel_type,omitempty"`
	User        string      `json:"user,omitempty"`
	BotID       string      `json:"bot_id,omitempty"`
	BotProfile  *BotProfile `json:"bot_profile,omitempty"`
	Text        string      `json:"text"`
	TS          string      `json:"ts"`
	ThreadTS    string      `json:"thread_ts,omitempty"`
	Files       []File      `json:"files,omitempty"`
}

// BotProfile names the app behind a bot's message.
type BotProfile struct {
	ID    string `json:"id"`
	AppID string `json:"app_id"`
	Name  string `json:"name"`
}

// File is a file shared with a message.
type File struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Title              string `json:"title"`
	Mimetype           string `json:"mimetype"`
	Size               int64  `json:"size"`
	URLPrivateDownload string `json:"url_private_download"`
	URLPrivate         string `json:"url_private"`
	// Mode is "tombstone" for a deleted file and "hidden_by_limit" past a free plan's limit;
	// FileAccess is "check_file_info" when the event left the file's details out (Slack Connect).
	Mode       string `json:"mode,omitempty"`
	FileAccess string `json:"file_access,omitempty"`
}

// ChannelRef is the channel of a channel_rename or group_rename event.
type ChannelRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ThreadRoot is the thread a reply to m goes into: its thread, or a new one under m.
func (m Message) ThreadRoot() string {
	if m.ThreadTS != "" {
		return m.ThreadTS
	}
	return m.TS
}

// InThread reports whether m was posted inside a thread (not its first message).
func (m Message) InThread() bool { return m.ThreadTS != "" && m.ThreadTS != m.TS }

// Time is when m was posted, from its ts ("1700000000.000100").
func (m Message) Time() time.Time { return tsTime(m.TS) }

func tsTime(ts string) time.Time {
	sec, frac, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}
	}
	var ns int64
	if frac != "" {
		frac = (frac + "000000000")[:9]
		ns, _ = strconv.ParseInt(frac, 10, 64)
	}
	return time.Unix(s, ns)
}

// MentionsUser reports whether m's text mentions userID (<@U…> or <@U…|name>).
func (m Message) MentionsUser(userID string) bool {
	if userID == "" {
		return false
	}
	return strings.Contains(m.Text, "<@"+userID+">") || strings.Contains(m.Text, "<@"+userID+"|")
}

// plainSubtypes are the message subtypes a person (or an app) writing in the channel produces;
// edits, deletes, joins, topic changes and the rest are never answered.
var plainSubtypes = map[string]bool{"": true, "thread_broadcast": true, "file_share": true, "bot_message": true}

// IsPlain reports whether m is something someone wrote, not a change to the channel or to an
// earlier message.
func (m Message) IsPlain() bool { return plainSubtypes[m.Subtype] }

// FromBot reports whether m was posted by an app's bot rather than a person.
func (m Message) FromBot() bool { return m.BotID != "" || m.Subtype == "bot_message" }
