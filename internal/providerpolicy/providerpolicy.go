// Package providerpolicy is the boundary between the core and the question of
// where model-provider API keys come from.
//
// The two distributions answer it differently, and the answer is not a detail:
// it decides whether an account may store a key at all, whether the app may
// spend a key the account did not supply, and whose bill the interface should
// describe. Every one of those is a behaviour difference, so it lives behind
// one interface rather than being decided again at each call site.
//
// In builds that link no implementation — the open-source distribution —
// accounts supply their own keys. That is the correct default for someone
// running this themselves, since the person operating it is the person paying.
// A build where the operator supplies the keys registers a Policy saying so;
// linking that implementation is all that changes the behaviour.
package providerpolicy

// Policy decides where provider API keys come from.
// Implementations must be safe for concurrent use.
type Policy interface {
	// AccountsSupplyKeys reports whether an individual account may store and
	// use its own provider credentials.
	//
	// False means the operator's credentials are the only ones: accounts cannot
	// store keys, and nothing the account does may spend a credential it did
	// not supply.
	AccountsSupplyKeys() bool
}

// Active is the linked Policy. It is nil in builds that register none, in which
// case accounts supply their own keys (see the package doc). Server setup sets
// it from New when an implementation is linked.
var Active Policy

// New constructs the production Policy. It is nil in builds that link no
// implementation; an implementation sets it in its init(). Server setup calls
// it (when non-nil) and assigns the result to Active.
var New func() Policy

// AccountsSupplyKeys reports whether accounts supply their own provider keys.
// True when no Policy is linked, so the open-source build lets the person
// running it enter their own credentials without any binding.
func AccountsSupplyKeys() bool {
	if Active == nil {
		return true
	}
	return Active.AccountsSupplyKeys()
}
