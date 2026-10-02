package telemetry

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

// initForTest runs Init and restores the global meter provider and Metrics afterwards.
func initForTest(t *testing.T) *Telemetry {
	t.Helper()
	prevProvider := otel.GetMeterProvider()
	prevGlobal := global.Load()
	telem, err := Init(context.Background(), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, telem.Shutdown(context.Background()))
		otel.SetMeterProvider(prevProvider)
		global.Store(prevGlobal)
	})
	return telem
}

func TestInit_NoExporterByDefault(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_METRICS_EXPORTER", "")

	telem := initForTest(t)
	require.NotNil(t, telem.Metrics)
	require.Same(t, telem.Metrics, Global())
	require.Nil(t, telem.meterProvider)
	require.Nil(t, telem.tracerProvider)
}

func TestInit_OTLPWithoutEndpointDisablesMetrics(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_METRICS_EXPORTER", "otlp")

	require.Nil(t, initForTest(t).meterProvider)
}

func TestInit_Console(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_METRICS_EXPORTER", "console")

	telem := initForTest(t)
	require.NotNil(t, telem.meterProvider)
	require.Nil(t, telem.tracerProvider)
}

func TestInit_PrometheusServesMetrics(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_METRICS_EXPORTER", "prometheus")
	port := freePort(t)
	t.Setenv("OTEL_EXPORTER_PROMETHEUS_PORT", strconv.Itoa(port))

	telem := initForTest(t)
	telem.Metrics.RecordDuration(context.Background(), JobDuration, 3*time.Second, AttrJobType.String("chat_message"))

	var body string
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://localhost:" + strconv.Itoa(port) + "/metrics")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body = string(b)
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)
	// Same naming as the deployed remote-write path: dots to underscores, unit suffix.
	require.Contains(t, body, `whatiff_job_duration_seconds_bucket{job_type="chat_message"`)
}

func TestMetricExportIntervalFromEnv(t *testing.T) {
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "")
	require.Equal(t, defaultMetricExportInterval, metricExportIntervalFromEnv(zap.NewNop()))
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "15000")
	require.Equal(t, 15*time.Second, metricExportIntervalFromEnv(zap.NewNop()))
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "soon")
	require.Equal(t, defaultMetricExportInterval, metricExportIntervalFromEnv(zap.NewNop()))
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
