# Package: `internal/accountkeys`

## Role

The model-provider API keys accounts store themselves: storage, the routes that manage them, and the resolver that answers a request with the requesting account's key.
An account's key for a built-in provider is its built-in provider connection (ADR 0x027).

## Responsibilities

- **Storage:** `Set` validates the provider and the key (8 to 512 characters, no whitespace), encrypts it with the deployment's token key (`Sealer`, implemented by `datastore.Datastore`) and stores it on the account's built-in connection for that provider, creating the connection (named after the provider) or replacing its key.
  The connection storage itself is `internal/providerconnections`' `Store`; this package keeps the per-provider view of it.
  `List` returns only provider, last-four hint and update time, for built-in connections only; `Delete` removes one.
  Other connections (custom, local servers) sit in the same table and are not keys here.
- **Resolver:** `Key(ctx, provider)` implements `providerkeys.Resolver` with the key of the account on the context (`middleware.GetUserIDFromContext`).
  A context naming no account, or a key that cannot be read, gets no key.
  `Configured` is always true, because which accounts have keys is only known per request, and `AccountsSupplyKeys` is true.
- **Conversion:** `ConvertStoredKeys` runs at startup (`cmd/api-server`).
  It turns each row of the old `account_provider_keys` table into a built-in connection, copying the ciphertext without decrypting it, and removes the row.
  It is idempotent and safe on two instances at once: an existing connection keeps its key over an older row, and takes the row's key when the row is newer (changed by an instance still on the previous build, mid-upgrade).
  A key removed on such an instance leaves no row, so the previous build's instances are stopped before this one starts.
  Converted connections are marked `source: converted`, so the Providers screen can tell those accounts once.
  The conversion and the old table are removed together once the ADR 0x027 rollout has merged.
- **Routes:** `GET /provider-keys`, `PUT /provider-keys/{provider}` (`{"key": "...", "current_password": "..."}`) and `DELETE /provider-keys/{provider}`, on the authenticated router.
  `PUT` asks `reauth.Confirmer` first and answers 403 `reauthentication_required` when the owner did not confirm; `DELETE` needs no password, since removing a key only breaks things visibly.

## Dependencies

- **Inbound:** `internal/server` only: it builds `Keys`, hands it to `providerkeys.New`, and mounts the routes only when the chosen resolver says accounts supply keys (`mountAccountKeyRoutes`).
- **Outbound:** `internal/providerconnections` (built-in connections), `ent` (`ProviderConnection` and `AccountProviderKey`, the latter read only to convert it), `internal/reauth`, `internal/middleware`, `internal/handlers/handlerutils`, `internal/models`.

## Non-obvious decisions

- It is used only when `providerkeys.New` picks it; a deployment that supplies its own keys leaves the table empty and serves no key routes.
  The table carries a database comment saying so, for anyone reading the schema there.
- The only code that touches the `AccountProviderKey` schema.
- One built-in connection per provider per account is enforced by a unique index on `user_id` and `builtin`, a column set only for built-in providers.
  The deployment's own connections (no `user_id`) get a partial unique index on `builtin` where `user_id IS NULL`, since a unique index treats each NULL as distinct.
  A request to a built-in provider asks for "the account's key", which needs one answer until models point at connections.
- A key is write-only: no response, error or log line contains it.
  Storage errors are logged by `RespondWithError`, which never puts the error in the response.
- The table's one-way edge to `User` carries the foreign key and its delete cascade, so deleting an account deletes its keys without the `User` schema changing.
- No cache: each request reads the stored key.

## Testing

- `convert_test.go` — the conversion: ciphertext, hint and timestamps carried over, the `converted` source, old rows removed, running twice, an existing connection keeping its key over an older row and taking a newer row's (mutation-checked), and one deployment connection per built-in provider.
- `accountkeys_test.go` — over in-memory SQLite (tables written by hand; `TestProviderConnectionsSchema` checks the schema facts they rely on): sealed storage, replace, validation, per-account isolation, delete, that other connections are not keys, and the cascade (also checked against the generated migration); the resolver's per-account answers, an account recorded with `apicontext` alone, and no key for no account or an unreadable key; each route, that every route requires an account, that setting a key needs the owner's confirmation (missing or wrong: 403 and nothing stored; a failed check: 500), and that deleting one does not.

## Related documentation

- [`internal/providerkeys`](../providerkeys/_PACKAGE_SUMMARY.md) — how the resolver is chosen and applied.
- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md)
