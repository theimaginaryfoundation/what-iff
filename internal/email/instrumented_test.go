package email

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

type fakeSender struct {
	err   error
	calls int
}

func (f *fakeSender) SendExportReady(context.Context, string, ExportReadyData) error {
	f.calls++
	return f.err
}

func TestInstrumentRecordsSends(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	ok := &fakeSender{}
	failing := &fakeSender{err: errors.New("throttled")}

	require.NoError(t, Instrument(ok, telemetry.DependencySES).SendExportReady(context.Background(), "a@example.com", ExportReadyData{}))
	require.Error(t, Instrument(failing, telemetry.DependencySES).SendExportReady(context.Background(), "a@example.com", ExportReadyData{}))
	require.Equal(t, 1, ok.calls)
	require.Equal(t, 1, failing.calls)

	name := telemetry.DependencyDuration.Name
	require.Equal(t, uint64(2), tm.HistogramCount(t, name,
		telemetry.AttrDependency.String(telemetry.DependencySES), telemetry.AttrOperation.String("send_export_ready")))
	require.Equal(t, uint64(1), tm.HistogramCount(t, name,
		telemetry.AttrDependency.String(telemetry.DependencySES), telemetry.AttrErrorType.String(telemetry.ErrorTypeOther)))
	require.Nil(t, Instrument(nil, telemetry.DependencySES))
}
