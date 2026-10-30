package providerconnections

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
)

// maxModelIDLen bounds a model id an account enables; provider ids are far
// shorter.
const maxModelIDLen = 200

var (
	// ErrDefaultModelInUse is returned when a change would take away the
	// account's default model: disabling it, or removing its connection. The
	// account chooses another default first.
	ErrDefaultModelInUse = errors.New("that is your default model; choose another default first")
	ErrInvalidModelID    = errors.New("model id must be 1 to 200 characters")
)

// ConnectionModel is one of a connection's models as the account sees it: what
// the provider lists, whether the account enabled it, and its capabilities
// (the enabled row's, or what the provider reports).
type ConnectionModel struct {
	ListedModel
	Enabled bool `json:"enabled"`
}

// ModelChoice enables or disables one model on a connection. The capabilities
// are what the account confirmed, prefilled from what the provider reports;
// nil leaves an enabled model's as they are, or off for a new one.
type ModelChoice struct {
	ID            string `json:"id"`
	Enabled       bool   `json:"enabled"`
	DisplayName   string `json:"display_name,omitempty"`
	ToolSupport   *bool  `json:"tool_support,omitempty"`
	VisionSupport *bool  `json:"vision_support,omitempty"`
}

// enabledModels returns the connection's enabled model rows by name.
func (s *Store) enabledModels(ctx context.Context, connectionID uuid.UUID) (map[string]*ent.Model, error) {
	rows, err := s.client.Model.Query().
		Where(entmodel.ConnectionID(connectionID), entmodel.Deleted(false)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read a connection's models: %w", err)
	}
	out := make(map[string]*ent.Model, len(rows))
	for _, row := range rows {
		out[row.Name] = row
	}
	return out, nil
}

// withEnabled merges a provider's listing with the connection's enabled
// models. An enabled model keeps the capabilities the account confirmed; a
// model the provider no longer lists still appears while it is enabled, so it
// can be seen and turned off.
func (s *Store) withEnabled(ctx context.Context, connectionID uuid.UUID, listed []ListedModel) ([]ConnectionModel, error) {
	enabled, err := s.enabledModels(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionModel, 0, len(listed)+len(enabled))
	for _, m := range listed {
		cm := ConnectionModel{ListedModel: m}
		if row, ok := enabled[m.ID]; ok {
			cm.Enabled = true
			cm.ToolSupport, cm.VisionSupport = capabilitiesOf(row)
			delete(enabled, m.ID)
		}
		out = append(out, cm)
	}
	for _, row := range enabled {
		cm := ConnectionModel{ListedModel: ListedModel{ID: row.Name, DisplayName: row.DisplayName}, Enabled: true}
		cm.ToolSupport, cm.VisionSupport = capabilitiesOf(row)
		out = append(out, cm)
	}
	return out, nil
}

func capabilitiesOf(row *ent.Model) (*bool, *bool) {
	tools := row.ToolSupport
	vision := row.VisionSupport != nil && *row.VisionSupport
	return &tools, &vision
}

// defaultModelOn reports whether the account's default model is one of the
// connection's, or one of names on it when names is given.
func (s *Store) defaultModelOn(ctx context.Context, userID, connectionID uuid.UUID, names ...string) (bool, error) {
	where := []predicate.Model{entmodel.ConnectionID(connectionID), entmodel.Deleted(false)}
	if len(names) > 0 {
		where = append(where, entmodel.NameIn(names...))
	}
	return s.client.UserPreference.Query().
		Where(
			userpreference.HasUserWith(user.ID(userID)),
			userpreference.HasModelWith(where...),
		).
		Exist(ctx)
}

// SetModels enables and disables models on one of the account's connections,
// as one change. A model is enabled by creating its row, or bringing back the
// one disabled before; it is disabled by soft-deleting its row, so a chat or
// job that names it fails clearly instead of pointing at nothing.
func (s *Store) SetModels(ctx context.Context, userID uuid.UUID, conn *ent.ProviderConnection, choices []ModelChoice) error {
	var disable []string
	for i, c := range choices {
		c.ID = strings.TrimSpace(c.ID)
		if c.ID == "" || len(c.ID) > maxModelIDLen {
			return ErrInvalidModelID
		}
		choices[i] = c
		if !c.Enabled {
			disable = append(disable, c.ID)
		}
	}
	if len(disable) > 0 {
		inUse, err := s.defaultModelOn(ctx, userID, conn.ID, disable...)
		if err != nil {
			return fmt.Errorf("check the default model: %w", err)
		}
		if inUse {
			return ErrDefaultModelInUse
		}
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin changing a connection's models: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range choices {
		if !c.Enabled {
			if _, err := tx.Model.Update().
				Where(entmodel.ConnectionID(conn.ID), entmodel.Name(c.ID)).
				SetDeleted(true).
				Save(ctx); err != nil {
				return fmt.Errorf("disable model %s: %w", c.ID, err)
			}
			continue
		}
		id, err := enableModel(ctx, tx, conn, c)
		if err != nil {
			return err
		}
		if err := defaultIfNone(ctx, tx, userID, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit a connection's models: %w", err)
	}
	return nil
}

// defaultIfNone makes modelID the account's default model when it has none,
// so the first model an account enables becomes its default (ADR 0x027).
func defaultIfNone(ctx context.Context, tx *ent.Tx, userID, modelID uuid.UUID) error {
	if _, err := tx.UserPreference.Update().
		Where(userpreference.HasUserWith(user.ID(userID)), userpreference.DefaultModelIsNil()).
		SetDefaultModel(modelID).
		Save(ctx); err != nil {
		return fmt.Errorf("set the default model: %w", err)
	}
	return nil
}

// enableModel creates or brings back a model on the connection and returns its ID.
func enableModel(ctx context.Context, tx *ent.Tx, conn *ent.ProviderConnection, c ModelChoice) (uuid.UUID, error) {
	displayName := strings.TrimSpace(c.DisplayName)
	if displayName == "" {
		displayName = c.ID
	}
	existing, err := tx.Model.Query().
		Where(entmodel.ConnectionID(conn.ID), entmodel.Name(c.ID)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		create := tx.Model.Create().
			SetConnectionID(conn.ID).
			SetName(c.ID).
			SetDisplayName(displayName).
			SetDescription("").
			SetProvider(entmodel.Provider(conn.Preset)).
			SetToolSupport(c.ToolSupport != nil && *c.ToolSupport).
			SetVisionSupport(c.VisionSupport != nil && *c.VisionSupport)
		created, err := create.Save(ctx)
		if err != nil {
			return uuid.Nil, fmt.Errorf("enable model %s: %w", c.ID, err)
		}
		return created.ID, nil
	case err != nil:
		return uuid.Nil, fmt.Errorf("read model %s: %w", c.ID, err)
	}
	update := existing.Update().SetDeleted(false).SetDisplayName(displayName)
	if c.ToolSupport != nil {
		update.SetToolSupport(*c.ToolSupport)
	}
	if c.VisionSupport != nil {
		update.SetVisionSupport(*c.VisionSupport)
	}
	if _, err := update.Save(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("enable model %s: %w", c.ID, err)
	}
	return existing.ID, nil
}
