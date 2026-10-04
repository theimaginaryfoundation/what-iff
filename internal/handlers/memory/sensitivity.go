package memory

import (
	"encoding/json"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// parseSensitivityValue validates a sensitivity string from a request (create body, patch field
// or list query). Empty is rejected here; callers decide whether an absent value means default.
func parseSensitivityValue(raw string) (models.MemorySensitivity, error) {
	s, ok := models.ParseMemorySensitivity(raw)
	if !ok {
		return "", fmt.Errorf("sensitivity must be public, personal, or sensitive")
	}
	return s, nil
}

// parsePatchSensitivity reads the optional sensitivity field of a patch payload.
func parsePatchSensitivity(raw json.RawMessage) (*models.MemorySensitivity, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("invalid sensitivity")
	}
	s, err := parseSensitivityValue(value)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
