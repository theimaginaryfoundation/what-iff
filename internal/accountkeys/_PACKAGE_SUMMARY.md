# Package: `internal/accountkeys`

## Role

The model-provider API keys accounts store themselves: storage, the routes that manage them, and the resolver that answers a request with the requesting account's key.

## Responsibilities

- **Storage:** `Set` validates the provider and the key (8 to 512 characters, no whitespace), encrypts it with the deployment's token key (`Sealer`, implemented by `datastore.Datastore`) and replaces any key already stored for that provider.
  `List` returns only provider, last-four hint and update time; `Delete` removes one.
- **Resolver:** `Key(ctx, provider)` implements `providerkeys.Resolver` with the key of the account on the context (`middleware.GetUserIDFromContext`).
  A context naming no account, or a key that cannot be read, gets no key.
  `Configured` is always true, because which accounts have keys is only known per request, and `AccountsSupplyKeys` is true.
- **Routes:** `GET /provider-keys`, `PUT /provider-keys/{provider}` (`{"key": "...", "current_password": "..."}`) and `DELETE /provider-keys/{provider}`, on the authenticated router.
  `PUT` asks `reauth.Confirmer` first and answers 403 `reauthentication_required` when the owner did not confirm; `DELETE` needs no password, since removing a key only breaks things visibly.

## Dependencies

- **Inbound:** `internal/server` only: it builds `Keys`, hands it to `providerkeys.New`, and mounts the routes only when the chosen resolver says accounts supply keys (`mountAccountKeyRoutes`).
- **Outbound:** `ent` (the `AccountProviderKey` schema, table `account_provider_keys`), `internal/reauth`, `internal/middleware`, `internal/handlers/handlerutils`, `internal/models`.

## Non-obvious decisions

- It is used only when `providerkeys.New` picks it; a deployment that supplies its own keys leaves the table empty and serves no key routes.
  The table carries a database comment saying so, for anyone reading the schema there.
- The only code that touches the `AccountProviderKey` schema.
- A key is write-only: no response, error or log line contains it.
  Storage errors are logged by `RespondWithError`, which never puts the error in the response.
- The table's one-way edge to `User` carries the foreign key and its delete cascade, so deleting an account deletes its keys without the `User` schema changing.
- No cache: each request reads the stored key.

## Testing

- `accountkeys_test.go` — over in-memory SQLite: sealed storage, replace, validation, per-account isolation, delete, and the cascade (also checked against the generated migration); the resolver's per-account answers, an account recorded with `apicontext` alone, and no key for no account or an unreadable key; each route, that every route requires an account, that setting a key needs the owner's confirmation (missing or wrong: 403 and nothing stored; a failed check: 500), and that deleting one does not.

## Related documentation

- [`internal/providerkeys`](../providerkeys/_PACKAGE_SUMMARY.md) — how the resolver is chosen and applied.
- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md)
