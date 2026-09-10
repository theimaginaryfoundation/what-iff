import { test, expect, shortId } from '../../../fixtures';
import { deleteChat, sendChatMessage } from '../../../sdk/client';
import { readDownloadedZip, parseJsonl } from '../../../helpers/zip';

/**
 * Exporting one thread from the chat page, through a real download.
 *
 * The API-level counterpart (`e2e/api-tests/chat-export.spec.ts`) already
 * pins what the endpoint returns. What only a browser can answer is what
 * happens to those bytes afterwards: the app builds a blob URL, clicks it,
 * and revokes the URL in the same turn, so the download either lands intact
 * or does not land at all, and nothing in between is visible from an HTTP
 * test.
 */

test.describe('chat export', () => {
  test('downloads a valid archive named after the thread', async ({ chatPage, page, seed, apiClient, userWithPersonality }, testInfo) => {
    /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
    test.skip(testInfo.project.name !== 'chromium-desktop', 'Export is desktop-only chrome.');

    const thread = await seed.thread();
    // Without a message, messages.jsonl is empty and the archive cannot show
    // that message streaming survived the download.
    await sendChatMessage(apiClient, thread.id as string, 'hello export');
    await chatPage.navigateTo(thread.id as string);

    // Registered before the click: a download that fired first would be missed.
    const downloadPromise = page.waitForEvent('download');
    await chatPage.exportThread();
    const download = await downloadPromise;

    expect(download.suggestedFilename()).toBe(`${thread.name}.zip`);

    const entries = await readDownloadedZip(download);
    expect(entries.map(e => e.name)).toStrictEqual(['chat.json', 'messages.jsonl']);

    const metadata = JSON.parse(entries[0]!.text) as { id?: string };
    expect(metadata.id).toBe(thread.id);
    expect(parseJsonl(entries[1]!.text).length).toBeGreaterThan(0);
  });

  test('keeps a non-ASCII thread name intact in the downloaded filename', async ({ chatPage, page, seed, userWithPersonality }, testInfo) => {
    /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
    test.skip(testInfo.project.name !== 'chromium-desktop', 'Export is desktop-only chrome.');

    // RED. The server sends the name in a bare `filename=` parameter, which is
    // latin-1 by the letter of RFC 6266, so a browser decodes these bytes as
    // ISO-8859-1 and the user gets mojibake. The fix is `filename*=` with
    // UTF-8, which the client must then prefer.
    test.fail(true, 'Content-Disposition carries no filename* parameter, so non-ASCII names arrive mojibaked');

    const name = `Café ☕ 日本語 ${shortId()}`;
    const thread = await seed.thread(name);
    await chatPage.navigateTo(thread.id as string);

    const downloadPromise = page.waitForEvent('download');
    await chatPage.exportThread();
    const download = await downloadPromise;

    expect(download.suggestedFilename()).toBe(`${name}.zip`);
    // 'Ã' and 'Â' are what UTF-8 bytes look like once read as ISO-8859-1, so
    // naming them tells a future reader what the failure was, not just that
    // two strings differed.
    expect(download.suggestedFilename()).not.toMatch(/[ÃÂ]/);
  });

  test('surfaces the server message when the thread is gone', async ({ chatPage, seed, apiClient, page, userWithPersonality }, testInfo) => {
    /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
    test.skip(testInfo.project.name !== 'chromium-desktop', 'Export is desktop-only chrome.');

    // RED. The export response is read as a blob whatever the status, so the
    // 404's JSON envelope never reaches the error formatter and the user is
    // told 'An error occurred' instead of what actually happened.
    test.fail(true, 'A failed export reports a generic message because the error body is read as a blob');

    const thread = await seed.thread();
    await chatPage.navigateTo(thread.id as string);
    await expect(chatPage.headingText).toHaveText(thread.name);

    // Deleted underneath the open page rather than through the UI, so the
    // page state stays exactly as the user left it.
    await deleteChat(apiClient, thread.id as string);

    await chatPage.exportThread();

    await expect(chatPage.statusMessage).toContainText('Chat not found');
    // An unrelated 4xx must not be mistaken for an expired session.
    await expect(page).not.toHaveURL(/\/auth\/login/);
  });

  test('offers no export affordance below 1024px', async ({ chatPage, page, seed, userWithPersonality }, testInfo) => {
    /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
    test.skip(testInfo.project.name === 'chromium-desktop', 'Covered by the desktop export specs.');

    const thread = await seed.thread();
    await chatPage.navigateTo(thread.id as string);
    await expect(chatPage.headingText).toHaveText(thread.name);

    await expect(chatPage.exportButton).toBeHidden();
    // Deliberately pins a product gap rather than a feature: if a mobile
    // export affordance is ever added, this fails and forces the decision to
    // be made explicitly instead of the gap closing unnoticed.
    await expect(page.getByRole('button', { name: /export/i })).toBeHidden();
  });
});
