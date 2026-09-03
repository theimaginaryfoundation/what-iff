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

// EventKind identifies the kind of action an Event describes. Today only agent
// replies are emitted; it exists so the same seam can carry other user-facing
// actions later without widening the interface.
type EventKind string

const (
	// EventKindAgentReply is a completed assistant reply from an autonomous or
	// webhook-triggered agent job.
	EventKindAgentReply EventKind = "agent_reply"
)

// Event describes a notable, user-facing action for Notifier.Notify. It carries
// only pointers the agent owns — never the message content itself: the recipient
// fetches the body through the API on tap, so reply text does not transit the
// notifier (nor any third-party push service). The implementation decides
// recipients (the user's registered devices), formatting, and transport.
type Event struct {
	// Kind is the action this event describes (e.g. EventKindAgentReply).
	Kind EventKind
	// UserID is the recipient: the owner of the chat/job that produced the action.
	UserID uuid.UUID
	// ChatID and MessageID identify the target, so a notification can deep-link to
	// it — and the client can fetch its content — on tap.
	ChatID    uuid.UUID
	MessageID uuid.UUID
}

// Notifier delivers a notification for one Event to a user's channels.
//
// The core calls Notify on a detached goroutine and recovers any panic, so an
// implementation may block on I/O and a faulty one cannot stall or crash the
// agent. It should still bound its own work (e.g. a context deadline) and
// swallow its own errors. Implementations must be safe for concurrent use.
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
