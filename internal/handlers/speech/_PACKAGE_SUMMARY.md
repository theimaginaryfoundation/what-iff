# Package: `internal/handlers/speech`

## Role

Serves voice replies and dictation (ADR 0x052): reading a reply aloud, a track's segments, trying a voice, and transcribing a clip the person recorded in the composer.

## Responsibilities

- `GET /speech/dictation` answers whether the composer has a microphone (`available`), and if so the dictation model's formats and longest clip, so the browser records what it takes.
  With no dictation model (off, not set, or unusable) it answers 200 with `available: false` and the `utility_*` code saying why, since no microphone is not an error.
- `POST /speech/messages/{id}` answers the reply's speech track in its personality's voice, started if it isn't there yet; `GET /speech/tracks/{id}` answers its status and segments so far, which the client polls while it is `open`; `GET /speech/tracks/{id}/segments/{seq}` answers one segment's audio, privately cacheable since it never changes.
  No voice, or one that can't be used, is a 409 `voice_unavailable`; segment file keys never leave the server.
- Autoplay (ADR 0x052 §9): `POST /speech/tracks/{id}/claim` (only one client gets a pending track; the rest are answered `claimed: false`), `POST /speech/tracks/{id}/skip`, `GET /speech/chats/{id}/pending` (newest first, `[]` when none) and `PUT /speech/chats/{id}/autoplay` (`true`, `false`, or `null` to follow the personality: its own route, since a PATCH can't tell null from absent).
- `POST /speech/preview` reads up to 300 characters aloud in a voice not yet saved (billed, not stored), answering the audio with `no-store`.
- `POST /speech/transcriptions` takes one clip as multipart form data (`audio`, `format`, `duration_seconds`, optional `chat_id` and `language`) and answers its transcript.

## Dependencies

- **Inbound:** `internal/server` mounts it on the authenticated router, with the agent.
- **Outbound:** `internal/agent` (`Dictation`, `Transcribe`, through the `Speech` interface), `internal/featuregate`, `internal/models`, `internal/handlers/handlerutils`.

## Non-obvious decisions

- Dictation is an entitlement-gated feature (`featuregate.IsEntitled`); the open-source build gates nothing.
- A request is capped at 13 MB before parsing; a larger one is a 413 `clip_too_long`, as is a clip longer than the model takes.
  A format the model doesn't take is a 415 `audio_format_unsupported` (the composer then records WAV), out of credits a 429 `quota_exceeded`, and no usable dictation model a 409 with the role's code.
- A provider's failure is a 502 with a plain message; the provider's own error is logged, not shown.
- A `chat_id` that isn't a UUID is dropped rather than trusted, and a negative duration read as none.
- The clip is never stored: it is read into memory, passed on, and the multipart temp files removed.

## Testing

- `handler_test.go` — over a fake agent: listening, a track and a segment's audio (headers; no file key in the answer), unknown ids, the refusals' codes, and previews (audio, a missing model, out of credits); the dictation model and why there is none; a transcript with every field passed on; a bad chat id and a negative duration dropped; each refusal's status and code (no clip, unknown format, out of credits, a format the model refuses, too long, no dictation model, a provider failure without its own error); an oversized upload.

## Related documentation

- [`internal/agent`](../../agent/_PACKAGE_SUMMARY.md) — `Transcribe`.
- [ADR 0x052](../../../docs/adr/0x052-voice-replies-and-dictation.md)
