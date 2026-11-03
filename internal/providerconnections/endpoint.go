package providerconnections

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Endpoint is where a stored connection is reached for chat, and with what key.
type Endpoint struct {
	Preset  string
	Flavor  Flavor
	BaseURL string
	// Key is the connection's key, decrypted, or empty when it has none.
	Key string
}

// ErrAddressNotAllowed is returned for a connection at an address the account
// chose, on a deployment that does not allow choosing one.
var ErrAddressNotAllowed = errors.New("this server does not allow choosing a provider's address")

// Resolver finds the endpoint behind an account's connection, for the agent's
// chat, subagent and job calls on that connection's models.
type Resolver struct {
	store   *Store
	presets []Preset
}

// NewResolver builds a Resolver over the same store and presets as the routes.
func NewResolver(store *Store, presets []Preset) *Resolver {
	return &Resolver{store: store, presets: presets}
}

// Endpoint returns where the account's connection is reached. It refuses a
// connection the account does not own (ErrNotFound), and, like the routes, one
// whose address the account chose when CustomAddressesAllowed is false.
func (r *Resolver) Endpoint(ctx context.Context, userID, connectionID uuid.UUID) (Endpoint, error) {
	row, err := r.store.get(ctx, userID, connectionID)
	if err != nil {
		return Endpoint{}, err
	}
	preset := presetOf(r.presets, row.Preset)
	if preset.AddressEditable && !CustomAddressesAllowed() {
		return Endpoint{}, ErrAddressNotAllowed
	}
	key, err := r.store.keyOf(row)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{Preset: row.Preset, Flavor: flavorOf(row, preset), BaseURL: addressOf(row, preset), Key: key}, nil
}
