// Package email delivers account-export "your export is ready" notifications.
//
// This package is transport-agnostic: it defines the Sender interface plus a Noop
// implementation for the open-source/local build, where the export bundle is written
// to the configured file store and its link is logged rather than emailed. A real
// transport (e.g. Amazon SES) is provided privately by registering a constructor into
// the New factory var below via an init(), so linking that implementation swaps the
// concrete Sender with no changes to the account-export handler or server wiring.
package email

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// ExportReadyData is the template data for the "your export is ready" email.
type ExportReadyData struct {
	Username   string
	BundleURL  string
	ExpiresAt  time.Time
	FilesCount int
}

// Sender delivers export notifications. Implementations: a private transport (e.g. SES)
// for hosted deployments, and NoopSender for the open-source/local build.
type Sender interface {
	SendExportReady(ctx context.Context, to string, data ExportReadyData) error
}

// New constructs the production Sender. It is nil in builds that do not link an email
// implementation (e.g. the open-source build), where callers fall back to NoopSender —
// the emailed link is a hosted-deployment feature, not a requirement to run the app.
// The implementation owns all of its own configuration (verified From identity, region),
// reading it from the environment at construction time, and returns nil when it is
// unconfigured or fails to initialize. The private implementation sets this in its init().
var New func(logger *zap.Logger) Sender

// NoopSender logs instead of sending — used when no email transport is linked/configured
// (open-source and local dev). The export bundle still lands in the file store; the logged
// link is how the operator retrieves it.
type NoopSender struct{ Logger *zap.Logger }

func (n NoopSender) SendExportReady(_ context.Context, to string, data ExportReadyData) error {
	if n.Logger != nil {
		n.Logger.Info("export-ready email (noop; no email transport configured)",
			zap.String("to", to), zap.String("bundle_url", data.BundleURL))
	}
	return nil
}
