# ADR 0x037: Timestamps from outside the server are untrusted

- **Status:** Accepted
- **Date:** 2026-11-01
- **Deciders:** What Iff maintainers

## Context

One production user broke the admin user list.
The API logged `error calling MarshalJSON for type time.Time: year outside range [0,9999]`, and every page or search that included that user returned a 500.

`encoding/json` refuses to encode a `time.Time` whose year is outside 0-9999.
`RespondWithJSON` marshals the whole payload at once, so one bad timestamp anywhere in a response fails all of it.

Several paths store a timestamp that came from outside the server without checking it:

- **Chat import.** `create_time` in a ChatGPT-format export is a float of Unix seconds. An export that uses milliseconds lands around the year 50000, and a value too large for int64 made the conversion undefined. `normalizeImportedMessageTimes` only moves times forward, so one such message dragged every later message in the transcript there too.
- **Provider responses.** `GenerateResponse.CreatedTime` becomes the chat's `last_message_time`. A provider can be any OpenAI-compatible endpoint an account configured (ADR 0x027), so its `created_at` is not ours to trust. #367 already fixed the zero case.
- **Remote sync** (ADR 0x033) writes a peer's message and thread times locally.
- **Account backup restore** writes every record's times as the file states them. JSON decoding limits these to 4-digit years, so they cannot break encoding, but they can still be the year 0001 or 9999.
- **Message creation** stores a caller's `SentAt` as given. Relays pass platform timestamps through it.

The data already in the database may be bad too, and we chose not to clean it up by hand.
The app has to be correct with bad rows present, because whatever let one in once can let another in.

## Decision

### 1. Timestamps from outside the server are checked against a plausible window before they are stored

`models.PlausibleTime(t, now)` accepts times from `models.EarliestPlausibleTime` (2000-01-01 UTC) up to now plus `models.MaxFutureSkew` (24 hours).
Nothing this app stores predates 2000, and the floor sits well clear of the zero and Unix-epoch values that broken sources fall back to.
The skew allows for a peer's or platform's clock running ahead.

Every write path listed above passes outside times through `models.PlausibleTimeOr(t, fallback, now)`.
What replaces an implausible time depends on the path:

- **Import:** a bad message time takes the time of the message before it, and `normalizeImportedMessageTimes` then spaces the tie apart, so the transcript keeps its order. A bad chat time takes the first good message time, or now. A bad checkpoint time is dropped. This runs before normalization, so one bad message cannot move the others.
- **Provider, message creation, remote sync, backup restore:** now.
- **An optional `last_message_time`** (chat create, `UpdateChatContinuity`, backup restore) is left unset rather than replaced.

Remote sync keeps the remote time on its link rows (`RemoteSentAt`, `RemoteHeadSentAt`) exactly as sent.
Sync compares those with the remote side, so only the local copy is replaced.

`floatToTime` also returns the zero time for NaN or a value beyond the year 9999, so the float-to-int64 conversion is always defined.

### 2. Reads cannot fail on a time JSON cannot encode

The datastore converters for chats and messages (`toChatModel`, `toChatMessageModel`) and the chat and account exports pass stored times through `models.JSONSafeTime`.
It returns an encodable time unchanged and clamps any other to the nearest end of the years 0-9999.
Those converters feed the REST API, the agent's tools and the exports alike.

`RespondWithJSON` is the safety net for everything else, including the private overlay's admin handlers.
When the first marshal fails, it clamps every such time on a copy of the payload and retries once, logging a warning.
The walk runs only on that failure path, so a normal response pays nothing for it.
It never modifies the caller's value, which may be shared with a cache.

Reads clamp to the encodable range, not to the plausible window.
A read should not quietly rewrite a stored value that is merely odd, only keep it from breaking the response.

## Alternatives considered

- **Fix the bad rows with SQL.** It repairs today's data and leaves the app just as fragile for the next bad row. Rejected in favour of making the app resilient.
- **Read millisecond values as milliseconds.** That would rescue the real times in such an export. It is a guess about one kind of bad source, and the plausible window catches that source and every other one. Left for later if real exports need it.
- **Reject an import that has a bad time.** One bad timestamp would cost the user the whole conversation. Replacing the time keeps the content.
- **Clamp to the plausible window on read.** That would hide odd stored values that still encode, such as the year 0001 or 9999, behind invented ones. Reads only prevent failures; writes enforce plausibility.
- **An Ent hook that rewrites times on every mutation.** It would apply implicitly to server-generated times too, and it would miss raw SQL. The explicit calls at each boundary are easier to follow.
- **A wrapper time type with a lenient `MarshalJSON`.** It would change every model that carries a time. That is too wide for the problem.

## Consequences

- A bad timestamp in an import, a provider response, a remote peer or a backup no longer reaches the database.
- A bad timestamp already stored shows as the year 0 or 9999 instead of breaking every response that includes it, and `RespondWithJSON` logs a warning when its safety net fires.
- The private admin user list, which builds its own `last_message_at` from `MAX(sent_at)`, is covered by the `RespondWithJSON` net once the overlay pins this change. It needs no change of its own.
- A new path that stores an outside timestamp should call `models.PlausibleTimeOr`. Nothing enforces this yet.

## Not done yet

- Other serializers that do not go through `RespondWithJSON` or the chat and message converters (agent tool results built from other entities, streaming payloads) still marshal stored times as they are.
- Times on other imported entities (moods, personalities, rituals and so on) are checked only on the backup-restore path.
