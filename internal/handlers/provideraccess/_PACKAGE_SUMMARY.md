# Package: `internal/handlers/provideraccess`

## Role

Reports which model providers the requesting account can reach, and whether its keys come from the deployment or from the account.

## Responsibilities

- `GET /provider-access` returns `models.ProviderAccess`: `keys_from` (`deployment` or `account`, from the resolver's `AccountsSupplyKeys()`) and `providers`, those the account has a key for right now, in `models.ModelProviders` order.

## Dependencies

- **Inbound:** `internal/server` mounts it on the authenticated router with the server's resolver.
- **Outbound:** `internal/providerkeys`, `internal/models`, `internal/handlers/handlerutils`.

## Non-obvious decisions

- It asks the same resolver every provider request asks, so the answer matches what a request would carry, in either mode.
- It states the fact only; what to tell the user (add a key, which screen) is the frontend's.
- It names providers, never keys.

## Testing

- `handler_test.go` — deployment keys, account keys, and an empty list (not null) when the account has none; no key value in any response.

## Related documentation

- [`internal/providerkeys`](../../providerkeys/_PACKAGE_SUMMARY.md)
- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md)
