package providermodels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenAILister reads GET /v1/models.
//
// The response carries id, object, created, owned_by and shutdown_date, and
// nothing else — no display name, no tool-support flag, no modalities. Anything
// richer than a name has to come from somewhere else.
type OpenAILister struct {
	BaseURL string
	Client  *http.Client
	// Now is injectable so retirement is testable without waiting for a date.
	Now func() time.Time
}

type openAIModelsResponse struct {
	Data []struct {
		ID           string  `json:"id"`
		ShutdownDate *string `json:"shutdown_date"`
	} `json:"data"`
}

func (l *OpenAILister) List(ctx context.Context, apiKey string) ([]Model, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("no API key available for openai")
	}
	base := strings.TrimSuffix(strings.TrimSpace(l.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach OpenAI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// The body can carry the caller's key back in an error message, so it is
		// deliberately not included here or in the logs.
		return nil, fmt.Errorf("OpenAI returned %d listing models", resp.StatusCode)
	}

	var payload openAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("could not read OpenAI's model list: %w", err)
	}

	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	out := make([]Model, 0, len(payload.Data))
	for _, m := range payload.Data {
		shutdown := ""
		if m.ShutdownDate != nil {
			shutdown = *m.ShutdownDate
		}
		out = append(out, Model{
			ID:        m.ID,
			Kind:      Classify(m.ID),
			RetiresOn: shutdown,
			Retired:   retiredAsOf(shutdown, now()),
		})
	}
	return out, nil
}
