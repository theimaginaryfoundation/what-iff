# Package: `internal/slackplugin`

## Role

Wires the Slack relay into the API server through the plugin seam: the `/api/slack` routes, the reply hook, and the gateway that holds each persona app's Socket Mode connection (ADR 0x032).
Linked by a blank import in `cmd/api-server/plugins.go`.

## Responsibilities

- **`init`:** registers the reply hook (which does nothing until the plugin is applied) and the plugin.
- **`apply` (`plugin.go`):** returns at once unless `slackrelay.Enabled()` (`SLACK_RELAY_ENABLED=true`), mounts the routes, builds the `slackrelay.Service` with the turn policy from the environment (`relaykit.PolicyFromEnv`), and starts a `relaykit.Supervisor` over `ListActiveSlackAppCredentials` (lock key `DefaultLockKey` = 80920035, `SLACK_GATEWAY_LOCK_KEY`, `SLACK_GATEWAY_SINGLE_INSTANCE`).
- **Gateway (`gateway.go`):** one `slackrelay.Socket` per active app; events go to the service off the read loop, and connection states are recorded on the app (`SetSlackAppState`: retrying errors keep it active with the error shown, a refused token marks it `invalid_token`).
- **`Handler` (`handlers.go`, `bindings.go`):** the manifest and its "create the app" link for a persona; apps (connect after checking both tokens are one app's: `auth.test`, `bots.info`, and the app id in a Socket Mode `hello`; replace tokens, refresh, pause, remove); channels (list, join a public one); bindings (create with a sandboxed relay thread or an acknowledged existing one, update, reactivate, remove); a thread's Slack state and the composer toggle.
  After an app changes it kicks the supervisor so the gateway reconciles at once.

## Key types and entry points

- `Handler`, `Store`, `Slack`, `AppResponse`, `ManifestResponse`, `ChatState`, `DefaultLockKey`.

## Dependencies

- **Inbound:** `internal/plugins.Apply` during server setup.
- **Outbound:** `internal/slackrelay`, `internal/relaykit`, `internal/datastore`, `internal/plugins`, `internal/replyhook`, `internal/handlers/handlerutils`, `internal/middleware`, `internal/models`, `github.com/gorilla/websocket`.

## Non-obvious decisions

- **The relay is one switch for the whole server.**
  With `SLACK_RELAY_ENABLED` off nothing is mounted, so every `/api/slack` route is a 404 and the web app reads that as off; there is no per-user gate (ADR 0x032 §7).
- **Two tokens, one app.**
  A bot token and an app-level token of different apps would hear one app and post as another, so connecting checks they match, and replacing either must keep the same app and workspace.
- **The fingerprint is a hash of the tokens**: a new token reconnects, a new binding does not (Slack sends an app every channel it is in), and no token sits in the supervisor's bookkeeping.
- **Ownership is the datastore's job**: every store call is owner-scoped, and another account's rows are a 404.

## Testing

- `handlers_test.go` uses a fake store and Slack: the manifest, token checks (wrong kinds, revoked, two apps), replacing tokens, sandboxed relay threads and the unrestricted acknowledgement, id cleaning, joining channels, ownership, resuming only undoing a pause, member-id cleaning, and the gateway's fingerprint.
