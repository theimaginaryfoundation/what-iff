// Package reauth answers whether a request has just proved it comes from the
// account's owner, for the few actions where holding a session should not be
// enough on its own.
//
// Changing an account's provider key is one. A stolen session is temporary, but
// a key swapped by whoever held it would keep sending the owner's conversations
// to someone else's provider account after the session was gone.
package reauth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotConfirmed is returned when the proof is missing or does not match.
var ErrNotConfirmed = errors.New("the account owner did not confirm this change")

// Confirmer checks proof that the requester is userID, right now.
type Confirmer interface {
	Confirm(ctx context.Context, userID uuid.UUID, proof string) error
}

// Passwords checks an account's current password.
type Passwords interface {
	CheckUserPassword(ctx context.Context, userID uuid.UUID, password string) (bool, error)
}

// New builds the Confirmer the server uses. The default takes the account's
// current password as proof. A build whose accounts sign in somewhere else
// sets New in an init() to confirm through that.
var New = func(passwords Passwords) Confirmer {
	return passwordConfirmer{passwords: passwords}
}

type passwordConfirmer struct {
	passwords Passwords
}

// Confirm accepts the account's current password. An empty proof is not
// checked at all.
func (c passwordConfirmer) Confirm(ctx context.Context, userID uuid.UUID, proof string) error {
	if proof == "" {
		return ErrNotConfirmed
	}
	ok, err := c.passwords.CheckUserPassword(ctx, userID, proof)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotConfirmed
	}
	return nil
}
