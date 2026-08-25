# Package: `internal/middleware`

## Role

HTTP **middleware** for the API: authentication, optional subscription gates, CORS, logging, and request context (user id, role, timezone, billing plan/status for downstream quota/accounting).

## Responsibilities

- **Auth (`auth.go`):** JWT/Cognito claim extraction (including API Gateway v2 context), validation, population of `UserIDKey`, `UserRoleKey`, `ClientTimezoneKey`, `UserPlanIDKey` on the request context. Paths for invite/login flows as implemented.
- **Subscriber access (`premium.go` — legacy filename):** `RequireActiveSubscription` / `HasActiveSubscription` gate routes that need an active Stripe subscription (`active`, `trialing`, `past_due`). Webhook-token requests are treated as pre-authorized.
- **CORS / logging:** Used from `internal/server` router setup.

## Key types and entry points

| Symbol | Notes |
|--------|--------|
| Context keys | `UserIDKey`, `UserRoleKey`, `ClientTimezoneKey`, `UserPlanIDKey`, `UserSubscriptionStatusKey` — consumed by handlers and agent for timezone formatting, quota, and billing/accounting context. |
| `RequireActiveSubscription` | Middleware for any remaining subscription-gated endpoints. |

## Dependencies

- **Inbound:** `internal/server` applies middleware to route trees.
- **Outbound:** `internal/auth`, `internal/datastore` (user/load where needed), `internal/handlers/handlerutils` (shared error envelope), `ent`, AWS Lambda API Gateway proxy types for JWT context, `zap`.

## Non-obvious decisions

- **Rejections use the shared error envelope, not `http.Error`.** Every middleware rejection goes through `handlerutils.RespondWithError`, so a client parsing the body does not have to special-case which layer refused it (ADR 0x020). This is why **`RequireRole`, `RequireActiveSubscription`, and `RequirePremiumPlan` all take a `*zap.Logger` as their first parameter**: the helper logs every error response, and a middleware rejection should be as visible as a handler one. Omitting it is a compile error rather than a silent loss of logging, so a new call site cannot get this wrong — but the parameter is easy to be surprised by, which is why all three are named here rather than described in general terms.
- **This package could not use `handlerutils` until the cycle was broken.** `handlerutils` imported `internal/agent` for one file-upload call, and `agent` imports this package — so `middleware → handlerutils` closed a ring. `handlerutils` now declares a narrow `FileAttachmentUploader` interface instead, which is what makes the shared helpers reachable from here at all.
- **Cognito / API Gateway:** `tryExtractCognitoClaims` documents prerequisites (HTTP API v2, JWT authorizer, `ProxyWithContext`).
- **Billing context:** `auth.go` carries plan/subscription fields into request and async contexts for quota and accounting; agent tool availability no longer gates on a specific product/price ID.

## Testing

- `auth_timezone_test.go` — timezone propagation behavior.
- `premium_test.go` — premium middleware behavior.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **HTTP Layer** (middleware bullet).
