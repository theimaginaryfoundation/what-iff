// Package providerconnections holds how accounts reach model providers (ADR
// 0x027): the connections table, the presets that fill one in, listing a
// provider's models, and the routes an account manages its connections
// through.
//
// It is the only code that writes the provider_connections table.
// internal/accountkeys keeps the older per-provider key routes and the
// resolver, over this package's built-in connections.
package providerconnections

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/providerconnection"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Bounds on a stored key. Real provider keys sit well inside them; the minimum
// keeps the four-character hint from being the whole key.
const (
	minKeyLen = 8
	maxKeyLen = 512
)

var (
	ErrUnknownProvider = errors.New("unknown model provider")
	ErrInvalidKey      = errors.New("provider key must be 8 to 512 characters, without spaces")
	ErrNotFound        = errors.New("no such provider connection")
	// ErrBuiltinExists is returned when an account adds a second connection to
	// a built-in provider. It edits the one it has instead.
	ErrBuiltinExists = errors.New("this account already has a connection to that provider")
)

// Sealer encrypts keys at rest with the deployment's token key.
type Sealer interface {
	EncryptToken(plaintext string) (string, error)
	DecryptToken(ciphertext string) (string, error)
}

// Connection describes a connection without its key.
type Connection struct {
	ID     uuid.UUID `json:"id"`
	Preset string    `json:"preset"`
	Name   string    `json:"name"`
	Flavor Flavor    `json:"flavor,omitempty"`
	// BaseURL is the address the account chose; empty means the preset's own.
	BaseURL string `json:"base_url,omitempty"`
	// Hint is the key's last four characters, empty when there is no key.
	Hint    string `json:"hint,omitempty"`
	HasKey  bool   `json:"has_key"`
	Builtin bool   `json:"builtin"`
	// Source is "converted" for a key stored before connections existed, and
	// empty for one added in the app.
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toConnection(row *ent.ProviderConnection) Connection {
	return Connection{
		ID:        row.ID,
		Preset:    row.Preset,
		Name:      row.Name,
		Flavor:    Flavor(row.Flavor),
		BaseURL:   row.BaseURL,
		Hint:      row.Hint,
		HasKey:    row.Key != "",
		Builtin:   row.Builtin != nil,
		Source:    row.Source,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// Store reads and writes connections.
type Store struct {
	client *ent.Client
	sealer Sealer
}

// NewStore builds a Store over the database and the deployment's token key.
func NewStore(client *ent.Client, sealer Sealer) *Store {
	return &Store{client: client, sealer: sealer}
}

// checkKey trims a key and checks its bounds.
func checkKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if len(key) < minKeyLen || len(key) > maxKeyLen || strings.ContainsFunc(key, unicode.IsSpace) {
		return "", ErrInvalidKey
	}
	return key, nil
}

// seal checks and encrypts a key, returning the ciphertext and its hint.
func (s *Store) seal(key string) (sealed, hint string, err error) {
	key, err = checkKey(key)
	if err != nil {
		return "", "", err
	}
	sealed, err = s.sealer.EncryptToken(key)
	if err != nil {
		return "", "", fmt.Errorf("encrypt provider key: %w", err)
	}
	return sealed, key[len(key)-4:], nil
}

// open decrypts a stored key; an empty one stays empty.
func (s *Store) open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	key, err := s.sealer.DecryptToken(sealed)
	if err != nil {
		return "", fmt.Errorf("decrypt provider key: %w", err)
	}
	return key, nil
}

// --- Built-in connections, one per provider per account.
//
// These match on the account, so they never touch a deployment-owned
// connection, which has no account and its own one-per-provider index (ADR
// 0x027).

// SetBuiltinKey stores the account's key for a built-in provider on its
// connection to it, creating the connection or replacing its key.
func (s *Store) SetBuiltinKey(ctx context.Context, userID uuid.UUID, provider models.ModelProvider, key string) (Connection, error) {
	if !provider.Known() {
		return Connection{}, ErrUnknownProvider
	}
	sealed, hint, err := s.seal(key)
	if err != nil {
		return Connection{}, err
	}
	id, err := s.client.ProviderConnection.Create().
		SetUserID(userID).
		SetPreset(string(provider)).
		SetBuiltin(string(provider)).
		SetName(provider.DisplayName()).
		SetKey(sealed).
		SetHint(hint).
		OnConflictColumns(providerconnection.FieldUserID, providerconnection.FieldBuiltin).
		UpdateKey().
		UpdateHint().
		UpdateUpdatedAt().
		ID(ctx)
	if err != nil {
		return Connection{}, fmt.Errorf("store provider key: %w", err)
	}
	row, err := s.client.ProviderConnection.Get(ctx, id)
	if err != nil {
		return Connection{}, fmt.Errorf("read stored provider key: %w", err)
	}
	return toConnection(row), nil
}

// Builtins lists the account's built-in connections, in provider order.
func (s *Store) Builtins(ctx context.Context, userID uuid.UUID) ([]Connection, error) {
	rows, err := s.client.ProviderConnection.Query().
		Where(providerconnection.UserID(userID), providerconnection.BuiltinNotNil()).
		Order(ent.Asc(providerconnection.FieldBuiltin)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list provider keys: %w", err)
	}
	return toConnections(rows), nil
}

// DeleteBuiltin removes the account's connection to a built-in provider.
func (s *Store) DeleteBuiltin(ctx context.Context, userID uuid.UUID, provider models.ModelProvider) error {
	n, err := s.client.ProviderConnection.Delete().
		Where(providerconnection.UserID(userID), providerconnection.Builtin(string(provider))).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete provider key: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// BuiltinKey returns the account's decrypted key for a built-in provider, or ""
// when it has none.
func (s *Store) BuiltinKey(ctx context.Context, userID uuid.UUID, provider models.ModelProvider) (string, error) {
	row, err := s.client.ProviderConnection.Query().
		Where(providerconnection.UserID(userID), providerconnection.Builtin(string(provider))).
		Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read provider key: %w", err)
	}
	return s.open(row.Key)
}

// hasBuiltin reports whether the account already has a connection to a
// built-in provider.
func (s *Store) hasBuiltin(ctx context.Context, userID uuid.UUID, preset string) (bool, error) {
	return s.client.ProviderConnection.Query().
		Where(providerconnection.UserID(userID), providerconnection.Builtin(preset)).
		Exist(ctx)
}

// --- Any connection.

func toConnections(rows []*ent.ProviderConnection) []Connection {
	out := make([]Connection, 0, len(rows))
	for _, row := range rows {
		out = append(out, toConnection(row))
	}
	return out
}

// List describes the account's connections: built-in ones first in provider
// order, then the rest by name.
func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]Connection, error) {
	rows, err := s.client.ProviderConnection.Query().
		Where(providerconnection.UserID(userID)).
		Order(ent.Asc(providerconnection.FieldBuiltin), ent.Asc(providerconnection.FieldName), ent.Asc(providerconnection.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list provider connections: %w", err)
	}
	out := toConnections(rows)
	// Built-in rows sort first wherever the database puts NULLs.
	builtin, other := out[:0:0], out[:0:0]
	for _, c := range out {
		if c.Builtin {
			builtin = append(builtin, c)
		} else {
			other = append(other, c)
		}
	}
	return append(builtin, other...), nil
}

// get returns one of the account's connections.
func (s *Store) get(ctx context.Context, userID, id uuid.UUID) (*ent.ProviderConnection, error) {
	row, err := s.client.ProviderConnection.Query().
		Where(providerconnection.ID(id), providerconnection.UserID(userID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read provider connection: %w", err)
	}
	return row, nil
}

// NewConnection is a connection to store, already checked against its preset.
type NewConnection struct {
	Preset  string
	Builtin bool
	Name    string
	Flavor  Flavor
	BaseURL string
	Key     string
}

// Create stores a new connection for the account.
func (s *Store) Create(ctx context.Context, userID uuid.UUID, c NewConnection) (Connection, error) {
	create := s.client.ProviderConnection.Create().
		SetUserID(userID).
		SetPreset(c.Preset).
		SetName(c.Name).
		SetFlavor(string(c.Flavor)).
		SetBaseURL(c.BaseURL)
	if c.Builtin {
		create.SetBuiltin(c.Preset)
	}
	if c.Key != "" {
		sealed, hint, err := s.seal(c.Key)
		if err != nil {
			return Connection{}, err
		}
		create.SetKey(sealed).SetHint(hint)
	}
	row, err := create.Save(ctx)
	if c.Builtin && ent.IsConstraintError(err) {
		return Connection{}, ErrBuiltinExists
	}
	if err != nil {
		return Connection{}, fmt.Errorf("store provider connection: %w", err)
	}
	return toConnection(row), nil
}

// Changes are the fields an edit sets; nil leaves a field as it is. An empty
// Key removes the key.
type Changes struct {
	Name    *string
	BaseURL *string
	Key     *string
}

// Update applies changes to one of the account's connections.
func (s *Store) Update(ctx context.Context, userID, id uuid.UUID, c Changes) (Connection, error) {
	row, err := s.get(ctx, userID, id)
	if err != nil {
		return Connection{}, err
	}
	update := row.Update()
	if c.Name != nil {
		update.SetName(*c.Name)
	}
	if c.BaseURL != nil {
		update.SetBaseURL(*c.BaseURL)
	}
	if c.Key != nil {
		if *c.Key == "" {
			update.SetKey("").SetHint("")
		} else {
			sealed, hint, err := s.seal(*c.Key)
			if err != nil {
				return Connection{}, err
			}
			update.SetKey(sealed).SetHint(hint)
		}
	}
	row, err = update.Save(ctx)
	if err != nil {
		return Connection{}, fmt.Errorf("update provider connection: %w", err)
	}
	return toConnection(row), nil
}

// Delete removes one of the account's connections.
func (s *Store) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := s.client.ProviderConnection.Delete().
		Where(providerconnection.ID(id), providerconnection.UserID(userID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete provider connection: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// keyOf returns a connection's decrypted key, or "" when it has none.
func (s *Store) keyOf(row *ent.ProviderConnection) (string, error) {
	return s.open(row.Key)
}
