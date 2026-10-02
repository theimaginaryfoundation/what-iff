# ADR 0x023: Webhook read access and token scopes

- **Status:** Proposed
- **Date:** 2026-09-30
- **Deciders:** What Iff maintainers
- **Issue:** [#174](https://github.com/theimaginaryfoundation/what-iff/issues/174)

## Context

The webhook surface could only send: `POST /api/webhooks/chat/{chatId}/messages` on a static `wht_` token.
Every other method under `/api/webhooks` was a 404.
Integrations that send on that token (bridges, automations, other projects that talk to a What Iff thread) had no way to read a reply, or to learn that the job producing it had finished.
The only way to read was the session API, which needs an account credential (a Cognito/JWT session) that a server-to-server integration should not hold.

Webhook tokens were also all-or-nothing.
A token identified a user and carried that user's permissions, with no record of what it was *made* for.
That was tolerable while the only route was a write.
Adding reads to the same token would quietly give every existing "post only" token read access to its owner's whole history, including tokens already sitting in third-party config.

Chris asked for the chat-focused reads on the webhook path and noted that "chats by persona" does not need a new route shape: the thread list already filters by persona, so what is missing is a way to list personas to choose a filter from.

## Decision

### 1. Reads live on the webhook prefix, addressed the way the session API addresses them

All `GET`, all behind the same static token:

| Route | What it returns |
|---|---|
| `GET /webhooks/chat` | The owner's threads. `personality_id` filters by persona; also `search`, `tag`, `name`, `is_favorite`, `archived`, `min_date`, `max_date`. |
| `GET /webhooks/personality` | The owner's personas as `id`, `name`, `accent_color`, `cover_image_url` and timestamps only. |
| `GET /webhooks/chat/{chatId}/messages` | One thread's messages, newest first. `page`/`limit`, or `cursor` to walk back. `origin`, `min_date`, `max_date` filter. |
| `GET /webhooks/chat/chat-message/{id}` | One message. |
| `GET /webhooks/job/{id}` | A background job's state: `status`, `result_id`, `error`. |

They call the same datastore methods the session handlers call, so ownership is enforced in the query, not re-implemented.
`POST /webhooks/chat/{chatId}/messages` is unchanged.

### 2. Tokens carry scopes, and reads have to be asked for

- `messages:write` allows the existing POST.
- `chat:read` allows the five routes above.
- A token holds a set of scopes chosen at creation (`POST /webhook-tokens {"name", "scopes"}`). Omitting `scopes` gives `["messages:write"]`.
- **Tokens created before this change have no scopes recorded and are treated as `messages:write` only.** Deploying this gives no existing token read access. A token that should read has to be created with `chat:read` (the Integrations page has a checkbox for it).
- The scope is checked by middleware before the handler runs, so a token without it learns nothing about what exists. The 403 names the missing scope.
- Unknown scopes are rejected at creation (400). The scope middleware fails closed if the auth middleware was forgotten.

This costs one nullable JSON column (`webhook_tokens.scopes`), additive and needing no data migration.

### 3. Responses are an explicit subset, not the internal structs

- **Messages** (`WebhookMessage`) keep `ChatMessage`'s field names for the fields they carry (`id`, `chat_id`, `message`, `origin`, `read_status`, `sent_at`, `generation_*`, `last_error_message`, `attachments`), so a client written against the session API reads them unchanged. They leave out tool calls, model reasoning, the context breakdown, per-message portrait thumbnails (base64 images), rituals, bookmarks, and storage keys. Attachments are metadata only.
- **Personas** (`WebhookPersonality`) are id, name and cover. The session persona list returns the system prompt, scratchpad, scratchpad history, memory prompts and attachments. None of that is served here.
- **Threads and jobs** are returned as the session API returns them: `Chat` already hides its prompt and scratchpad from JSON, and `Job` is operational state.
- The subset is an allow-list in `internal/models/webhook_message.go`, so a field added to `ChatMessage` later does not reach integrations by accident.

### 4. Errors, disclosure and limits

- A chat, message or job that belongs to another account is indistinguishable from one that does not exist: both are 404, with the same wording, and the job lookup's "unauthorized" is folded into it.
- Missing scope is 403; no or bad token is 401; malformed input is 400. Unlike the session API, which ignores a filter it cannot parse, the webhook reads reject it: an integration should never believe it filtered when it did not.
- `limit` defaults to 20 and is clamped to 100. Page numbers and limits must be positive integers.
- Every read response carries `Cache-Control: no-store`, since all of it is live state (new messages, job progress).
- Each read logs the operation, the token id and the number of rows returned, never content.

## Alternatives considered

- **Reads on the existing token, no scopes.** The cheapest option, and what the requesting project first asked for. Rejected: it silently widens every existing token. A leaked Slack-trigger token would go from "can post into my threads" to "can read everything I have written". The cost of not doing it is that an integration wanting to read needs a newly created token, which is one click.
- **Per-chat tokens** (a token bound to specific threads). A stricter boundary, and worth having, but it changes the token schema and the middleware, not just handlers, and the set of threads a bridge needs changes. Tracked as a follow-up; scopes are the coarser step that does not preclude it.
- **A single `read()` operation with filters.** Distinct, cacheable, individually-authorized resources fit the existing API and its tooling better than one operation with a mode parameter.
- **A persona-addressed route** (`GET /personality/{id}/chats`). Redundant with `GET /chat?personality_id=`; listing personas (a small, deliberately thin route) is the missing piece.
- **Returning the session structs unchanged.** Identical shapes by construction, but it would serve model reasoning, portrait images, tool calls and storage keys to a static token, and couple this surface to every future `ChatMessage` field.

## Answers to the issue's open design questions

- **One operation or several?** Several, one per resource, matching the session API.
- **Shared schemas and identifiers with webhook delivery?** Identifiers, yes: a message id returned by POST is the id `GET /webhooks/chat/chat-message/{id}` takes and the id in message lists, and `job_id` from the POST response is what `GET /webhooks/job/{id}` takes. Schemas: messages use the session field names; the POST response keeps its own small shape.
- **Minimum useful read scope?** Threads, messages, job status and persona names, for the token owner only, behind an explicit scope. No cross-account, no content other than messages, no persona internals.
- **What is pushed and what is pulled?** Everything is pulled for now. There is no outbound webhook or callback from What Iff. The documented pattern is send, then poll the job, then read the reply ([integrations guide](../integrations.md)).
- **Slack/Discord-style bridges versus first-party clients?** Bridges hold a scoped static token and poll. First-party clients use the session API. See the integrations guide.

## Consequences

- Existing integrations keep working unchanged. To read, create a new token with `chat:read`.
- `webhook.Provider.CreateWebhookToken` gained a `scopes` parameter (the private overlay does not implement or call it).
- The webhook surface now has six operations and a documented capability inventory ([integrations guide](../integrations.md)).
- Tracked follow-ups, not done here: per-chat token scoping; an ascending, cursor-based "messages since" read for cheaper polling (the datastore already supports it, capped at 32 per page); rate limiting (none on the webhook routes, tracked in #247; the memory export has a per-user limiter that can be reused; page-size limits bound the cost per request); an idempotency key on POST (retrying a POST can duplicate a message); a way to tell webhook-posted user messages from UI ones; attachment downloads; outbound event delivery; message search; retention and redaction (there is no soft-delete or redaction state today: deleting a thread removes it and its messages, and messages cannot be deleted individually, so a deleted thread is simply a 404).
