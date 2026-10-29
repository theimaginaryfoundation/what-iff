# Package: `internal/reauth`

## Role

Answers whether a request has just proved it comes from the account's owner, for the few actions where holding a session should not be enough.

## Responsibilities

- **`Confirmer`:** `Confirm(ctx, userID, proof)` returns nil when the proof shows the requester is the owner, `ErrNotConfirmed` when it is missing or wrong, and any other error when the check itself failed.
- **`New`:** builds the Confirmer the server uses.
  The default takes the account's current password as proof (`Passwords`, implemented by `datastore.CheckUserPassword`).
  A build whose accounts sign in somewhere else sets `New` in an `init()` to confirm through that.

## Dependencies

- **Inbound:** `internal/server` builds it; `internal/accountkeys` asks it before a provider key is set.
- **Outbound:** none beyond the `Passwords` interface.

## Non-obvious decisions

- Changing an account's provider key needs it: a stolen session is temporary, but a key swapped by whoever held it would keep sending the owner's conversations to someone else's provider account after the session was gone.
- An empty proof is refused without a lookup.
- A failed check is reported as an error, not as a wrong password, so callers do not tell the owner their password is wrong when the database is unavailable.
- Accounts whose password is managed by an identity provider have no local password, so the default never confirms them; such a build binds its own `New`.

## Testing

- `reauth_test.go` — the right password confirms and a wrong one does not, an empty proof is refused without a lookup, and a failed check is not a rejection.

## Related documentation

- [`internal/accountkeys`](../accountkeys/_PACKAGE_SUMMARY.md)
- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md)
