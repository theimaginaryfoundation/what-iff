package models

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EncodeMessageCursor builds an opaque keyset token for the descending chat-message view from a
// message's (sent_at, id). It is URL-safe and unpadded so it survives a query string untouched
// (Angular's HttpParams does not re-encode '+' in standard base64, which would corrupt it). The
// codec lives in models because it is a contract shared between the datastore (which mints the
// token) and the handler (which parses an incoming one).
func EncodeMessageCursor(sentAt time.Time, id uuid.UUID) string {
	raw := sentAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeMessageCursor reverses EncodeMessageCursor. A malformed token is an error rather than a
// silent fall-through so a corrupted cursor cannot quietly restart pagination from the newest page.
func DecodeMessageCursor(token string) (time.Time, uuid.UUID, error) {
	rawBytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("decode message cursor: %w", err)
	}
	parts := strings.SplitN(string(rawBytes), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("malformed message cursor")
	}
	sentAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("parse message cursor time: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("parse message cursor id: %w", err)
	}
	return sentAt, id, nil
}

type PaginatedResponse struct {
	Results    []any `json:"results"`
	TotalCount int   `json:"total_count"`
	TotalPages int   `json:"totalPages,omitempty"`
	Page       int   `json:"page"`
	// NextCursor is an opaque keyset token for continuing a cursor-paginated view past the last
	// row in this response (empty when there is nothing more). Only the descending chat-message
	// view sets it; other paginated endpoints omit it.
	NextCursor string `json:"next_cursor,omitempty"`
}
