# Package: `internal/handlers/weave`

## Role

The weave API (ADR 0x030): owner-only routes that weave sandboxed threads together so each may read the others.

## Responsibilities

- **Routes (`/api/weaves`):** list and create (`{name, chat_ids}`), get and delete, and add or remove one thread (`/weaves/{id}/chats/{chatId}`).
- **Errors:** another account's weave or thread is a 404; a thread that cannot be woven (not sandboxed, another personality, already in a weave) is a 400 with the reason from `datastore.ErrWeaveChat`.

## Key types and entry points

- `Handler`, `NewHandler`, `Store`.

## Dependencies

- **Inbound:** `internal/server` mounts it on the authenticated router.
- **Outbound:** `internal/datastore` (weave methods and errors), `internal/handlers/handlerutils`, `internal/middleware`, `internal/models`.

## Non-obvious decisions

- **No agent tool weaves threads.**
  Membership is the owner's: in a sandboxed relay thread the agent is driven by strangers, who must not be able to weave their room with the owner's other threads.

## Testing

- `handler_test.go` covers creation and the mapping of storage errors to statuses with a fake store.
