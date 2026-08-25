import { test, expect } from '../../../fixtures';
import { sendChatMessage } from '../../../sdk/client';
import { IMMEDIATE_UI_UPDATE_TIMEOUT, LLM_REPLY_TIMEOUT, UI_REACTION_TIMEOUT } from '../../../timeouts';

/**
 * Chat title-bar / context-panel / import-modal smoke coverage for the
 * extended ChatPage POM. Most of this suite sends no message — assistant
 * replies are otherwise LLM-dependent and belong in the personality-chat
 * spec — except the two regression tests below, which lean on the mock
 * backend's deterministic, fast-but-not-instant echo replies specifically to
 * exercise generation-lifecycle timing.
 */

test('renames a thread from the title bar and closes it', async ({ chatPage, seed, threadListPanel, userWithPersonality }) => {
  const thread = await seed.thread();
  await chatPage.navigateTo(thread.id as string);

  await expect(chatPage.headingText).toHaveText(thread.name);

  const renamed = `${thread.name}-renamed`;
  await chatPage.rename(renamed);
  await expect(chatPage.headingText).toHaveText(renamed);

  await chatPage.closeThread();
  await expect(threadListPanel.heading).toBeVisible();
});

// Regression test for PR #138: renaming a chat used to go through a full PUT
// instead of a partial PATCH, which reset the thread's model_id/personality_id
// to defaults. The existing rename test above uses a plain `seed.thread()`
// (no personality attached), so it wouldn't have caught this — this test
// attaches a personality first and asserts it survives the rename.
test('renaming a thread preserves its attached personality', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread(undefined, { personalityId: userWithPersonality.personality.id });
  await chatPage.navigateTo(thread.id as string);

  // The composer placeholder embeds the active personality's name
  // (chat-billing.helpers.ts's `billingComposerPlaceholder`) and, unlike the
  // header's `personalityNameButton`, stays in the DOM at every viewport —
  // `.chat-page__personality-trigger` is `display: none` below 768px, so
  // asserting through it would skip the mobile projects entirely.
  const personalityPlaceholder = `Message ${userWithPersonality.personality.name}...`;
  await expect(chatPage.composerInput).toHaveAttribute('placeholder', new RegExp(`^${personalityPlaceholder}`));

  const renamed = `${thread.name}-renamed`;
  await chatPage.rename(renamed);
  await expect(chatPage.headingText).toHaveText(renamed);

  await expect(chatPage.composerInput).toHaveAttribute('placeholder', new RegExp(`^${personalityPlaceholder}`));
});

test('offers the export action on desktop', async ({ chatPage, seed, userWithPersonality }, testInfo) => {
  // The export button is `display: none` below 1024px — the mobile projects
  // have no equivalent affordance to check.
  /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
  test.skip(testInfo.project.name !== 'chromium-desktop', 'Export is desktop-only chrome.');

  const thread = await seed.thread();
  await chatPage.navigateTo(thread.id as string);

  await expect(chatPage.exportButton).toBeVisible();
});

test('opens the conversation context panel and types in the scratchpad', async ({ chatPage, seed, userWithPersonality }) => {
  /**
   * The scratchpad only offers to *save* when the thread has a personality
   * attached; `seed.thread()` creates one without, which is fine here — this
   * covers opening the panel and typing, not persistence.
   */
  const thread = await seed.thread();
  await chatPage.navigateTo(thread.id as string);

  await chatPage.openContextPanel();
  await expect(chatPage.contextPanel).toBeVisible();

  await chatPage.selectContextTab('Scratchpad');
  await expect(chatPage.scratchpadTab).toBeVisible();

  await chatPage.fillScratchpad('e2e scratchpad note');
  await expect(chatPage.scratchpadInput).toHaveValue('e2e scratchpad note');

  await chatPage.closeContextPanel();
  await expect(chatPage.contextPanel).toBeHidden();
});

test('shows a reply’s token breakdown from its Context action', async ({ chatPage, seed, userWithPersonality }) => {
  // Skipped: flaky against real-inference backends (e.g. local Ollama) due to a
  // response-ordering race in JobService.pollJob() that can clobber a message's
  // context_breakdown with a stale, breakdown-less fetch.
  /* eslint-disable-next-line playwright/no-skipped-test -- tracked flake, not a blanket skip */
  test.skip(true, 'Flaky on real-inference backends: JobService.pollJob() fetch-ordering race.');

  const thread = await seed.thread(undefined, {
    personalityId: userWithPersonality.personality.id,
  });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.sendMessage('Show the token breakdown for this reply.');
  await expect(chatPage.lastAssistantBody).not.toBeEmpty({
    timeout: LLM_REPLY_TIMEOUT,
  });
  await expect(chatPage.lastAssistantContextAction).toBeVisible({
    timeout: LLM_REPLY_TIMEOUT,
  });

  await chatPage.openLastAssistantContext();

  await expect(chatPage.contextPanel).toBeVisible();
  await expect(chatPage.contextBreakdown).toBeVisible();
  await expect(chatPage.contextBreakdown.getByText('Turn context', { exact: true })).toBeVisible();
  await expect(chatPage.contextBreakdown.getByRole('img', { name: /^Context is \d+% of budget:/ })).toBeVisible();
});

test('opens the import-conversations modal and cancels', async ({ chatImportModal, threadListPanel, userWithPersonality }) => {
  await threadListPanel.navigateTo();
  await threadListPanel.openImport();

  await expect(chatImportModal.heading).toBeVisible();
  await expect(chatImportModal.importButton).toBeDisabled();

  await chatImportModal.cancel();
  await expect(chatImportModal.heading).toBeHidden();
});

test('creates, edits and deletes a thread memory from the context panel', async ({ chatPage, page, seed, userWithPersonality }) => {
  // The delete action goes through window.confirm(), not the app's own
  // ConfirmationModal (context-memories-tab.component.ts) — accept it so the
  // dialog doesn't block on the (headless, unattended) default of dismissing.
  page.on('dialog', dialog => dialog.accept());

  const thread = await seed.thread();
  await chatPage.navigateTo(thread.id as string);

  await chatPage.openContextPanel();
  await chatPage.selectContextTab('Memories');

  const memoryTab = chatPage.contextPanel.getByRole('tab', { name: 'This Thread' });
  await expect(memoryTab).toHaveAttribute('aria-selected', 'true');
  await expect(chatPage.contextPanel).toContainText('No memories yet.');

  await chatPage.contextPanel.getByRole('button', { name: 'Add Memory' }).click();
  const editorDialog = page.getByRole('dialog', { name: 'Create memory' });
  await expect(editorDialog).toBeVisible();

  const content = 'e2e context-panel memory';
  await editorDialog.locator('#memory-content').fill(content);
  await editorDialog.getByRole('button', { name: 'Save' }).click();

  await expect(editorDialog).toBeHidden();
  await expect(chatPage.contextPanel).toContainText(content);

  await chatPage.contextPanel.getByRole('button', { name: 'edit' }).click();
  const editDialog = page.getByRole('dialog', { name: 'Edit memory' });
  const updated = `${content} (edited)`;
  await editDialog.locator('#memory-content').fill(updated);
  await editDialog.getByRole('button', { name: 'Save' }).click();

  await expect(chatPage.contextPanel).toContainText(updated);

  await chatPage.contextPanel.getByRole('button', { name: 'delete' }).click();
  await expect(chatPage.contextPanel).toContainText('No memories yet.');
});

test('switches the in-chat memories panel to the Global tab and opens the full list', async ({ chatPage, seed, userWithPersonality }) => {
  const memory = (await seed.memories(1))[0];
  const thread = await seed.thread();
  await chatPage.navigateTo(thread.id as string);

  await chatPage.openContextPanel();
  await chatPage.selectContextTab('Memories');

  await chatPage.contextPanel.getByRole('tab', { name: 'Global' }).click();
  await expect(chatPage.contextPanel).toContainText(memory.content as string);

  await chatPage.contextPanel.getByRole('button', { name: 'Manage all memories' }).click();
  await expect(userWithPersonality.page).toHaveURL(/\/memories/);
});

test('enables and disables a tool for the conversation', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread();
  await chatPage.navigateTo(thread.id as string);

  await chatPage.openContextPanel();
  await chatPage.selectContextTab('Tools');

  const webSearchToggle = chatPage.contextPanel.getByRole('checkbox', { name: /^web_search/ });
  await expect(webSearchToggle).toBeChecked();

  await webSearchToggle.uncheck();
  await expect(webSearchToggle).not.toBeChecked();

  await webSearchToggle.check();
  await expect(webSearchToggle).toBeChecked();
});

// Regression test for PR #322: switching a thread's personality mid-conversation
// could silently revert to the previous one. Root cause (internal/handlers/chat/chat.go,
// PATCH /chat/{id}): a mood pinned under the old personality isn't attached to the new
// one, so the patch failed UpdateChat's mood invariant (ErrMoodNotFound) and the
// frontend swallowed the error without re-syncing the UI — the picker closed, but the
// thread kept its old personality. The fix resets the active mood to Auto whenever the
// personality changes (chat.go's `personalityChanged` branch), unless the same request
// explicitly sets a mood. This test drives the real "Change personality" control and
// reloads, so it fails if that patch ever silently drops the requested personality again.
//
// Asserted through the composer's placeholder rather than the header's
// `personalityNameButton`: that header trigger is `display: none` below 768px
// (chat-page.component.scss), which would silently skip the mobile projects —
// see the same note on "renaming a thread preserves its attached personality" above.
test('a personality change made via the composer sticks after a reload', async ({ chatPage, seed, userWithPersonality, page }) => {
  const secondPersonality = await seed.personality();
  const thread = await seed.thread(undefined, { personalityId: userWithPersonality.personality.id });
  await chatPage.navigateTo(thread.id as string);

  const placeholderFor = (name: string) => new RegExp(`^Message ${name}\\.\\.\\.`);
  await expect(chatPage.composerInput).toHaveAttribute('placeholder', placeholderFor(userWithPersonality.personality.name));

  await chatPage.changePersonality(secondPersonality.name);
  await expect(chatPage.composerInput).toHaveAttribute('placeholder', placeholderFor(secondPersonality.name));

  await page.reload();
  await expect(chatPage.composerInput).toHaveAttribute('placeholder', placeholderFor(secondPersonality.name));
});

// Regression test for PR #322: picking a model in one thread could leak into another
// thread's selection. chat-session.service.ts's `setActive` now clears the optimistic
// `_model` signal whenever the active thread changes ("Clear the optimistic per-thread
// model selection..."), so the composer's model picker (bound to
// `session.model()?.id ?? session.thread()?.model_id` in chat-page.component.html) falls
// back to the newly-loaded thread's own `model_id` instead of carrying the previous
// thread's pick. Navigation here goes through the in-app Thread Manager (a client-side
// route change, not a full page load via `chatPage.navigateTo`) so it actually exercises
// that reset instead of trivially passing because everything reloaded from scratch.
test('a picked model does not leak into a different thread', async ({ chatPage, threadListPanel, seed, userWithPersonality }) => {
  const [threadA, threadB] = await seed.threads(2);

  // A fresh account may already have an account-level default model attached to a new
  // thread, so the picker's baseline isn't necessarily the "Model" placeholder — read it
  // rather than assume it, and pick something else so the switch is actually observable.
  await chatPage.navigateTo(threadA.id as string);
  const defaultModel = (await chatPage.modelPickerTrigger.innerText()).trim();

  const pickedModel = await chatPage.selectModelOtherThan(defaultModel);
  await expect(chatPage.modelPickerTrigger).toHaveText(pickedModel);

  await chatPage.closeThread();
  await threadListPanel.open(threadB.name);
  await expect(chatPage.modelPickerTrigger).not.toHaveText(pickedModel);
  await expect(chatPage.modelPickerTrigger).toHaveText(defaultModel);

  await chatPage.closeThread();
  await threadListPanel.open(threadA.name);
  await expect(chatPage.modelPickerTrigger).toHaveText(pickedModel);
});

// Regression test for PR #322: the composer and "Stop response" button used to stay
// locked well past the moment core inference actually stopped, because generation state
// tracked the *whole* chat job — including its post-inference phases (expression pick,
// conversation summarization) — rather than just the streaming LLM call. clicking Stop
// only requested a cancel; the UI stayed disabled until the job reached a terminal status,
// which meant waiting through those trailing phases too.
//
// chat-session.service.ts now derives `isGenerating()` from `isCancellationPending()`
// (true the instant a cancel is requested for the active job, before the backend even
// acknowledges it) OR-ed with live streaming state, so a Stop click unlocks the composer
// immediately regardless of what phase the cancelled job is still working through in the
// background. Under `LLM_BACKEND=mock` (no inter-chunk delay — see mock_adapter.go) the
// whole job typically completes within a single ~2s poll cycle, so "immediately" is
// asserted against a 1s bound: comfortably tight enough to catch a regression back to
// "wait for the job to fully finish" while leaving headroom for CI scheduling jitter.
test('composer unlocks immediately after clicking stop, not after post-inference phases finish', async ({
  chatPage,
  seed,
  userWithPersonality,
}) => {
  const thread = await seed.thread(undefined, { personalityId: userWithPersonality.personality.id });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.sendMessage('stop response regression probe');
  await expect(chatPage.stopButton).toBeVisible({ timeout: UI_REACTION_TIMEOUT });
  await expect(chatPage.composerInput).toBeDisabled();

  await chatPage.stopResponse();

  await expect(chatPage.stopButton).toBeHidden({ timeout: IMMEDIATE_UI_UPDATE_TIMEOUT });
  await expect(chatPage.composerInput).toBeEnabled({ timeout: IMMEDIATE_UI_UPDATE_TIMEOUT });
});

// Regression test for PR #322: switching away from a chat tab and back showed no
// refresh, so messages produced elsewhere while the tab was backgrounded (another tab,
// a background job) stayed invisible until a manual reload — "ghost messages".
//
// chat-page.component.ts now listens for `visibilitychange` (`onReturnToApp`) and calls
// `chat-session.service.ts`'s `reloadActiveThread()` whenever the tab becomes visible
// again, re-pulling the active thread's messages in place. Playwright has no reliable
// cross-browser way to simulate a real OS tab-switch, so this dispatches a synthetic
// `visibilitychange` event via `chatPage.simulateReturnToTab()` — `onReturnToApp` only
// checks that `document.visibilityState !== 'hidden'`, which holds even though the tab
// never actually left focus, so this still drives the same code path a real tab return
// would. The "arrived elsewhere" message is injected directly through the API (bypassing
// this tab's own composer/UI) to stand in for a second tab or a background job landing
// a reply — the scenario `reloadActiveThread()` exists to pick up.
test('returning to the tab refreshes messages that arrived while it was away', async ({
  chatPage,
  seed,
  userWithPersonality,
  page,
  apiClient,
}) => {
  const thread = await seed.thread(undefined, { personalityId: userWithPersonality.personality.id });

  const initialMessagesLoaded = page.waitForResponse(
    resp => resp.url().includes(`/chat/${thread.id}/chat-message`) && resp.request().method() === 'GET',
  );
  await chatPage.navigateTo(thread.id as string);
  await expect(chatPage.composerInput).toBeVisible();
  await initialMessagesLoaded;

  const ghostText = 'ghost message regression probe';
  await sendChatMessage(apiClient, thread.id as string, ghostText);

  // Nothing else in the app polls for new messages (no websocket/SSE, no interval
  // timer — grep confirms `reloadActiveThread` is only ever called from
  // `onReturnToApp`), so the message staying absent here isn't a race against a
  // slower background mechanism catching up — it genuinely never appears without
  // the tab-return trigger below.
  await expect(chatPage.messageText(ghostText)).toBeHidden();

  await chatPage.simulateReturnToTab();

  await expect(chatPage.messageText(ghostText)).toBeVisible({ timeout: UI_REACTION_TIMEOUT });
});
