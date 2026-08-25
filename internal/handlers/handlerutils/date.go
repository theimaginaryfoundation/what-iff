package handlerutils

import (
	"time"
)

// parseDate parses a date string into a time.Time, handling various formats and incomplete dates
func parseDate(dateStr string) time.Time {
	if dateStr == "" || dateStr == "null" {
		return time.Time{} // Return zero value
	}

	// Try full date format: YYYY-MM-DD
	if t, err := time.Parse("2006-01-02", dateStr); err == nil {
		return t
	}

	// Try year-month format: YYYY-MM
	if t, err := time.Parse("2006-01", dateStr); err == nil {
		return t
	}

	// Try month-year format: MM/YYYY or MM-YYYY
	for _, format := range []string{"01/2006", "01-2006"} {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t
		}
	}

	// Try just year: YYYY
	if len(dateStr) == 4 {
		if t, err := time.Parse("2006", dateStr); err == nil {
			return t
		}
	}

	// Default to zero time if no format matches
	return time.Time{}
}
