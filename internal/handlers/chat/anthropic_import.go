package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Supported import export formats.
const (
	importFormatOpenAI    = "openai"
	importFormatAnthropic = "anthropic"
)

// formatProbe captures the discriminating keys of the first conversation in an export so we can
// route to the correct parser. OpenAI exports key messages under a "mapping" graph and carry a
// "conversation_id"; Anthropic exports carry a flat "chat_messages" array.
type formatProbe struct {
	Mapping        json.RawMessage `json:"mapping"`
	ConversationID *string         `json:"conversation_id"`
	ChatMessages   json.RawMessage `json:"chat_messages"`
}

func (p formatProbe) format() string {
	if len(p.ChatMessages) > 0 {
		return importFormatAnthropic
	}
	// "mapping"/"conversation_id" (or anything else) is handled by the OpenAI parser, which also
	// tolerates object-wrapped archives.
	return importFormatOpenAI
}

// detectImportFormat sniffs the leading JSON structure to decide which parser to use without
// consuming the whole (potentially very large) stream. Callers must re-open / seek the source to
// the start before parsing for real.
func detectImportFormat(r io.Reader) (string, error) {
	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if err != nil {
		return "", fmt.Errorf("reading first JSON token: %w", err)
	}

	delim, ok := tok.(json.Delim)
	if !ok {
		return "", errors.New("unrecognized export: expected a JSON array or object at top level")
	}

	switch delim {
	case '[':
		if !dec.More() {
			return "", errors.New("export contains no conversations")
		}
		var probe formatProbe
		if err := dec.Decode(&probe); err != nil {
			return "", fmt.Errorf("probing first conversation: %w", err)
		}
		return probe.format(), nil
	case '{':
		// Object-wrapped archive (e.g. {"conversations": [...]}). Only OpenAI exports use this shape.
		return importFormatOpenAI, nil
	default:
		return "", errors.New("unrecognized export: unexpected top-level JSON")
	}
}

// anthropicConversation mirrors the relevant fields of an Anthropic conversations.json entry.
type anthropicConversation struct {
	UUID         string             `json:"uuid"`
	Name         string             `json:"name"`
	CreatedAt    time.Time          `json:"created_at"`
	ChatMessages []anthropicMessage `json:"chat_messages"`
}

type anthropicMessage struct {
	Sender    string                  `json:"sender"` // "human" | "assistant"
	Text      string                  `json:"text"`
	CreatedAt time.Time               `json:"created_at"`
	Content   []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// messageText returns the human/assistant-visible text for a message, preferring the flattened
// top-level "text" and falling back to concatenating "text"-type content blocks (tool blocks ignored).
func (m anthropicMessage) messageText() string {
	if m.Text != "" {
		return m.Text
	}
	var combined string
	for _, b := range m.Content {
		if b.Type == "text" && b.Text != "" {
			if combined != "" {
				combined += "\n"
			}
			combined += b.Text
		}
	}
	return combined
}

// parseAnthropicArchive stream-decodes an Anthropic conversations.json export into role-filtered
// ImportConversations ready for persistence, returning per-conversation parse errors separately.
// now must be in UTC and is used as the fallback when a timestamp is absent.
func parseAnthropicArchive(ctx context.Context, r io.Reader, now time.Time) ([]models.ImportConversation, []string, error) {
	now = now.UTC()
	dec := json.NewDecoder(r)

	tok, err := dec.Token()
	if err != nil {
		return nil, nil, fmt.Errorf("reading first JSON token: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, nil, errors.New("expected a JSON array of conversations")
	}

	var (
		convs []models.ImportConversation
		errs  []string
		idx   int
	)

	for dec.More() {
		if err := ctx.Err(); err != nil {
			return convs, errs, err
		}
		idx++

		var elem json.RawMessage
		if err := dec.Decode(&elem); err != nil {
			errs = append(errs, fmt.Sprintf("conversation entry %d: malformed entry in archive; skipped", idx))
			continue
		}

		var raw anthropicConversation
		if err := json.Unmarshal(elem, &raw); err != nil {
			errs = append(errs, fmt.Sprintf("conversation entry %d: malformed or unsupported entry; skipped", idx))
			continue
		}

		conv, convErrs := prepareAnthropicConversation(raw, now)
		errs = append(errs, convErrs...)
		if conv != nil {
			convs = append(convs, *conv)
		}
	}

	return convs, errs, nil
}

// anthropicImportOrigin maps a Claude export chat_messages[].sender to a persistable MessageOrigin.
// ok=false means the message is not imported; knownSkip=true for expected non-chat senders.
func anthropicImportOrigin(sender string) (origin models.MessageOrigin, ok bool, knownSkip bool) {
	switch sender {
	case "human":
		return models.MessageOriginUser, true, false
	case "assistant":
		return models.MessageOriginAssistant, true, false
	case "system", "tool":
		return "", false, true
	default:
		return "", false, false
	}
}

// prepareAnthropicConversation maps a single raw Anthropic conversation to an ImportConversation,
// applying the same role-filtering and title-fallback rules as the OpenAI path. Returns nil when the
// conversation has no usable messages or no stable ID.
func prepareAnthropicConversation(raw anthropicConversation, now time.Time) (*models.ImportConversation, []string) {
	var errs []string

	title := raw.Name
	if title == "" {
		ts := now
		if !raw.CreatedAt.IsZero() {
			ts = raw.CreatedAt.UTC()
		}
		title = "Imported chat " + ts.Format("2006-01-02 15:04")
	}

	var (
		msgs         []models.ChatMessage
		unknownRoles int
	)
	for _, m := range raw.ChatMessages {
		origin, ok, knownSkip := anthropicImportOrigin(m.Sender)
		if !ok {
			if !knownSkip {
				unknownRoles++
			}
			continue
		}

		text := m.messageText()
		if text == "" {
			continue
		}

		sentAt := now
		if !m.CreatedAt.IsZero() {
			sentAt = m.CreatedAt.UTC()
		}

		msgs = append(msgs, models.ChatMessage{
			Message:    text,
			Origin:     origin,
			ReadStatus: models.MessageReadStatusRead,
			SentAt:     sentAt,
		})
	}

	if unknownRoles > 0 {
		errs = append(errs, fmt.Sprintf("conversation %q: dropped %d message(s) with unknown/unsupported senders", models.TruncateImportTitle(title), unknownRoles))
	}

	if len(msgs) == 0 {
		errs = append(errs, fmt.Sprintf("conversation %q: no human/assistant messages after filtering; skipped", models.TruncateImportTitle(title)))
		return nil, errs
	}

	if raw.UUID == "" {
		errs = append(errs, fmt.Sprintf("conversation %q: missing conversation UUID; skipped", models.TruncateImportTitle(title)))
		return nil, errs
	}

	createdAt := now
	if !raw.CreatedAt.IsZero() {
		createdAt = raw.CreatedAt.UTC()
	}

	return &models.ImportConversation{
		Title:      title,
		CreatedAt:  createdAt,
		Source:     models.ChatSourceAnthropic,
		ImportHash: conversationHash(raw.UUID),
		Messages:   msgs,
	}, errs
}
