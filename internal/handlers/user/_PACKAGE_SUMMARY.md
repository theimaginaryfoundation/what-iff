# Package: `internal/handlers/user`

## Role

HTTP API for **users** — profile, preferences, registration/login-adjacent flows, and invite handling per `user.go` and related files.

## Responsibilities

- **`Handler`:** `/api/...` user routes (exact paths in `user.go`); uses `internal/auth` and datastore.
- Registration validates signup policy before user creation: production rejects `+` email aliases, and a configured email allowlist (`ALLOWED_EMAILS`) applies in every environment — empty allowlist permits all emails.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/auth`, `internal/models`, `mux`, `zap`.

## Non-obvious decisions

- Cognito vs legacy JWT flows — follow comments in middleware and this handler when changing auth.
- Production's plus-alias restriction is driven by `server.Config.Environment` (`ENV`, then `ENVIRONMENT`); the email allowlist is configured via `ALLOWED_EMAILS`.
  Both checks run before datastore/Stripe side effects.
- **Preferences report the deployment's image vendors.**
  `WithImageGenerationProviders` (set by `internal/server` from the key resolver, asked per request) is echoed as the read-only `image_generation_providers` on GET/PUT `/user/preferences`, so the settings UI offers only vendors the caller has a key for.
- **Account deletion purges stored files.**
  With a store from `WithFileStore`, `DeleteUser` removes everything under `users/{id}/` and the account's `exports/{id}/` bundles after the row delete (`storage.PurgeUserObjects`), best effort.

- **Current timezone is separate from the profile timezone.**
  `PUT /user/current-timezone` (`current_timezone.go`) stores the zone the web app's browser reports (`users.current_timezone`), writing only on a change, and answers with the resulting `CurrentTimezoneReport`.
  When the user's devices keep disagreeing, the datastore pauses following instead; the answer then carries `timezone_following_paused_at` (and `paused_now` on the report that paused it).
  `POST /user/current-timezone/resume` ends the pause.
  Jobs with `follow_user_timezone` run in it; the profile `timezone` is unchanged.
  It is best effort: Discord and webhooks never report one.

## Testing

- `user_test.go` — selected HTTP behaviors.
- `current_timezone_test.go` — current timezone reporting: valid, unknown, empty, bad body, no user.
- `preferences_test.go` — image vendor list on preference responses; an unknown `image_generation_provider` is a 400.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **HTTP Layer** and auth.
