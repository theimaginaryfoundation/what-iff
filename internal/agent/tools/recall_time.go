package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// timeWindow is a resolved [Min, Max] time filter. Either bound may be nil,
// meaning "unbounded on that side".
type timeWindow struct {
	Min *time.Time
	Max *time.Time
}

// acceptedDateLayouts are the date/datetime formats parseTimeScope understands for
// explicit dates. Bare dates are interpreted at UTC midnight.
var acceptedDateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"2006/01/02",
	"01/02/2006",
}

var (
	reLastN    = regexp.MustCompile(`(?i)^last\s+(\d+)\s+(hour|hours|day|days|week|weeks|month|months)$`)
	reBefore   = regexp.MustCompile(`(?i)^before\s+(.+)$`)
	reAfter    = regexp.MustCompile(`(?i)^(?:after|since)\s+(.+)$`)
	reBetween  = regexp.MustCompile(`(?i)^between\s+(.+?)\s+and\s+(.+)$`)
	reBareDate = regexp.MustCompile(`^\s*[\d/:\sTZ.+-]+\s*$`)
)

// parseTimeScope resolves a natural-language or ISO-8601 time scope into a timeWindow.
// Supported forms (case-insensitive):
//
//	"last 7 days" / "last 3 hours" / "last 2 weeks" / "last 6 months"
//	"before 2025-01-01"
//	"after 2025-06-01" (also "since ...")
//	"between 2025-01-01 and 2025-03-01"
//	a bare date ("2025-06-01") — treated as after that date
//
// An empty scope returns a zero window (no filtering) and no error. `now` is injected
// so callers (and tests) control the reference point for relative windows.
func parseTimeScope(scope string, now time.Time) (timeWindow, error) {
	s := strings.TrimSpace(scope)
	if s == "" {
		return timeWindow{}, nil
	}

	if m := reLastN.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return timeWindow{}, fmt.Errorf("invalid duration count in time_scope: %q", scope)
		}
		min := now.Add(-relativeDuration(n, m[2]))
		return timeWindow{Min: &min}, nil
	}

	if m := reBetween.FindStringSubmatch(s); m != nil {
		start, err := parseDate(m[1])
		if err != nil {
			return timeWindow{}, err
		}
		end, err := parseDate(m[2])
		if err != nil {
			return timeWindow{}, err
		}
		// Normalize order so callers never see Min > Max.
		if start.After(end) {
			start, end = end, start
		}
		return timeWindow{Min: &start, Max: &end}, nil
	}

	if m := reBefore.FindStringSubmatch(s); m != nil {
		d, err := parseDate(m[1])
		if err != nil {
			return timeWindow{}, err
		}
		return timeWindow{Max: &d}, nil
	}

	if m := reAfter.FindStringSubmatch(s); m != nil {
		d, err := parseDate(m[1])
		if err != nil {
			return timeWindow{}, err
		}
		return timeWindow{Min: &d}, nil
	}

	// Bare date → treat as "after".
	if reBareDate.MatchString(s) {
		d, err := parseDate(s)
		if err != nil {
			return timeWindow{}, err
		}
		return timeWindow{Min: &d}, nil
	}

	return timeWindow{}, fmt.Errorf("unrecognized time_scope: %q", scope)
}

func relativeDuration(n int, unit string) time.Duration {
	switch strings.ToLower(unit) {
	case "hour", "hours":
		return time.Duration(n) * time.Hour
	case "week", "weeks":
		return time.Duration(n) * 7 * 24 * time.Hour
	case "month", "months":
		// Calendar months are irregular; approximate at 30 days for a coarse window.
		return time.Duration(n) * 30 * 24 * time.Hour
	default: // day/days
		return time.Duration(n) * 24 * time.Hour
	}
}

// timeScopeReceipt builds an inspectable receipt for a parsed time_scope. Relative windows
// ("last N days") include NormalizedEnd=now so the caller can see the closed interval that was applied.
func timeScopeReceipt(input string, w timeWindow, now time.Time) *recallTimeScope {
	input = strings.TrimSpace(input)
	if input == "" || (w.Min == nil && w.Max == nil) {
		return nil
	}
	r := &recallTimeScope{Input: input, Timezone: "UTC"}
	if w.Min != nil {
		r.NormalizedStart = w.Min.UTC().Format(time.RFC3339)
	}
	if w.Max != nil {
		r.NormalizedEnd = w.Max.UTC().Format(time.RFC3339)
	} else if w.Min != nil && reLastN.MatchString(input) {
		// Open-ended upper bound for "last N …" is the reference now used to resolve the window.
		r.NormalizedEnd = now.UTC().Format(time.RFC3339)
	}
	return r
}

func parseDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	for _, layout := range acceptedDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse date %q (use ISO-8601, e.g. 2025-06-01)", raw)
}
