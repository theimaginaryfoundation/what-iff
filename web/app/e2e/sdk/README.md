# Generated API SDK

## What's generated vs hand-written

- `schema.d.ts` — **generated**, committed. Types for every path/schema in
  the repo-root `openapi.yaml`, produced by `openapi-typescript`. Never
  hand-edit this file; regenerate it instead.
- `client.ts` — **hand-written**. The only file that constructs an
  `openapi-fetch` client. Exposes `createApiClient(token?)` plus thin typed
  helpers (typed call + throw on non-2xx, no business logic):
  - auth/users — `registerUser`, `loginUser`, `refreshToken`, `deleteUser`
  - models — `listModels`
  - personalities — `createPersonality`, `listPersonalities`,
    `listPersonalitiesPage` (keeps the pagination envelope, for callers that
    assert on `total_count`), `getPersonality`, `updatePersonality` (PUT;
    `name` and `system_prompt` are required on every call),
    `deletePersonality`, `uploadPersonalityAttachment`
  - personality expressions — `listPersonalityExpressions`,
    `upsertPersonalityExpression`, `deletePersonalityExpression`
  - chats/threads — `createChat`, `listChats`, `getChat`, `updateChat`
    (PATCH; the only verb that accepts `archived`), `deleteChat`,
    `sendChatMessage`, `listChatMessages`, `getChatContext`,
    `patchChatContext`
  - search — `search` (the cross-resource command palette)
  - jobs — `getJob` (the background job a chat message spawns to generate
    its reply)
  - memories — `createMemory`, `createMemoriesBatch`, `listMemories`,
    `deleteMemory`
  - rituals (skills) — `createRitual`, `listRituals`, `listSystemRituals`,
    `deleteRitual`
  - webhook tokens — `createWebhookToken`, `listWebhookTokens`,
    `revokeWebhookToken`
  - agent jobs — `listAgentJobs`, `parseSchedule`, `deleteAgentJob`

  Add helpers here as fixtures need them; keep it thin rather than wrapping
  every endpoint speculatively.

Deliberate gap, because the operation isn't in `openapi.yaml` and so isn't
in the generated types: **no `createAgentJob`** (no `POST /agent-job` at all).

`e2e/fixtures/` is the only place in the **browser** suite allowed to import
from `e2e/sdk` directly — it's the swap point between "raw HTTP" and
"generated SDK", kept small so future changes here stay mechanical. A
browser spec that needs API-level setup goes through a fixture, not this
module.

`e2e/api-tests/` is the deliberate exception: it has no browser and no UI to
set up, so its specs *are* direct SDK calls.

## Regenerating

```bash
npm run sdk:generate
```

Runs `openapi-typescript ../../openapi.yaml -o e2e/sdk/schema.d.ts`. Run
this after any change to the repo-root `openapi.yaml` and commit the diff.

If generation fails with `Can't resolve $ref`, the spec itself has a broken
or missing schema reference — fix `openapi.yaml`, don't work around it here.

## CI drift check

`frontend-pr-validation.yml` runs `npm run sdk:generate` and fails the build
if it produces a git diff, the same pattern used for Ent codegen
(`make generate` + `git diff --exit-code`). This keeps `schema.d.ts` from
silently drifting from `openapi.yaml` between PRs.
