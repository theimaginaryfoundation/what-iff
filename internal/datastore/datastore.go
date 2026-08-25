package datastore

import (
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
