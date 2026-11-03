package reauth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type passwords struct {
	want   string
	err    error
	called bool
}

func (p *passwords) CheckUserPassword(_ context.Context, _ uuid.UUID, password string) (bool, error) {
	p.called = true
	return password == p.want, p.err
}

func TestDefaultConfirmsWithTheCurrentPassword(t *testing.T) {
	p := &passwords{want: "correct horse"}
	c := New(p)
	ctx, user := context.Background(), uuid.New()

	require.NoError(t, c.Confirm(ctx, user, "correct horse"))
	require.ErrorIs(t, c.Confirm(ctx, user, "wrong"), ErrNotConfirmed)
}

// An empty proof is a refusal without a lookup.
func TestEmptyProofIsNotChecked(t *testing.T) {
	p := &passwords{want: ""}
	require.ErrorIs(t, New(p).Confirm(context.Background(), uuid.New(), ""), ErrNotConfirmed)
	require.False(t, p.called)
}

// A failure to check is reported as such, not as a wrong password.
func TestCheckFailureIsNotARejection(t *testing.T) {
	broken := errors.New("db down")
	err := New(&passwords{err: broken}).Confirm(context.Background(), uuid.New(), "anything")
	require.ErrorIs(t, err, broken)
	require.NotErrorIs(t, err, ErrNotConfirmed)
}
