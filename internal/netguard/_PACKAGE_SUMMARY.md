# Package: `internal/netguard`

## Role

Keeps server-side requests to user-chosen addresses on the public internet.

## Responsibilities

- **`IsPublicIP`:** true only for a global unicast address that is not loopback, private, link-local (which includes the cloud metadata address), shared (100.64.0.0/10), multicast or unspecified.
- **`PublicOnlyControl` / `PublicOnlyDialer`:** a `net.Dialer` hook, and a dialer using it, that refuse any other address.

## Dependencies

- **Inbound:** `internal/discordrelay` (the Discord CDN fetcher), `internal/remotesync` (the remote server client).
- **Outbound:** Standard library only.

## Non-obvious decisions

- **Checked at connect time, not at URL-parse time.**
  The hook sees the resolved address of every connection, so a name that resolves to an internal address, or is rebound to one after a first check, is still refused.

## Testing

- `netguard_test.go` — public and non-public addresses, IPv4 and IPv6.
