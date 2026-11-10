// Package filechange announces changes to a user's files (ADR 0x043 §3): a content write, a
// rename, or a new file saved from the file workspace. The public build only records subscribers;
// the private build subscribes in an init() to push the change into the conversation's container.
//
// Notifications are best effort and synchronous in the caller's goroutine, so a subscriber that
// does slow work must hand it off. A subscriber never fails the write that caused it.
package filechange

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"go.uber.org/zap"
)

// Kind says what changed.
type Kind string

const (
	// KindContent is a content write: the row now points at a new object.
	KindContent Kind = "content"
	// KindRename is a display-name change; the object is unchanged.
	KindRename Kind = "rename"
	// KindCreate is a new file created from the workspace (Save As) or an upload.
	KindCreate Kind = "create"
)

// Event is one change to one file. File is the row after the change. OldKey and OldName are the
// values before it, empty when they did not change. ChatID is set when the change was made from a
// conversation (a chat upload, or a save from the chat-hosted workspace).
type Event struct {
	Kind    Kind
	UserID  uuid.UUID
	File    *models.FileAttachment
	OldKey  string
	OldName string
	ChatID  *uuid.UUID
}

// Subscriber receives events. It must not block for long and must not return the error to the
// user: a failure to propagate is the subscriber's to log and repair later.
type Subscriber func(ctx context.Context, ev Event)

var (
	mu   sync.RWMutex
	subs []Subscriber
)

// Subscribe registers fn for every later event.
func Subscribe(fn Subscriber) {
	if fn == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	subs = append(subs, fn)
}

// Notify sends ev to every subscriber, in registration order. A nil File is ignored.
func Notify(ctx context.Context, ev Event) {
	if ev.File == nil {
		return
	}
	mu.RLock()
	fns := append([]Subscriber(nil), subs...)
	mu.RUnlock()
	for _, fn := range fns {
		call(ctx, fn, ev)
	}
}

// call runs one subscriber; a panic in it must not fail the write that raised the event, or the
// subscribers after it.
func call(ctx context.Context, fn Subscriber, ev Event) {
	defer func() {
		if r := recover(); r != nil {
			zap.L().Error("filechange: subscriber panicked", zap.Any("panic", r), zap.String("kind", string(ev.Kind)))
		}
	}()
	fn(ctx, ev)
}

// Reset drops every subscriber. For tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	subs = nil
}
