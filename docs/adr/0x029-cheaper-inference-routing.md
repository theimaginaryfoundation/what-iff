# ADR 0x029: Discounted inference routing through the Cheaper Inference relay

- **Status:** Proposed (phase 1; not for real-user traffic until the vendor questions below are answered)
- **Date:** 2026-10-24
- **Deciders:** What Iff maintainers
- **Issue:** none
- **PR:** #377

## Context

Cheaper Inference (platform.cheaperinference.com, run by Keak AI, Inc.) sells discounted access to models that our own providers already serve: Gemini, Mistral, DeepSeek, Qwen, MiMo, GPT, Claude, GLM and more.
It exposes OpenAI-compatible Chat Completions, a stateless Responses layer and an Anthropic-compatible Messages endpoint.

We want those savings without adding anything users have to think about when they pick a model.
A first cut added relay copies of models (`ci/glm-5.3`) to the catalog.
We rejected it because it puts the same model in the picker twice and leaves the user to choose a route.
The relay is a way to reach a model, not a model, so it belongs below the model picker.

The relay also changes where a user's conversation goes:

- The relay keeps no prompt or response bodies in its own database, but forwards them to whichever source is cheapest.
- By default that is a "spare-capacity marketplace" that the relay's subprocessor page does not name.
- Its Seller Agreement routes customer requests through sellers' own cloud API accounts (AWS, Microsoft, Google, Venice and others).
  It says those providers may monitor, retain, review or report the requests under the seller's terms, and that Keak has not reviewed whether those terms allow this.
- A request with `zdr: true` skips the marketplace and uses only sources that claim zero data retention.
  For seller accounts that claim rests on the seller's word.
- The subprocessor page is marked "Draft — not legal copy. Do not rely on this page."

So routing has to be something an account chooses.
It should be easy to leave, and later it can come with a share of the savings.

How hard routing is depends on how each family of models talks to its vendor:

- **Chat Completions vendors** (Gemini, Mistral, DeepSeek, Qwen, MiMo) are stateless.
  Each request carries the whole conversation, so any endpoint that speaks the format can serve any call.
- **Claude and GLM** use the Messages API.
  The relay has a Messages endpoint, but its support for the Anthropic-only features we use (native web search, the beta MCP connector, prompt caching) is undocumented.
- **OpenAI models** chain `previous_response_id`, which OpenAI stores on its side.
  The relay's Responses layer stores nothing and rejects `previous_response_id`.
  Routing them would mean rebuilding OpenAI context from our database, and any route switch mid-chat would break the chain.

## Decision

### 1. Routing sits below the model, invisible in the picker

- No model is added to the catalog, and the model a user picks never changes.
- `provider.RelayRoute` sits inside the existing Chat Completions providers (Gemini, Mistral, DeepSeek, Qwen, Xiaomi).
  Each call goes through `relay.chatCompletionsNew` / `relay.chatCompletionsStream`, which either sends it to the relay or to the provider's own client.
- `CheaperInferenceModels` maps our model names to the relay's ids (for example `mistral-medium-3-5` → `mistral-medium-3.5`).
  Only listed models are routed.
  The list holds the eight Chat Completions catalog models the relay's public catalog showed on 2026-10-24; `gemini-3.5`, `deepseek-flash` and `mimo-v2.6-flash` are not on it.
- The adapters, rendering, tool handling and replay quirks (Gemini thought signatures, MiMo `reasoning_content`) are untouched.
  The relay serves the same wire format, so the same adapter drives both routes.

### 2. Phase 1 covers the Chat Completions vendors only

- Claude/GLM wait for the vendor's answer on Messages feature support.
- OpenAI waits for a decision on whether the savings justify rebuilding OpenAI threading.
- Subagents, checkpoint summaries, memory and naming calls are not routed.
  Only a user's own chat turns are.

### 3. An account-level opt-in, set by admins for now

- `users.inference_relay_opt_in` (default false) is the switch.
  `Datastore.AdminPatchUserInferenceRelay` sets it, sharing one write path with the experimental-models toggle.
- The public build ships no route or UI to change it.
  The private layer's admin tools call the datastore method.
  A self-hosted operator can set the column directly.
- `UserResponse.inference_relay_opt_in` shows it read-only, so a later settings screen can show and change it without an API shape change.
- The opt-in is read once per turn, and only when the turn's model is routable and a relay key is configured.
  The resulting `provider.RelayTurn` travels on the turn's context, and its presence is what allows routing.

### 4. Fallback and a circuit breaker

- Any relay error falls back, within the same call, to the provider's own endpoint with our own model id.
  A stream that already showed output is not retried, because the user has seen it.
- After a fallback, the rest of that turn stays direct.
  A turn never alternates routes, so replayed tool-call history comes from one place.
- A per-model, per-process breaker skips the relay for two minutes after three consecutive unhealthy failures.
  Unhealthy means transport errors, 5xx, 401, 402, 403, 404, 408 and 429.
  After the cooldown, one probe call decides whether it closes or reopens.
  A 400 counts as healthy: the relay answered and the request was the problem, so one user's bad request cannot switch routing off for everyone.
- The relay client makes no SDK retries.
  The relay fails over internally, and a quick fallback beats a slow retry.
  Each fallback counts on `whatiff.gen_ai.retries` with reason `relay_fallback`.

### 5. Zero data retention is on by default

- Every relay request carries `zdr: true` unless `CHEAPER_INFERENCE_ZDR=false`.
- The flag is set on the relay client, so no call path can drop it.
  It never reaches the direct providers.
- Turning it off logs a warning at boot.
  It is for synthetic test traffic only.

### 6. Usage says how the turn was served

- `metering.Usage.Route` is `""` (own provider), `cheaperinference` (every call through the relay) or `mixed`.
- A metering implementation can give relay-served usage a discount.
  The plan is for the private layer to share 5–10% of the savings with opted-in users.
  The public build carries the fact, never a price.

## Alternatives considered

- **Relay models as separate catalog entries (`ci/…`).**
  This was the first cut; rejected as above.
- **A dedicated relay adapter per wire format.**
  Unneeded: the existing adapter already speaks the format, and only the endpoint differs.
- **Rewriting requests in the HTTP transport.**
  This would be invisible to every call site, including memory and summaries.
  It was rejected because it means editing request bodies by host and hiding a routing decision far from the code that cares about it.
- **Switching back to the relay mid-turn once it recovers.**
  Rejected because a turn's tool-call replay should come from one provider.
- **A breaker shared across server instances.**
  Not needed at this traffic level: each instance learns quickly from its own calls.
- **Routing only when a user did not bring their own key.**
  Deferred; see Not done yet.

## Consequences

- An opted-in account's turns on the eight routed models go through the relay, and nothing visible changes.
  A relay outage costs at most one failed call per turn plus the fallback latency, and after three failures it costs nothing until the probe.
- Switching routes discards provider-side prompt caches, so some of the discount goes to cache misses.
- Gemini tool-call turns depend on the relay passing `extra_content.google.thought_signature` through.
  If it strips it, Google rejects the second round from either route.
  This must be checked live before anyone is opted in.
- Two vendors now see an opted-in user's content: the relay and its chosen source.
  The ADR's Context is what an opt-in screen must explain.

## Not done yet

- **Vendor answers needed before real-user traffic:**
  - Name the marketplace upstream.
  - Their policy on seller-account routing.
  - Which sources serve ZDR for each routed model.
  - Whether Gemini thought signatures pass through.
  - Messages-endpoint feature support (for phase 2).
- **A live smoke test** with a relay key: one routed turn per vendor, with and without tool calls.
- **Phase 2:** Claude and GLM through the relay's Messages endpoint, gated per turn on the Anthropic-only features it supports.
- **Phase 3, if worth it:** OpenAI models, which first need OpenAI chats to stop depending on `previous_response_id`.
- **Bring-your-own-key accounts:** an account using its own provider key should probably never be routed onto our relay key.
  The `providerkeys.Resolver` interface cannot yet say whose key a call would use.
- **Opt-in surfaces:** the admin route and UI (private), a user-facing opt-in with the privacy explanation, and the metering discount.
- **`safety_identifier`:** the relay asks for one per end user, so that a provider block hits one user rather than our whole account.
  None of the Chat Completions adapters send it yet.
