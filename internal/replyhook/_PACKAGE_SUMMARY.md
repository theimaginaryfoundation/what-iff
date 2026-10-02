# Package: `internal/replyhook`

## Role

Extension seam for features that react to a **finished assistant reply**, whichever path produced it (interactive turn, webhook user or background mode, scheduled agent job).
The core registers nothing; with no hook linked, firing is a no-op that starts no goroutine.

## Responsibilities

- **`Register(Hook)`:** called from a feature package's `init()`; linking that package (a blank import in `cmd/api-server`) activates it.
  Returns an unregister function, which exists for tests in other packages.
- **`Fire(ctx, logger, Event)`:** runs every hook on its own detached goroutine with panic recovery, so a slow or faulty hook never delays, fails or crashes a turn.
- **`Event`:** pointers only (`UserID`, `ChatID`, `MessageID`, optional `TriggerMessageID` and `JobID`, `CallPath`).
  A hook that needs the reply text reads it through the datastore as the owner.

## Dependencies

- **Inbound:** `internal/agent` fires it (`fireReplyHook` in `reply_hook.go`) at the end of `runUserChatPostInferencePhases` and `handleEphemeralPrompt`, after the job reaches `complete`.
- **Outbound:** `internal/telemetry` (for `CallPath`), `zap`.

## Non-obvious decisions

- **Safe for concurrent use.**
  The hook list is behind an RWMutex and `Fire` iterates a snapshot, so registering (normally at init, but tests do it at runtime) never races a firing reply.
- **Fired on success only.** A failed or cancelled turn fires nothing; a hook that cares about failures can watch the job it was given.
- **`TriggerMessageID` is nil for ephemeral prompts** (agent jobs, webhook background mode), which save no user message; `JobID` is nil for scheduled runs, which create no tracking job.
- **`pushnotify` is separate and older.** It keeps its own call site, limited to agent-job replies, and can move onto this hook later.
