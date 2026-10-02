// Package replyhook lets optional features react to a finished assistant reply,
// whichever path produced it: an interactive turn, a webhook-triggered turn, or
// a scheduled agent job. A feature that mirrors replies somewhere else (another
// chat service, an archive, an audit log) registers a Hook from an init() and is
// called once per completed reply.
//
// Like internal/plugins, the core registers nothing itself. With no hook linked,
// the default, Fire returns immediately without starting a goroutine.
//
// Events carry pointers only. A hook that needs the reply text reads it through
// the datastore as the owning user, so content never travels through this
// package and every read stays owner-scoped.
//
// pushnotify predates this seam and still has its own call site, limited to
// agent-job replies; it can move onto this hook later.
package replyhook

import (
	"context"
	"runtime/debug"
	"sync"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// Event describes one completed assistant reply.
type Event struct {
	// UserID owns the chat the reply was written in.
	UserID uuid.UUID
	// ChatID and MessageID identify the saved reply.
	ChatID    uuid.UUID
	MessageID uuid.UUID
	// TriggerMessageID is the saved user message the reply answers. It is nil for
	// ephemeral prompts (agent jobs, webhook background mode), which save none.
	TriggerMessageID *uuid.UUID
	// JobID is the tracking job for the turn, when it had one. Scheduled agent-job
	// runs do not create a tracking job.
	JobID *uuid.UUID
	// CallPath is the feature that produced the reply (telemetry.CallPathUserChat
	// for interactive and webhook user-mode turns, telemetry.CallPathAgentJob for
	// scheduled and webhook background-mode turns).
	CallPath telemetry.CallPath
}

// Hook reacts to one completed reply.
//
// Fire calls each hook on its own detached goroutine and recovers any panic, so
// a hook may block on I/O and a faulty one cannot stall or crash the turn. It
// should still bound its own work and handle its own errors. Hooks must be safe
// for concurrent use.
type Hook func(ctx context.Context, ev Event)

// registered wraps a hook so it can be found again by identity on removal.
type registered struct{ fn Hook }

// mu guards hooks. Registration normally happens during package init, but the
// list is locked anyway so Register, Enabled and Fire are safe whenever they
// run (tests register at runtime, and nothing stops a later caller doing so).
var (
	mu    sync.RWMutex
	hooks []*registered
)

// Register adds a hook. Call it from an init() in the feature's package; linking
// that package (a blank import in cmd/api-server) is what activates it. It is safe
// for concurrent use; a hook registered while a reply is firing is called from
// the next reply on.
//
// The returned function removes the hook again. Production code has no reason to
// call it; it lets tests in other packages register a hook without leaking it
// into the rest of the suite.
func Register(h Hook) (unregister func()) {
	r := &registered{fn: h}
	mu.Lock()
	hooks = append(hooks, r)
	mu.Unlock()
	return func() {
		mu.Lock()
		defer mu.Unlock()
		for i, x := range hooks {
			if x == r {
				hooks = append(hooks[:i:i], hooks[i+1:]...)
				return
			}
		}
	}
}

// Enabled reports whether any hook is registered.
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return len(hooks) > 0
}

// Fire hands ev to every registered hook on detached goroutines. ctx should
// outlive the request (the server's lifecycle context), since hooks run after
// the turn has returned.
func Fire(ctx context.Context, logger *zap.Logger, ev Event) {
	mu.RLock()
	snapshot := append([]*registered(nil), hooks...)
	mu.RUnlock()
	for _, reg := range snapshot {
		go func(h Hook) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("reply hook panicked",
						zap.Any("recover", r),
						zap.String("chat_id", ev.ChatID.String()),
						zap.String("message_id", ev.MessageID.String()),
						zap.ByteString("stack", debug.Stack()))
				}
			}()
			h(ctx, ev)
		}(reg.fn)
	}
}
