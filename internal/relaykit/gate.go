package relaykit

import (
	"errors"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// RelayThreadOpenWithoutAcknowledgement reports whether a thread may be driven from another
// chat service without the owner's acknowledgement: only a sandboxed one. Anything else can
// read the owner's account (their name, memories, other conversations, files, scratchpad) on
// behalf of whoever may tag the persona.
func RelayThreadOpenWithoutAcknowledgement(chat *models.Chat) bool {
	return chat.IsSandboxed()
}

// Gate is one binding's bound thread, as ThreadOpen needs it, with the callbacks that record
// what it decides on the binding.
type Gate struct {
	// Chat is the bound thread as read just now, or ChatErr why it could not be read.
	Chat    *models.Chat
	ChatErr error
	// Acknowledged is the binding's stored "not sandboxed is fine" acknowledgement.
	Acknowledged bool
	// Broken is whether the binding is already marked broken; LastError its recorded error.
	Broken    bool
	LastError *string
	// Warning is recorded on the binding while tags are dropped because the thread is not
	// sandboxed; DeletedError when the thread no longer exists.
	Warning      string
	DeletedError string

	// MarkBroken marks the binding broken with a reason.
	MarkBroken func(reason string)
	// SetLastError records (or with "" clears) the binding's error, keeping its status.
	SetLastError func(msg string)
	// Withdraw clears the binding's acknowledgement.
	Withdraw func()
}

// ThreadOpen is the relays' fail-closed check that a bound thread may be driven from outside:
// it must be sandboxed, or the owner must have acknowledged the binding. The thread's CURRENT
// sandbox flag is used (it can be switched off in the app after binding), and any doubt
// (thread missing, lookup failing) means closed.
//
// It also keeps the binding's record honest: a deleted thread marks the binding broken; a
// sandboxed thread withdraws a stale acknowledgement, so switching the sandbox off later
// pauses the binding instead of silently re-opening it; and an unacknowledged thread that is
// not sandboxed records Warning (nothing is posted to the channel).
func ThreadOpen(g Gate) bool {
	if g.ChatErr != nil || g.Chat == nil {
		if errors.Is(g.ChatErr, datastore.ErrChatNotFound) && !g.Broken && g.MarkBroken != nil {
			g.MarkBroken(g.DeletedError)
		}
		return false
	}
	clearWarning := func() {
		if g.LastError != nil && *g.LastError == g.Warning && g.SetLastError != nil {
			g.SetLastError("")
		}
	}
	if RelayThreadOpenWithoutAcknowledgement(g.Chat) {
		clearWarning()
		if g.Acknowledged && g.Withdraw != nil {
			g.Withdraw()
		}
		return true
	}
	if g.Acknowledged {
		clearWarning()
		return true
	}
	if (g.LastError == nil || *g.LastError != g.Warning) && g.SetLastError != nil {
		g.SetLastError(g.Warning)
	}
	return false
}
