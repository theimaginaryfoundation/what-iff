package search

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// parseSearchParams extracts and validates query, types, and limit_per_type
// from the request URL. Returns an error suitable for a 400 response so the
// handler does not have to know the validation rules.
func parseSearchParams(r *http.Request) (query string, types []string, limitPerType int, err error) {
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	runes := []rune(q)
	if len(runes) < minQueryRunes {
		return "", nil, 0, errors.New("query must be at least 2 characters")
	}
	if len(runes) > maxQueryRunes {
		return "", nil, 0, errors.New("query exceeds maximum length")
	}

	rawTypes := strings.TrimSpace(r.URL.Query().Get("types"))
	requestedTypes, terr := parseTypes(rawTypes)
	if terr != nil {
		return "", nil, 0, terr
	}

	limit := defaultLimitPerTyp
	if raw := strings.TrimSpace(r.URL.Query().Get("limit_per_type")); raw != "" {
		n, perr := strconv.Atoi(raw)
		if perr != nil {
			return "", nil, 0, errors.New("limit_per_type must be an integer")
		}
		if n < 1 || n > maxLimitPerType {
			return "", nil, 0, errors.New("limit_per_type must be between 1 and 25")
		}
		limit = n
	}

	return q, requestedTypes, limit, nil
}

// parseTypes splits a CSV `types` value into the canonical slice. An empty
// or missing value defaults to AllTypes; unknown tokens return an error so
// typos surface as 400s instead of silently dropping sections.
func parseTypes(raw string) ([]string, error) {
	if raw == "" {
		dup := make([]string, len(AllTypes))
		copy(dup, AllTypes)
		return dup, nil
	}

	wanted := make(map[string]struct{}, len(AllTypes))
	for _, part := range strings.Split(raw, ",") {
		t := strings.ToLower(strings.TrimSpace(part))
		if t == "" {
			continue
		}
		if !isKnownType(t) {
			return nil, errors.New("unknown type: " + t)
		}
		wanted[t] = struct{}{}
	}
	if len(wanted) == 0 {
		return nil, errors.New("types must include at least one value")
	}

	// Preserve the canonical order so response sections render predictably.
	out := make([]string, 0, len(wanted))
	for _, t := range AllTypes {
		if _, ok := wanted[t]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func isKnownType(t string) bool {
	for _, known := range AllTypes {
		if known == t {
			return true
		}
	}
	return false
}
