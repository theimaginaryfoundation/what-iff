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
	// UserID, when set, adds the models the account enabled on its own provider
	// connections (ADR 0x027). Another account's connection models are never found.
	UserID uuid.UUID
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

	allModels, err := a.modelsVisibleTo(ctx, opts.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	if parsed, err := uuid.Parse(query); err == nil {
		model, getErr := a.ds.GetModel(ctx, parsed)
		if getErr == nil && (model.ConnectionID == nil || containsModel(allModels, model.ID)) {
			return model, nil
		}
		if getErr != nil && !errors.Is(getErr, datastore.ErrModelNotFound) {
			return nil, fmt.Errorf("failed to get model: %w", getErr)
		}
	}

	var exactMatches []*models.Model
	for _, m := range allModels {
		// A model whose display name is its name matches once.
		if strings.EqualFold(m.Name, query) || (opts.AllowDisplayName && strings.EqualFold(m.DisplayName, query)) {
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

// modelsVisibleTo is the catalog, plus the account's own connection models when
// userID is set.
func (a *Agent) modelsVisibleTo(ctx context.Context, userID uuid.UUID) ([]*models.Model, error) {
	if userID == uuid.Nil {
		return a.ds.ListModels(ctx)
	}
	return a.ds.ListModelsForUser(ctx, userID)
}

func containsModel(list []*models.Model, id uuid.UUID) bool {
	for _, m := range list {
		if m.ID == id {
			return true
		}
	}
	return false
}
