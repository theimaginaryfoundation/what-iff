package email

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// opSendExportReady is the operation attribute for export-ready notifications.
const opSendExportReady = "send_export_ready"

// Instrument wraps s so each send records telemetry.DependencyDuration (through
// telemetry.Global()) with the given dependency (e.g. telemetry.DependencySES) and operation
// send_export_ready. Wrap only real transports: NoopSender makes no call worth timing.
func Instrument(s Sender, dependency string) Sender {
	if s == nil {
		return nil
	}
	return instrumentedSender{inner: s, dependency: dependency}
}

type instrumentedSender struct {
	inner      Sender
	dependency string
}

func (s instrumentedSender) SendExportReady(ctx context.Context, to string, data ExportReadyData) error {
	done := telemetry.Global().TimeDependency(ctx, s.dependency, opSendExportReady)
	err := s.inner.SendExportReady(ctx, to, data)
	done(err)
	return err
}
