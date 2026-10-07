# Package: `internal/discordplugin`

## Role

Wires the Discord relay into the API server through the plugin seam: the `/api/discord` routes, the reply hook, and the gateway supervisor (ADR 0x025).
Linked by a blank import in `cmd/api-server/plugins.go`.

## Responsibilities

- **`init`:** registers the reply hook (which does nothing until the plugin is applied) and the plugin.
- **`apply`:** reads `DISCORD_RELAY_ENABLED` (off for `false`, `0` or `off`; read here rather than at init so a value from `.env` counts), mounts the routes on the authenticated router, builds the `discordrelay.Service`, and starts the gateway supervisor on the server's lifecycle context.
- **`Handler` (`handlers.go`):** bots (add with token validation, update, pause, remove, sync profile, list servers and channels), bindings (create, update, remove), a thread's Discord state, and the composer toggle (`/chats/{chatId}/pending/{bindingId}`).

## Key types and entry points

- `Handler`, `Store` (the storage the routes need), `BotResponse`, `ChatState`.

## Dependencies

- **Inbound:** `internal/plugins.Apply` during server setup.
- **Outbound:** `internal/discordrelay` and its `gateway`, `internal/datastore`, `internal/plugins`, `internal/replyhook`, `internal/handlers/handlerutils`, `internal/middleware`, `internal/storage`.

## Non-obvious decisions

- **A relay thread the server creates is sandboxed and quiet:** `ContextScope` sandbox, so `CreateChat` gives it the sandbox's default-off tools (`models.SandboxDefaultDisabledTools`), and no MCP connectors (it uses `CreateChat`, which attaches none).
  A refresh or a new token clears `invalid_token` but never resumes a bot the owner paused (`botStatus`).
  Binding an existing thread leaves its settings alone.
- **Threads that are not sandboxed need an acknowledgement** (`allow_unrestricted`) to be bound or repointed to, and it is stored only while the thread is not sandboxed (`bindableChat`, `discordrelay.RelayThreadOpenWithoutAcknowledgement`).
  Bindings report whether their thread is sandboxed right now (`chat_sandboxed`, computed on read).
- **A failed binding leaves nothing behind:** the relay thread created for it is deleted again, and a replaced or removed token's cached REST session is forgotten.
- **Tokens never leave the server.** Responses carry the bot without its token; `cleanToken` accepts a pasted `Bot ` prefix.
- **Ownership is the datastore's job.** Every store call is owner-scoped, and another account's bot, binding, thread or persona is a 404.

## Testing

- `handlers_test.go` and `handlers_unrestricted_test.go` use a stub store and a stub Discord client.
