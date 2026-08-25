import { test, expect } from '../../../fixtures';
import { buildChatgptExport } from '../../../fixtures/chatgpt-export';

/**
 * Chat import coverage against a small, in-memory ChatGPT `conversations.json` export — driving
 * the real `<input type="file">` in `ChatImportModal` against the mock/local backend. The backend
 * import path (`internal/handlers/chat/import.go`) parses the export entirely locally (no
 * OpenAI/Claude API calls), so this is hermetically testable, unlike personality file attachments
 * (see `TEST_PLAN.md`).
 *
 * Deliberately out of scope: the >60MB client-side chunking path — not practical to fixture a file
 * that large in a test.
 */

// Regression test for PR #326/#327: a parsing-phase progress payload (which intentionally carries
// no counts) was being treated as a real zero-count update and overwrote the prior real total, so
// a successful import could still show "Imported 0 threads" in the UI. Also covers PR #309/#304
// (non-chunked imports reporting 0 due to the same progress-count parsing bug): this fixture is
// well under the chunk-size threshold, so the whole file uploads as a single, non-chunked request.
test('reports the correct imported count, not zero', async ({ chatImportModal, threadListPanel, userWithPersonality }) => {
  const { titles, file } = buildChatgptExport(3);

  await threadListPanel.navigateTo();
  await threadListPanel.openImport();
  await expect(chatImportModal.heading).toBeVisible();

  await chatImportModal.chooseFile(file);
  await expect(chatImportModal.importButton).toBeEnabled();
  await chatImportModal.startImport();

  // A successful import with imported > 0 goes straight to the post-import picker rather than the
  // 'done' summary; skip it to reach the summary text this test is really about.
  await expect(chatImportModal.pickerCandidateRows).toHaveCount(titles.length);
  await chatImportModal.skipPicker();

  await expect(chatImportModal.completeMessage).toBeVisible();
  await expect(chatImportModal.resultDetail).toContainText(`Imported ${titles.length} threads`);
});

// Regression test for PR #326/#327: the post-import "select threads to prepare" picker queried
// listChatsPage() with no `ids` filter, so it pulled the account's most-recent archived threads —
// including ones that had nothing to do with the current import run — instead of scoping to just
// the threads this import created.
test('the post-import picker only offers threads from this import run', async ({
  chatImportModal,
  seed,
  threadListPanel,
  userWithPersonality,
}) => {
  const unrelated = await seed.thread();
  await threadListPanel.navigateTo();
  await threadListPanel.narrowTo(unrelated.name);
  await threadListPanel.archive(unrelated.name);

  const { titles, file } = buildChatgptExport(2);
  await threadListPanel.openImport();
  await chatImportModal.chooseFile(file);
  await expect(chatImportModal.importButton).toBeEnabled();
  await chatImportModal.startImport();

  await expect(chatImportModal.pickerCandidateRows).toHaveCount(titles.length);
  for (const title of titles) {
    await expect(chatImportModal.candidateRow(title)).toBeVisible();
  }
  await expect(chatImportModal.candidateRow(unrelated.name)).toHaveCount(0);
});
