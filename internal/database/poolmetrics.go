package database

import (
	"context"
	"database/sql"

	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// telemetry.AttrDBConnectionState is the semconv attribute for db.client.connection.count.

// RegisterPoolMetrics samples db's connection pool at each metrics export:
// db.client.connection.count by db.client.connection.state (idle, used), and the cumulative wait count and wait
// time for a free connection (whatiff.db.pool.*), which rise when MaxOpenConns is too low.
// Call once per process; the returned function unregisters the callback.
func RegisterPoolMetrics(m *telemetry.Metrics, db *sql.DB) (func() error, error) {
	if m == nil || db == nil {
		return func() error { return nil }, nil
	}
	gauges := []telemetry.Gauge{telemetry.DBPoolConnections, telemetry.DBPoolWaits, telemetry.DBPoolWaitTime}
	return m.RegisterGauges(gauges, func(_ context.Context, report telemetry.GaugeObserver) error {
		stats := db.Stats()
		report(telemetry.DBPoolConnections, float64(stats.Idle), telemetry.AttrDBConnectionState.String("idle"))
		report(telemetry.DBPoolConnections, float64(stats.InUse), telemetry.AttrDBConnectionState.String("used"))
		report(telemetry.DBPoolWaits, float64(stats.WaitCount))
		report(telemetry.DBPoolWaitTime, stats.WaitDuration.Seconds())
		return nil
	})
}
