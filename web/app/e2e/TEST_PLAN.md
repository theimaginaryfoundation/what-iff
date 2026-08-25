# Frontend E2E test plan

Living document. Built from a manual exploration pass through the app
(chrome-devtools MCP, logged in as a fresh local account under
`LLM_BACKEND=local`) plus the coverage that already exists. Update this
file in the same PR whenever a spec is added — move the item from
"Planned" to "Covered" and link the spec/test name.

See [`README.md`](README.md) for how to run these against a local stack.

This plan tracks browser flows. `e2e/api-tests/` covers the API contract
directly (auth token lifecycle, cross-user scoping, message/job polling,
memory CRUD) and is not itemized here the same way — see
[`docs/what-runs-where.md`](docs/what-runs-where.md#the-api-tests-suite) for
what it covers and why it's a separate suite.

## Covered

| Flow                                                                            | Spec                       | Notes                                                                               |
| ------------------------------------------------------------------------------- | -------------------------- | ----------------------------------------------------------------------------------- |
| Register with an arbitrary email (no dev-domain restriction)                    | `auth.spec.ts`             | Direct UI regression test for the removal of the hardcoded signup-domain gate |
| Login — valid credentials                                                       | `auth.spec.ts`             |                                                                                     |
| Login — invalid credentials                                                     | `auth.spec.ts`             | Asserts the error banner and that the user stays on `/auth/login`                   |
| Create a personality manually → start a new chat → send a message → get a reply | `personality-chat.spec.ts` | Uses manual creation, not the AI wizard (see Constraints below)                     |
| Returning user: resume an existing conversation found via the command palette, and carry it on | `journeys/resume-conversation.spec.ts` | Arrives by search, not by URL — a thread swap inside a live workspace rather than a cold route resolve. Mobile parked on #70 |
| Returning user: stage a skill onto a message from the chat composer | `journeys/skill-in-conversation.spec.ts` | Starts from an account that already has a personality, a skill and a thread |

## Constraints that shape what's testable hermetically

- **AI-driven flows require a real vendor LLM key** and are intentionally
  disabled under `LLM_BACKEND=mock`/`local`
  (`internal/agent/generate_personality.go`, and the same gate covers
  personality expression generation and the image-generation skill). Any
  test that exercises these needs a separate, explicitly-gated suite (real
  `OPENAI_API_KEY`, opt-in like the backend's `integration` CI job) — not
  part of the hermetic mock/local suite these specs target.
- The backend must already be running (`make dev-up` or `make run-mock` /
  `make run-local`) — Playwright's `webServer` config only manages the
  Angular dev server.

## Planned — High priority (core flows, currently zero coverage)

### 1. Chat thread lifecycle

Route: `/chat/:id`. Discovered controls (from a live chat header):
`Rename chat` (button, "Click to rename"), `Change personality (currently
<name>)`, `Export thread`, `Close thread`. Thread Manager (`/chat`, when no
thread is open, or via `All threads`) has `Active`/`Archived` tabs and a
search box.

- [x] Rename a thread and verify the new title persists — `thread-workspace.spec.ts`
      (title bar) and `thread-manager.spec.ts` (list row, `@serial`,
      currently `test.fixme` pending a fix for the dblclick-on-thread-name
      race with click-to-navigate)
- [ ] Change personality mid-thread via `Change personality (currently …)`
      and verify subsequent messages use the new personality (check the
      assistant bubble's attributed name/avatar, not just that the button
      label updated) — still open, this is a genuinely separate code path
      from the persona picker `personality-chat.spec.ts` exercises
- [x] Export button is offered on desktop — `thread-workspace.spec.ts`
      (click-through to an actual download is still unexercised)
- [x] Close a thread — `thread-workspace.spec.ts` (falls back to Thread
      Manager; reopening is exercised via `threadListPanel.open()` elsewhere)
- [x] Archive/restore a thread — `thread-manager.spec.ts`
- [x] Star/unstar, select-all + clear selection, bulk-assign personality,
      cancel the tag editor, cancel the delete confirmation —
      `thread-manager.spec.ts`
- [x] In-chat context panel: Memories tab (create/edit/delete a thread
      memory, switch to Global, "Manage all memories" nav) and Tools tab
      (enable/disable a tool for the conversation) — `thread-workspace.spec.ts`

### 2. Personality detail page

Route: `/personality/:id`. Discovered controls: `Edit` (opens the system
prompt editor, region `System prompt editor`), `Delete`, `Use in new chat`
(button, top of page — this is the code path `personality-chat.spec.ts`
already exercises via `onUseInNewChat()`; the persona-picker's "new-thread"
selection in `chat-page.component.ts` is a **separate, untested** code
path — see item 1's "change personality").

- [x] Edit the system prompt via the `Edit` button, save, verify the
      updated text renders in the `System prompt editor` region — and
      cancelling discards the draft — `personality-detail.spec.ts` (a
      subsequent chat message reflecting the new prompt is still open,
      LLM-response-dependent)
- [x] Delete a personality via `Delete`, confirm the confirmation dialog
      (shares `ConfirmationService` with everything else), verify it
      disappears from `/personality` — `personality-detail.spec.ts`
- [x] "Make default" on a non-default personality — `personality-detail.spec.ts`
- [x] Toggle "Auto-pin new User memories" — `personality-detail.spec.ts`
- [ ] Attachment upload/delete — **not testable hermetically**:
      `CreateFileAttachment` (internal/handlers/personality/fileattachment.go)
      always proxies the file to the OpenAI Files API
      (internal/agent/provider/fileattachment.go) with no mock/local bypass,
      unlike chat completions. Under `LLM_BACKEND=mock`/`local` every upload
      500s on the provider network-egress guard. Same category as item 11
      below — needs the same real-vendor-key-gated, opt-in suite, not a spec
      in this file.
- [x] Expression slots — the "Add expression" custom-key flow: the key rules,
      the duplicate-key rejection, the disabled submit, and a valid key landing
      a slot and opening the image picker for it —
      `functional/personality/expressions.spec.ts`. Note what it deliberately
      does not assert: a new key is a client-side placeholder
      (`submitCustomKey()` -> `PersonalityViewService.setExpressions()`, which
      updates the cache "without re-fetching"), and is only persisted once an
      image is assigned to it — which needs a gallery a hermetic run cannot
      fill. Assigning an image is therefore still open, blocked behind the
      same constraint as the gallery import item below.

### 3. Profile & Settings

Opened via the sidebar's `Open profile for <username>` button — renders a
modal (`Profile & Settings`, tabs `Profile` / `Subscription` / `Usage` /
`Billing`). Discovered form fields on the `Profile` tab: `Email`, `First
Name`, `Last Name`, `Theme` (select: `Use system setting` / `Light` /
`Dark`), `Save Changes`. Separate password form: `Change Password` (current
password), `New Password`, `Confirm New Password`, `Update Password`.

- [x] Update first/last name and theme, save, verify the change persists
      (reload, reopen modal) — `profile.spec.ts`
- [x] Change password with a correct current password, verify the new
      password logs in successfully on next login (and the old one doesn't)
      — `profile.spec.ts`
- [x] Change password with an incorrect current password — **found a real
      bug**: the backend correctly returns 401 ("Current password is incorrect",
      `internal/handlers/user/user.go`), but the frontend's global
      `auth.interceptor.ts` treats _any_ 401 (other than on `/user/login`,
      `/user/register`, `/user/refresh`) as session expiry. It calls
      `authService.refreshToken()`, retries the original request, and when
      that retry also 401s (refreshing the token doesn't fix a wrong
      password) the shared `catchError` calls `authService.logout()`. Net
      effect: entering the wrong current password force-logs the user out
      instead of showing an inline error. Test written as `test.fixme` in
      `profile.spec.ts` documenting the current (wrong) behavior — flip it
      once the wrong-current-password force-logout bug is fixed (either
      exclude password-change-style 401s
      from the interceptor, or have `/user/password` respond with a status
      the interceptor doesn't treat as session-expiry, e.g. 400/403 —
      either fix works, pick whichever the team prefers).

### 4. Billing

Billing, subscription and usage are not part of the open-source build: the
routes are mounted through the private route seam and the open-source app
carries none of the UI. Their tests live with that code, outside this repo.
Nothing here should assert on billing state; the Integrations specs branch on
the subscription gate (open in the OSS build) for exactly that reason.

## Planned — Medium priority

### 5. Skills

Route: `/skills` (Config mode). A built-in system skill "Generate image"
already exists (`SYSTEM` badge, `Call`/`Edit` buttons, `Delete` disabled —
presumably system skills aren't user-deletable). `Create skill` button for
custom ones.

- [x] Create a custom skill (name + prompt template), verify it appears in
      the list, then delete it — `skills.spec.ts`
- [x] Edit an existing skill and save; verify the updated name renders —
      `skills.spec.ts`
- [x] Stage a skill onto a message from the chat composer — the picker's
      menu entry, filtering, the staged chip and its removal, and the
      `rituals` field on the send — `journeys/skill-in-conversation.spec.ts`;
      the `/skill` slash command and the no-match state —
      `functional/chat/skill-picker.spec.ts`. Note this is a different data
      source from the Skills page: `getAvailableRituals(chatId)` merged with
      `listSystemRituals()`, so a skill that lists at `/skills` can still be
      absent here.
- [ ] Call a skill from the Skills page directly (`Call` button) — still
      open, and distinct from the composer path now covered above

### 6. Memories

Route: `/memories` (Config mode). Filters: `All` / `Global` / `Personality`
/ `Thread` / `Summary`; sort: `Created (newest)` / `Created (oldest)` /
`Last updated`; `Add memory` button (also reachable from the in-chat context
panel's Memories tab — see item 1); `Batch Import` (disabled in the empty
state — find out what enables it).

- [x] Sort by creation time (newest/oldest) and verify row order —
      `memories.spec.ts`
- [x] Paginate past the 24-per-page boundary (`core/services/memory-view.service.ts`)
      and back — `memories.spec.ts`
- [x] Delete a memory from its own detail page (`/memories/:id`), distinct
      from the list page's delete — `memories.spec.ts`
- [ ] Add a memory manually via the list page's own `Add memory` button (the
      in-chat context panel's is now covered — item 1)

### 7. Modes

Route: `/mode` (Config mode). `Create mode` — "Modes define prompt
behavior, attached skills, MCP reach, and optional default model
preferences."

- [ ] Create a mode with a name/prompt, verify it appears in the list and
      can be selected somewhere (need to find where modes get applied —
      wasn't visible in this exploration pass; likely the chat composer's
      "Open chat options" button, `uid=14_18` in the raw exploration)

### 8. Gallery

Route: `/gallery`. Toggle between `Gallery` and `Expression Manager` views;
filters (`Show all personalities` / `Show global images only`, search);
sort (`All` / `Generated` / `Imported` counts, `Created` / `Last used`);
`Import image` button.

- [x] Switch between Gallery and Expression Manager, the source/sort segments,
      and the empty state for a search that matches nothing —
      `functional/gallery/gallery.spec.ts`. These cover the page's controls
      rather than its contents, which is all a hermetic run can reach — see
      the import item below.
- [ ] Import an image, verify it appears and the `Imported` count increments —
      **not testable hermetically**, same category as the personality
      attachment item above: `ImportImage`
      (internal/handlers/imagegallery/import.go) goes through
      `handlerutils.UploadFileAttachment`, which proxies every upload to the
      vendor Files API with no mock/local bypass, so imports fail on the
      provider egress guard under `LLM_BACKEND=mock`/`local`. Needs the same
      real-vendor-key-gated, opt-in suite.
- [ ] Filter by a specific personality vs. global-only — still open; needs an
      image in the library, so it is blocked behind the same constraint

## Planned — Lower priority / blocked on a decision

### 9. Tools / Integrations (connectors + webhooks)

Route: `/integrations` (Config mode). Has a real credential input
(`Authentication token`) in the "Add Connector" form.

- **Open question before writing this**: how do we avoid a fixture value
  that reads as a real secret ending up in a Playwright trace/video
  artifact (which get attached to CI runs and could get shared)? Use an
  obviously-fake placeholder (`test-token-not-real`) at minimum; consider
  whether trace/video retention should be `off` specifically for this spec.
- [ ] Create a connector with a placeholder token, verify it lists under
      `Connectors`
- [x] Create and revoke a webhook API token; copy the one-time token via the
      "Copy Token" button (asserted through the shared `ConfirmationService`
      success alert, not the real OS clipboard — see `integrations.spec.ts`);
      empty-tokens state before any token exists — `integrations.spec.ts`

### 10. Jobs

Route: `/agent-jobs` (Config mode). `Create job`; filters: status
(`All`/`Active`/`Paused`/`Complete`/`Failed`), schedule
(`All`/`One-off`/`Recurring`).

- [ ] Lower priority — feels better suited to backend integration/unit
      tests than browser E2E given it's schedule/execution-state heavy, not
      UI-interaction heavy. Revisit if the team disagrees.

### 11. Personality expression generation

"Generate default expressions" on the personality detail page — real
image-generation call, same `LLM_BACKEND=mock/local` disablement as the AI
personality wizard. Not testable hermetically; would need the same
real-vendor-key-gated, opt-in pattern as item 11 in "Constraints" above.
Not scheduling this until that pattern exists for _any_ AI-generation flow.

## Notes for whoever picks up an item

- No `data-testid` attributes exist anywhere in this app yet — everything
  in the specs so far targets by role/placeholder/text. Keep doing that;
  don't introduce test-ids piecemeal, it makes selectors inconsistent
  across specs. (Worth a separate discussion if this starts getting
  genuinely brittle.)
- Modals in this app are **not all built on the same component** — the
  shared `ui-modal` (`shared/ui/modal/`) is fine for ARIA roles, but the
  terms-of-service modal was a bespoke one with a real accessibility bug
  (`aria-hidden` on its own backdrop, fixed in `c294d960`). Before writing
  a test against any modal you haven't touched yet, check which
  implementation it uses — don't assume role-based selectors will just
  work.
- The persistent background API/Ollama/web-dev-server processes used
  during manual exploration can get into a stale state after heavy file
  changes (e.g. mid-rebase) — a Vite `<vite-error-overlay>` blocking all
  clicks was hit twice this session, both times fixed by killing and
  restarting `ng serve`. If a spec starts timing out on **every** locator
  with no clear cause, check the dev server log before debugging the test.
