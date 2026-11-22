# Package: `internal/mcpoauth`

## Role

OAuth for remote MCP connectors: starting sign-in, the callback's code exchange, refreshing tokens (`Sweeper`), and, since issue #517, reading a server's sign-in settings from the server itself and getting a client for it.

## Responsibilities

- **`service.go`:** `StartAuth` builds the authorization URL (PKCE S256, `resource` for a discovered connector); `HandleCallback` checks `iss` against a discovered connector's issuer (RFC 9207), exchanges the code and stores the tokens; `RefreshDueConnectors` refreshes tokens before they expire.
- **`discovery.go`:** `Discover` follows the MCP authorization spec: an unauthenticated `initialize` for the 401 `WWW-Authenticate` challenge (`resource_metadata`, `scope`), Protected Resource Metadata (RFC 9728) from the challenge or the well-known addresses (path-inserted, then root), then the authorization server's metadata (RFC 8414, then OpenID Connect, in the spec's order).
  A server without Protected Resource Metadata is read as its own authorization server (the spec's first version).
- **`register.go`:** `Register` gets a client in the spec's order: a Client ID Metadata Document (this deployment's document URL as the client ID, served by the handler) where the server takes one, else Dynamic Client Registration (RFC 7591).
  `AutoConfigure` = discover + register (or the person's own client ID); `AutoConfig.Apply` writes the settings onto a connector.

## Dependencies

- **Inbound:** `internal/server` (wiring), `internal/handlers/mcpserver`.
- **Outbound:** `internal/datastore` (via `Store`), `internal/models`, `internal/netguard`, `internal/agent/mcpclient` (protocol version only).

## Non-obvious decisions

- **Every discovery and registration request goes through `discoveryClient`**, never the credential transport: public addresses only (`netguard.PublicOnlyDialer`, checked at dial time so DNS rebinding can't reach internal hosts), https only (plain http only to loopback, and only with `MCP_ALLOW_LOCALHOST_CONNECTIONS`), short timeouts, 256 KB bodies, at most 3 redirects.
  The addresses come from what a person typed and what remote documents name.
- **Refused, per the spec:** authorization server metadata whose `issuer` isn't exactly the issuer asked for; a server whose metadata doesn't list `S256` in `code_challenge_methods_supported`; Protected Resource Metadata whose `resource` is another origin (or not a parent path of the server URL).
- **Scopes:** the challenge's `scope` wins over `scopes_supported`; `offline_access` is added only where the authorization server lists it (so refresh tokens can be issued).
- **`resource` (RFC 8707)** is sent on authorize, code exchange and refresh only for discovered connectors; connectors whose settings were typed in are unchanged (some non-MCP providers reject an unknown `resource`).
- **The metadata document needs a public https URL:** `Config.ClientMetadataURL`, else beside an https callback (`.../oauth/callback` → `.../oauth/client-metadata.json`).
  With an http (localhost) callback there is none and dynamic registration is used.
- Tokens belong to one client at one issuer: the datastore clears them when a connector's client ID, issuer or token URL changes.

## Testing

- `service_test.go` (start, callback, refresh); `discovery_test.go` (discovery order, issuer/PKCE/resource checks, legacy servers, internal-address refusal, challenge parsing, registration, `resource` and `iss` handling) against `httptest` servers on loopback.

## Related documentation

- MCP authorization spec (2026-07-28): authorization server discovery, client registration, security considerations. Issue #517.
