package webhook

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

const (
	// defaultReadPageSize is the page size when a client does not ask for one.
	defaultReadPageSize = 20
	// maxReadPageSize bounds a page. Webhook clients poll on their own schedule, so an unbounded
	// limit would let one integration put arbitrary load on its owner's account. Larger values are
	// clamped, not rejected: a client asking for "as many as you'll give me" gets the maximum.
	maxReadPageSize = 100
)

// A readParams error is always the caller's fault and is shown to them verbatim, so each message
// says what to send instead. Unlike the session API, which quietly ignores a malformed filter,
// the webhook reads reject one: an integration should never believe it filtered when it did not.

// pagingFrom reads page and limit. Absent values take defaults; a present value must be a positive
// integer; a limit above maxReadPageSize is clamped to it.
func pagingFrom(q url.Values) (page, limit int, err error) {
	page, limit = 1, defaultReadPageSize
	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		if page, err = strconv.Atoi(raw); err != nil || page < 1 {
			return 0, 0, fmt.Errorf("page must be a positive integer")
		}
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil || limit < 1 {
			return 0, 0, fmt.Errorf("limit must be a positive integer (maximum %d)", maxReadPageSize)
		}
		limit = min(limit, maxReadPageSize)
	}
	return page, limit, nil
}

// timeParam parses an optional RFC 3339 time.
func timeParam(q url.Values, name string) (*time.Time, error) {
	raw := strings.TrimSpace(q.Get(name))
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be an RFC 3339 time, e.g. 2026-09-30T14:00:00Z", name)
	}
	return &t, nil
}

// boolParam parses an optional boolean.
func boolParam(q url.Values, name string) (*bool, error) {
	raw := strings.TrimSpace(q.Get(name))
	if raw == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be true or false", name)
	}
	return &b, nil
}

// textParam returns a trimmed optional string.
func textParam(q url.Values, name string) *string {
	if v := strings.TrimSpace(q.Get(name)); v != "" {
		return &v
	}
	return nil
}

// messageFiltersFrom builds thread-message filters: origin (user or assistant) and a min_date and
// max_date range over sent_at.
func messageFiltersFrom(q url.Values) (models.ChatMessageFilters, error) {
	var f models.ChatMessageFilters
	switch raw := strings.ToLower(strings.TrimSpace(q.Get("origin"))); raw {
	case "":
	case "user":
		o := models.MessageOriginUser
		f.Origin = &o
	case "assistant":
		o := models.MessageOriginAssistant
		f.Origin = &o
	default:
		return f, fmt.Errorf("origin must be user or assistant")
	}
	var err error
	if f.MinDate, err = timeParam(q, "min_date"); err != nil {
		return f, err
	}
	if f.MaxDate, err = timeParam(q, "max_date"); err != nil {
		return f, err
	}
	return f, nil
}

// chatFiltersFrom builds thread-list filters. personality_id is how a client lists the threads of
// one persona; archived threads are excluded unless archived=true asks for them.
func chatFiltersFrom(q url.Values) (models.ChatFilters, error) {
	var f models.ChatFilters
	f.Name = textParam(q, "name")
	f.Query = textParam(q, "search")
	f.Tag = textParam(q, "tag")
	if raw := strings.TrimSpace(q.Get("personality_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return f, fmt.Errorf("personality_id must be a UUID (list personas with GET /webhooks/personality)")
		}
		f.PersonalityID = &id
	}
	var err error
	if f.IsFavorite, err = boolParam(q, "is_favorite"); err != nil {
		return f, err
	}
	if f.Archived, err = boolParam(q, "archived"); err != nil {
		return f, err
	}
	if f.MinDate, err = timeParam(q, "min_date"); err != nil {
		return f, err
	}
	if f.MaxDate, err = timeParam(q, "max_date"); err != nil {
		return f, err
	}
	return f, nil
}

// personalityFiltersFrom builds persona-list filters.
func personalityFiltersFrom(q url.Values) models.PersonalityFilters {
	return models.PersonalityFilters{Name: textParam(q, "name"), Query: textParam(q, "search")}
}
