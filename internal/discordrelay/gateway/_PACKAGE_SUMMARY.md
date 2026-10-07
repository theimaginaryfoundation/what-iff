# Package: `internal/discordrelay/gateway`

## Role

Holds one Discord Gateway (websocket) connection per active bot, on exactly one process at a time.

## Responsibilities

- **`Supervisor.Run`:** while it holds the leader lock, reconciles the open connections with `BotSource.ActiveBots` about every 30 s (open new bots, reopen on a new token or intents, close removed ones); followers retry the lock about every 15 s.
  On shutdown or a lost lock it closes every connection and releases the lock.
- **`DiscordgoDialer`:** opens a discordgo session with the guild and guild-message intents (plus Message Content when the application has it) and converts each `MESSAGE_CREATE` with `ToInbound`, resolving a Discord thread to its parent channel.
- **Lockers:** `AdvisoryLocker` (the datastore's Postgres advisory lock, key `DefaultLockKey` = 80920032) and `SingleInstanceLocker` (always leads; one process only).

## Dependencies

- **Inbound:** `internal/discordplugin`.
- **Outbound:** `internal/discordrelay` (`InboundMessage`), `internal/datastore` (`TryAcquireSchedulerLeaderLock`), `github.com/bwmarrin/discordgo`, `zap`.

## Non-obvious decisions

- **Nothing here depends on running in the API server.**
  `Run` takes a locker, a bot source and a sink, so the gateway can move to its own process without changes.
- **Handover overlap is harmless** because inbound messages are deduplicated on the Discord message id in storage.

## Testing

- `supervisor_test.go` uses fake lockers, sources and dialers to cover leading, reconciling, reconnecting on a changed fingerprint, and stepping down.
