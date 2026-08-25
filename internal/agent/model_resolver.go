package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type modelResolverOptions struct {
	AllowDisplayName bool
	// AllowPrefixMatch enables a prefix-match fallback when no exact name/display-name match
	// is found. Only models whose name (or display name, when AllowDisplayName is set) starts
	// with the supplied string (case-insensitive) are considered. Callers should supply a
	// prefix specific enough to produce a single match; if multiple models share the prefix the
	// resolver returns an "ambiguous" error.
	AllowPrefixMatch bool
}

// resolveModelByReference resolves a model from a raw string by trying, in order:
//  1. UUID parse → exact lookup by ID.
//  2. Case-insensitive exact name match (and display name when AllowDisplayName is set).
//  3. Prefix match on name / display name (when AllowPrefixMatch is set).
//
// Agents should prefer UUIDs or exact model names to avoid ambiguity.
func (a *Agent) resolveModelByReference(ctx context.Context, raw string, opts modelResolverOptions) (*models.Model, error) {
	query := strings.TrimSpace(raw)
	if query == "" {
		return nil, fmt.Errorf("model reference is required")
	}
	if a.ds == nil {
		return nil, fmt.Errorf("model %q not found", query)
	}

	if parsed, err := uuid.Parse(query); err == nil {
		model, getErr := a.ds.GetModel(ctx, parsed)
		if getErr == nil {
			return model, nil
		}
		if !errors.Is(getErr, datastore.ErrModelNotFound) {
			return nil, fmt.Errorf("failed to get model: %w", getErr)
		}
	}

	allModels, err := a.ds.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	var exactMatches []*models.Model
	for _, m := range allModels {
		if strings.EqualFold(m.Name, query) {
			exactMatches = append(exactMatches, m)
		}
		if opts.AllowDisplayName && strings.EqualFold(m.DisplayName, query) {
			exactMatches = append(exactMatches, m)
		}
	}
	if len(exactMatches) == 1 {
		return exactMatches[0], nil
	}
	if len(exactMatches) > 1 {
		return nil, fmt.Errorf("model reference %q is ambiguous (%d exact matches)", query, len(exactMatches))
	}

	if opts.AllowPrefixMatch {
		needle := strings.ToLower(query)
		var prefixMatches []*models.Model
		for _, m := range allModels {
			if strings.HasPrefix(strings.ToLower(m.Name), needle) {
				prefixMatches = append(prefixMatches, m)
				continue
			}
			if opts.AllowDisplayName && strings.HasPrefix(strings.ToLower(m.DisplayName), needle) {
				prefixMatches = append(prefixMatches, m)
			}
		}
		if len(prefixMatches) == 1 {
			return prefixMatches[0], nil
		}
		if len(prefixMatches) > 1 {
			return nil, fmt.Errorf("model reference %q is ambiguous (%d prefix matches); supply a more specific name or use the model UUID", query, len(prefixMatches))
		}
	}

	return nil, fmt.Errorf("model %q not found", query)
}
