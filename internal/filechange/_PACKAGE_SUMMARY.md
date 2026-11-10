# Package: `internal/filechange`

## Role

A process-wide seam that announces changes to a user's files (ADR 0x043 §9): a content write, a rename, or a file created from the file workspace.

## Responsibilities

- **`Event`** carries the kind of change, the owner, the row after the change, the previous object key and name when they changed, and the conversation the change was made from, if any.
- **`Subscribe`** registers a `Subscriber`; **`Notify`** calls every subscriber in order, synchronously, in the caller's goroutine.
- The public build has no subscribers.
  The private build subscribes in an `init()` to push the change into the conversation's live container (what-iff-private `docs/design/2026-11-06-file-workspace-c4a-sync.md`).

## Dependencies

- **Inbound:** `internal/filesave`, `internal/handlers/imagegallery` (rename), `internal/handlers/chat` and `internal/handlers/imagegallery` (uploads), `internal/agent/tools` (agent file writes).
- **Outbound:** `internal/models`.

## Non-obvious decisions

- A subscriber never fails the write that caused the event: propagation is best effort, and the container's next `shell_exec` repairs anything a push missed.
- Events are sent after the datastore write has committed, so a subscriber reading the row sees the new content.

## Testing

- `filechange_test.go` — ordering, nil subscribers, events without a file.
