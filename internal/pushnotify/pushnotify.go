// Package pushnotify defines the boundary between the core agent and any
// push-notification implementation. The agent depends only on the Notifier
// interface and the plain-data Event type declared here — never on a concrete
// sender or its transport (FCM, APNs, web push, e-mail, ...).
//
// Two implementations satisfy Notifier:
//
//   - NoopNotifier (this package): sends nothing. It is the default for builds
//     that do not link a push implementation (e.g. the open-source
//     distribution).
//   - an external implementation, linked in privately: it registers its
//     constructor into the New factory var below via an init(), so linking that
//     package (a blank import in main) is all that swaps a real sender in.
//     Removing the package — and its blank import — leaves the core compiling
//     against NoopNotifier with no edits to the agent or server wiring.
//
// This is a supported extension seam: an operator who wants completed replies to
// reach users out-of-band writes a package that assigns New in its init().
package pushnotify

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

// Event describes one completed assistant reply for Notifier.Notify. It carries
// only facts the agent legitimately owns; the implementation decides recipients
// (the user's registered devices), formatting, and transport.
type Event struct {
	// UserID is the recipient: the owner of the chat/job that just completed.
	UserID uuid.UUID
	// ChatID and MessageID identify the reply so a notification can deep-link to
	// it on tap.
	ChatID    uuid.UUID
	MessageID uuid.UUID
	// Body is the assistant reply text (unbounded); the implementation truncates
	// it to a notification-sized preview.
	Body string
}

// Notifier delivers a push for one completed reply.
//
// Notify is fire-and-forget: it must not block the caller on network I/O
// (implementations fan out to the user's devices on their own goroutine) and
// must swallow its own errors. Implementations must be safe for concurrent use;
// Notify is called from per-job goroutines.
type Notifier interface {
	Notify(ctx context.Context, ev Event)
}

// NoopNotifier sends nothing. It is the default when no push implementation is
// linked (the open-source build), so the completion path can call the notifier
// unconditionally without a nil check.
type NoopNotifier struct{}

// Notify does nothing.
func (NoopNotifier) Notify(context.Context, Event) {}

// New constructs the production Notifier. It is nil in builds that do not link a
// push implementation; NewAgent falls back to NoopNotifier in that case (see the
// NoopNotifier doc). The private implementation sets this in its init().
//
// The implementation owns all of its own configuration, reading whatever
// credentials it needs at construction time. The core passes only the datastore
// (to look up a user's device tokens) and logger, so no implementation settings
// cross this boundary.
var New func(ds *datastore.Datastore, logger *zap.Logger) Notifier
