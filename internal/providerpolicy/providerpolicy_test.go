package providerpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type fixedPolicy struct{ accounts bool }

func (f fixedPolicy) AccountsSupplyKeys() bool { return f.accounts }

// The unlinked default is the open-source answer: the person running it is the
// person paying, so they enter their own credentials without binding anything.
func TestAccountsSupplyKeys_DefaultsToTrueWithNoPolicy(t *testing.T) {
	restore := Active
	t.Cleanup(func() { Active = restore })

	Active = nil
	require.True(t, AccountsSupplyKeys())
}

// A linked policy is authoritative in both directions, so a deployment whose
// operator supplies the keys is a binding rather than a fork.
func TestAccountsSupplyKeys_FollowsTheLinkedPolicy(t *testing.T) {
	restore := Active
	t.Cleanup(func() { Active = restore })

	Active = fixedPolicy{accounts: false}
	require.False(t, AccountsSupplyKeys())

	Active = fixedPolicy{accounts: true}
	require.True(t, AccountsSupplyKeys())
}
