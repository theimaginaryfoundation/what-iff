package database

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"

	_ "github.com/mattn/go-sqlite3"
)

func TestRegisterPoolMetrics(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Ping()) // leaves one idle connection

	conn, err := db.Conn(t.Context()) // hold one in use
	require.NoError(t, err)
	defer conn.Close()

	tm := telemetrytest.New(t)
	unregister, err := RegisterPoolMetrics(tm.Metrics, db)
	require.NoError(t, err)
	defer func() { require.NoError(t, unregister()) }()

	used, ok := tm.GaugeValue(t, telemetry.DBPoolConnections.Name, telemetry.AttrDBConnectionState.String("used"))
	require.True(t, ok)
	require.Equal(t, float64(1), used)
	_, ok = tm.GaugeValue(t, telemetry.DBPoolConnections.Name, telemetry.AttrDBConnectionState.String("idle"))
	require.True(t, ok)
	waits, ok := tm.GaugeValue(t, telemetry.DBPoolWaits.Name)
	require.True(t, ok)
	require.Equal(t, float64(0), waits)
	_, ok = tm.GaugeValue(t, telemetry.DBPoolWaitTime.Name)
	require.True(t, ok)
}
