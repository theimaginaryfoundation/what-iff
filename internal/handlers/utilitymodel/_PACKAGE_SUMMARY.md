# Package: `internal/handlers/utilitymodel`

## Role

Serves an account's choice of model for each utility role: the background work, such as memory extraction or chat naming, that isn't the assistant talking (ADR 0x027 section 6).

## Responsibilities

- `GET /utility-models` returns `models.UtilityModels`: every role in `models.UtilityRoles` order, with the account's choice, the deployment's, and the built-in model the role runs on when neither chose one.
- `PUT /utility-models/{role}` chooses the account's model for a role (`model_id`), and `DELETE /utility-models/{role}` clears it; both answer with every role after the change.

## Dependencies

- **Inbound:** `internal/server` mounts it on the authenticated router, with the datastore, `agent.UtilityBuiltInModel` and the key resolver.
- **Outbound:** `internal/datastore` (`ListUtilityModelSettings`, `SetUtilityModel`, `ClearUtilityModel`, `GetModel`, through the `Store` interface), `internal/providerkeys`, `internal/models`, `internal/handlers/handlerutils`.

## Non-obvious decisions

- Accounts choose only where they bring their own keys (`providerkeys` `AccountsSupplyKeys`), since then they pay for their background work; where the deployment supplies the keys, writes are a 403 and `accounts_choose` is false, and a choice stored before is not reported.
- It writes the account scope only.
  The deployment's choices are set elsewhere; this package only reports them.
- The datastore decides which models an account may choose (the catalog and its own connections' models); a refusal is a 400, "That model is not available".
- A catalog model whose provider the account holds no key for is refused as a 409 `provider_not_configured` naming the provider, since it would fail at every call; a connection model carries its connection's key.
  An experimental model the account hasn't opted into is a 403, and an account deleted while the request ran is a 401.
- An unset choice is `null`, not missing, so a client can tell "not chosen" from an older server.
- `built_in_model` is a name, not a model ID: a built-in model need not be a catalog row on every install.

## Testing

- `handler_test.go` — over a fake store: every role in order with both scopes, never another account's choice; setting and clearing; refusals (a model the account can't use, no model, an unknown role, no signed-in account) that change nothing; a deployment that supplies the keys refusing account writes; a catalog model without a key refused as `provider_not_configured`; a deleted account answered 401; and a model that can't be read answered 500 before anything is saved.

## Related documentation

- [`internal/agent`](../../agent/_PACKAGE_SUMMARY.md) — how a role's model is resolved and called.
- [`internal/datastore`](../../datastore/_PACKAGE_SUMMARY.md)
- [ADR 0x027](../../../docs/adr/0x027-provider-connections.md)
