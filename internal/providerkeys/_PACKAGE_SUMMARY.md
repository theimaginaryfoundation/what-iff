# Package: `internal/providerkeys`

## Role

Answers which model-provider credential a request uses, in one place.

## Responsibilities

- **`Resolver`:** `Key(ctx, provider)` returns the credential for a request, or `""` when there is none.
  `Configured(provider)` answers the startup question of whether a client for that provider is worth building.
  `AccountsSupplyKeys()` reports whether keys come from accounts; it is part of the interface so a wrapping resolver has to answer it.
- **`DeploymentResolver`:** answers from the keys the deployment was configured with, the same for every caller; blank values count as unconfigured.
- **`NotConfiguredError`:** returned by the credential transport, in place of sending, when a request's provider has no key; it names the provider.
  `HTTPStatusCode()` is 401, so error classification treats it as an authentication failure and the app does not retry it as a dropped connection.
  `Message()` states it in words ("No OpenAI API key is set."), and `FailureText(err)` is what jobs and chat turns record: that message for a missing key, `err.Error()` otherwise.
- **`New`:** chooses the resolver the server uses, given the deployment resolver and the account resolver (`internal/accountkeys`).
  Its default is `AccountKeys`: each account brings its own keys, and the deployment's configured keys are not used.
  A build that always supplies keys itself sets it in an `init()`, which is why tests check `AccountKeys` rather than `New`.
  The server mounts the account key routes only when the chosen resolver's `AccountsSupplyKeys()` is true.

## Dependencies

- **Inbound:** `internal/server` (builds the resolver and the credential transport), `internal/agent` (`AgentConfig.KeyResolver`).
- **Outbound:** `internal/models` only.

## Non-obvious decisions

- An empty `Key` result is not an error: callers report the provider as unconfigured rather than substituting another credential.
- Keys are applied at the shared HTTP transport (`provider.CredentialHTTPClient`) rather than at client construction, because the SDK client value is copied into several holders and has no single field to reassign.
- Work that runs after its request has ended must put the owning account on its context, or the account resolver gives it no key.
  File chunking (`handlerutils.TriggerAsyncFileChunking`) and account import (`accountexport`) do.
  The startup memory-embedding backfill pages across every account at once and has no single owner.

## Testing

- `providerkeys_test.go` — configured and blank values, that the caller does not change the answer, `FailureText` through wrapping, and the 401 classification.
- `AccountKeys` answers with the account's key and ignores the deployment's (also in `providerkeys_test.go`); the account resolver is tested in `internal/accountkeys`.
- The transport itself is tested in `internal/agent/provider/credentials_test.go`.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md)
