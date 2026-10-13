# Package: `internal/remotesyncplugin`

## Role

Wires remote sync into the API server through the plugin seam: the `/api/remote-sync` routes and the runner (ADR 0x026).
Linked by a blank import in `cmd/api-server/plugins.go`.

## Responsibilities

- **`init`:** registers the plugin.
- **`apply`:** does nothing unless `REMOTE_SYNC_ENABLED` is true (read here rather than at init so a value from `.env` counts).
  Otherwise it reads `REMOTE_SYNC_INTERVAL`, `REMOTE_SYNC_MAX_THREADS_PER_RUN` and `REMOTE_SYNC_ALLOW_LOCAL_NETWORK`, mounts the routes on the authenticated router, and starts the runner on the server's lifecycle context.
- **`Handler` (`handlers.go`):** `GET`, `PUT` and `DELETE /remote-sync`, and `POST /remote-sync/run`.

## Key types and entry points

- `Handler`, `Store`, `Starter`.

## Dependencies

- **Inbound:** `internal/plugins.Apply` during server setup.
- **Outbound:** `internal/remotesync`, `internal/datastore`, `internal/plugins`, `internal/handlers/handlerutils`, `internal/middleware`, `internal/models`.

## Non-obvious decisions

- **Off by default.**
  With it on, the server makes outbound requests to an address each user chooses; a self-hosted install wants that, a shared one should decide it on purpose.
  The open-source `docker-compose.yml` turns it on.
  When it is off the routes do not exist, which is how the UI knows.
- **A save proves the token first** (`Check`: the remote's persona list, one row), so a typo, a revoked token or a token without `chat:read` is reported on the form with the reason, not by a failed run later.
  Switching sync off skips the check.
- **Runs outlive requests.** Saves and "sync now" start the run on the server's lifecycle context, not the request's.
- **Tokens never leave the server.** Responses carry the last four characters only, and a pasted `Bearer ` prefix is stripped.

## Testing

- `handlers_test.go` uses a stub store, syncer and check: the token check and its error messages, input validation, switching off, run now and delete, and authentication.
