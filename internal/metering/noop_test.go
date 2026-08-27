package metering

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNoopMeterCheck(t *testing.T) {
	tests := []struct {
		name       string
		tier       string
		actionType string
	}{
		{name: "known tier and action", tier: "high", actionType: "chat"},
		{name: "empty tier is treated conservatively but still allowed", tier: "", actionType: "chat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NoopMeter{}
			got := m.Check(context.Background(), uuid.New(), tt.tier, tt.actionType)
			assert.Equal(t, Decision{Allowed: true, DontRecordUsage: true}, got)
		})
	}
}

func TestNoopMeterRecordWithNilLogger(t *testing.T) {
	m := NoopMeter{}
	// Must not panic when Logger is unset.
	assert.NotPanics(t, func() {
		m.Record(context.Background(), Decision{Allowed: true}, Usage{UserID: uuid.New()})
	})
}

func TestNoopMeterRecordLogsAtDebug(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	m := NoopMeter{Logger: zap.New(core)}

	userID := uuid.New()
	m.Record(context.Background(), Decision{}, Usage{
		UserID:     userID,
		ActionType: "chat",
		Model:      "test-model",
		Tokens:     42,
	})

	entries := logs.All()
	require.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, zap.DebugLevel, entry.Level)
	assert.Equal(t, "metering(noop): turn not tracked", entry.Message)

	fields := entry.ContextMap()
	assert.Equal(t, userID.String(), fields["user_id"])
	assert.Equal(t, "chat", fields["action_type"])
	assert.Equal(t, "test-model", fields["model"])
	assert.Equal(t, int64(42), fields["tokens"])
}
