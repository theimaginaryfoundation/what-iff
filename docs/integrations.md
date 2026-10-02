# Integrating with What Iff

How an outside program (a bridge, an automation, another project) talks to a What Iff account, which API to use, and what to expect from it.
The rationale for the webhook surface is in [ADR 0x023](adr/0x023-webhook-read-access.md); the contract is in [`openapi.yaml`](../openapi.yaml).

## Two surfaces

| | Session API | Webhook surface |
|---|---|---|
| **For** | First-party clients (the web app) | Non-interactive, server-to-server integrations |
| **Credential** | An account session (JWT), obtained by signing in | A static `wht_` token created under Integrations, sent as `Authorization: Bearer wht_...` |
| **Paths** | `/api/...` | `/api/webhooks/...` |
| **Reach** | The whole product | A deliberately small set of operations, each behind a scope |
| **Response shapes** | Full internal shapes | Allow-listed subsets that never include private working state |

A webhook token acts as the account that created it, limited to the scopes it was created with.
Integrations should use the webhook surface and never hold an account credential.

## Capability inventory

The spec has 132 operations on 100 paths.
By area (`tags` in the spec):

| Area | Operations | On the webhook surface |
|---|---:|---|
| Personality Management | 17 | List (names only) |
| Chat Management | 17 | List threads |
| Memory Management | 15 | No |
| MCP Integrations | 13 | No |
| Chat Messages | 9 | List, get one |
| User Management | 7 | No |
| AgentJob Management | 7 | No |
| Ritual Management | 6 | No |
| Webhooks | 6 | (the surface itself) |
| Admin | 5 | No |
| Billing | 5 | No |
| Job Management | 5 | Get one |
| Account Export | 5 | No |
| File Attachments | 5 | No (metadata is inside messages) |
| Authentication | 3 | No |
| Webhook Tokens | 3 | No (session-managed) |
| System | 2 | No |
| Model Management | 1 | No |
| Search | 1 | No |

### The webhook surface today

| Operation | Scope | Purpose |
|---|---|---|
| `POST /webhooks/chat/{chatId}/messages` | `messages:write` | Post a message (`user`, `assistant` or `background` mode) |
| `GET /webhooks/chat` | `chat:read` | List threads; `personality_id` filters by persona |
| `GET /webhooks/personality` | `chat:read` | List personas (id, name, cover) to choose a filter from |
| `GET /webhooks/chat/{chatId}/messages` | `chat:read` | Read a thread, newest first, paged or by cursor |
| `GET /webhooks/chat/chat-message/{id}` | `chat:read` | Read one message |
| `GET /webhooks/job/{id}` | `chat:read` | Poll a background job |

### Where new operations belong

An operation goes on the webhook surface only if all of these hold:

1. A non-interactive integration genuinely needs it.
2. It has its own scope, off by default for existing tokens.
3. It returns an allow-listed shape (see `internal/models/webhook_message.go`), not an internal struct.
4. It never exposes private working state: system prompts, scratchpads, memories, credentials, or other accounts' data.

Everything that manages the account (creating or editing personas, memories, connectors, billing, export) stays on the session API.

## Recommended integration paths

**Send, then poll, then read.**
A send that runs the agent (`user` or `background` mode) is asynchronous; there is no synchronous reply.

1. `POST /webhooks/chat/{chatId}/messages` with `{"mode":"user","message":"..."}`. The response (202) has the message `id` and a `job_id`.
2. `GET /webhooks/job/{job_id}` until `result_id` is set, or `status` is `failed` or `cancelled`. `result_id` is the id of the reply and appears as soon as `status` reaches `inference_complete`; `complete` means post-processing has finished as well. On failure, `error` says why.
3. `GET /webhooks/chat/chat-message/{result_id}` for the reply.

To match a reply to its prompt by time instead, read `sent_at` from `GET /webhooks/chat/chat-message/{id}` for the id the POST returned.
`mode: "background"` triggers a run without saving a user message; `mode: "assistant"` writes an assistant message and runs nothing (`generation_model` is `none`, `generation_personality` is `webhook`).

**Walk a thread's history.**
`GET /webhooks/chat/{chatId}/messages?limit=100` returns the newest page and a `next_cursor`.
Pass it back as `cursor` for the messages strictly older than that page.
Prefer the cursor to `page`: it stays correct while new messages arrive, which page numbers do not.
An empty page has no `next_cursor`.

**Find the threads of a persona.**
`GET /webhooks/personality` to choose one, then `GET /webhooks/chat?personality_id=<id>`.
Threads are created and archived over time, so look them up by persona when needed instead of hard-coding thread ids.

**Bridges (Slack, Discord and similar).**
The bridge holds one token with only the scopes it needs, keeps its own mapping from an external channel to a What Iff chat id, posts inbound messages, and polls for replies.
One token per bridge lets you revoke it alone.
First-party clients use the session API instead.

## What to expect from the contract

| Topic | Status | Detail |
|---|---|---|
| Authentication | Implemented | `Authorization: Bearer wht_...`; revoked or unknown tokens are 401. |
| Authorization | Implemented | Scopes `messages:write` and `chat:read`; a missing scope is 403, checked before any lookup. Tokens from before scopes are write-only. Reads cover the token owner's data only. |
| Unavailable, other people's, deleted | Implemented | A chat, message or job that is not yours and one that does not exist are both 404 with the same wording. There is no soft-delete or redaction state: a deleted thread is a 404. |
| Errors | Implemented | JSON body with an `error` message. 400 for malformed input (filters are rejected, never ignored), 401, 403, 404, 500. |
| Pagination and ordering | Implemented | Messages newest first, ordered by `sent_at` then `id`. `limit` defaults to 20, clamped to 100. Offset (`page`) or keyset (`cursor`). |
| Consistency | Implemented | Live reads, `Cache-Control: no-store`. A message is readable once it is stored; a reply is readable once the job reports its `result_id`. |
| Provenance | Partly | `origin`, `generation_model`, `generation_personality`. Webhook-written assistant messages are marked `none` / `webhook`. A user message posted by webhook looks the same as one typed in the app. |
| Read and delivery state | Partly | `read_status` per message; job `status` for processing. No delivery receipts. |
| Audit | Partly | Each read logs the operation and token id (never content). No per-token audit trail in the product UI; `last_used_at` is shown per token. |
| Retries | Guidance | `GET` is safe to retry. `POST` is not idempotent. |
| Idempotency | Tracked | No idempotency key on `POST`; a retried `POST` can create a duplicate message. |
| Rate limits | Tracked | None in the application layer. The page-size cap bounds the cost of one request, not the request rate. Poll job status at a sensible interval (a second or more). |
| Push / callbacks | Not available | What Iff does not call out to integrations. Poll. |

## Follow-ups

Tracked in ADR 0x023: per-chat token scoping; an ascending "messages since" read for cheaper polling; rate limiting; an idempotency key on `POST`; marking webhook-posted user messages; attachment downloads; outbound event delivery; message search.
