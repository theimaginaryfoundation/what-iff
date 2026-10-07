package memory

import (
	"encoding/json"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// parseProvenanceValue validates a provenance string from a request (patch field or list query).
func parseProvenanceValue(raw string) (models.MemoryProvenance, error) {
	p, ok := models.ParseMemoryProvenance(raw)
	if !ok {
		return "", fmt.Errorf("provenance must be user or external")
	}
	return p, nil
}

// parsePatchProvenance reads the optional provenance field of a patch payload.
func parsePatchProvenance(raw json.RawMessage) (*models.MemoryProvenance, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("invalid provenance")
	}
	p, err := parseProvenanceValue(value)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
