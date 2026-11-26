# Package: `internal/netguard`

## Role

Keeps server-side requests to user-chosen addresses on the public internet.

## Responsibilities

- **`IsPublicIP`:** true only for a global unicast address that is not loopback, private, link-local (which includes the cloud metadata address), multicast, unspecified, or a special-use range: shared (100.64.0.0/10), "this network" (0.0.0.0/8), NAT64 and 6to4 (which embed an IPv4 address), benchmarking, documentation, and deprecated site-local.
- **`PublicOnlyControl` / `PublicOnlyDialer`:** a `net.Dialer` hook, and a dialer using it, that refuse any other address with an error wrapping `ErrNonPublicAddress`.
  Callers check `errors.Is(err, ErrNonPublicAddress)` to treat a refusal as permanent rather than retryable.
- **`PublicOrLoopbackControl` / `Control(allowLoopback)`:** the same hook that also allows loopback, for a local-development opt-in that should reach this machine and nothing else internal.
- **`NoAddressCheck`:** a hook that allows everything, so turning the guard off (a local-network opt-in) is explicit and greppable.
- **`NewHTTPClient(control, timeout)`:** the HTTP client for user-chosen addresses, with that hook on every connection (nil means `PublicOnlyControl`, so leaving it out fails closed), no proxy, same-origin redirects only (`SameOriginRedirects`), and otherwise a clone of `http.DefaultTransport`.

## Dependencies

- **Inbound:** `internal/relaykit` (`relaykit.PublicOnlyControl` wraps it with the caller's name, for the Discord CDN fetcher and the Buzz and Slack clients), `internal/remotesync` (the remote server client), `internal/agent/mcpclient` (its default client), `internal/mcpoauth` (its default client), `internal/server` (the shared MCP client for connector calls and OAuth token requests).
- **Outbound:** Standard library only.

## Non-obvious decisions

- **Checked at connect time, not at URL-parse time.**
  The hook sees the resolved address of every connection, so a name that resolves to an internal address, or is rebound to one after a first check, is still refused.
  It never sees a hostname: a dialer resolves first and calls the hook once per IP it tries, so public names work and need no lookup of their own here.
- **`IsPublicIP` only gets stricter.**
  It is the boundary for every caller at once, so a caller that needs an internal address uses an explicit opt-in at its call site (`PublicOrLoopbackControl`, `NoAddressCheck`) rather than widening it.

## Testing

- `client_test.go` — special-use ranges, the loopback opt-in, `NewHTTPClient` checking every connection, and same-origin redirects.
- `netguard_test.go` — public and non-public addresses, IPv4 and IPv6, and a hostname dial showing the hook sees resolved IP literals (a hostname that reaches the hook directly is refused).
