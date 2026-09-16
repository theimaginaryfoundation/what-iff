package datastore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"go.uber.org/zap"
)

// Datastore handles database operations
type Datastore struct {
	dbClient    *ent.Client
	sqlDB       *sql.DB
	logger      *zap.Logger
	metrics     *telemetry.Metrics
	tokenCrypto *tokenCrypto
	// providerConfigured reports whether the calling account can reach a
	// provider. Injected rather than imported: the provider-key registry reads
	// through this package, so depending on it here would cycle.
	//
	// nil means "assume reachable", which is the fail-open answer — an account
	// seeing a model it cannot yet use is a better failure than an empty picker.
	providerConfigured func(ctx context.Context, provider string) bool
}

// SetProviderConfigured injects the provider-reachability check used when
// seeding an account's model list. Called once at wiring time.
func (d *Datastore) SetProviderConfigured(fn func(ctx context.Context, provider string) bool) {
	d.providerConfigured = fn
}

// canReachProvider is the nil-safe form of providerConfigured.
func (d *Datastore) canReachProvider(ctx context.Context, provider string) bool {
	if d.providerConfigured == nil {
		return true
	}
	return d.providerConfigured(ctx, provider)
}

// NewDatastore creates a new Datastore.
// sqlDB is the underlying *sql.DB used for raw queries that don't need transactions.
// metrics may be nil (e.g. in tests); optional counters are skipped when unset.
func NewDatastore(dbClient *ent.Client, sqlDB *sql.DB, logger *zap.Logger, tokenEncryptionSecret string, metrics *telemetry.Metrics) (*Datastore, error) {
	ds := &Datastore{
		dbClient: dbClient,
		sqlDB:    sqlDB,
		logger:   logger,
		metrics:  metrics,
	}

	crypto, err := newTokenCrypto(tokenEncryptionSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize token crypto: %w", err)
	}
	ds.tokenCrypto = crypto

	return ds, nil
}
