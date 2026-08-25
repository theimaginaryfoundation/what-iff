import { test, expect, seedName } from '../../../fixtures';

/** Mode/mood editor (`/mode`) coverage for the ModeEditModalPage POM. */

test('prompts to discard unsaved changes on backdrop dismissal', async ({ modeEditModalPage, confirmationModal, userWithPersonality }) => {
  // Regression test for PR #289: backdrop-click/Escape used to discard
  // unsaved edits silently. ModeEditModalComponent's backdrop handler and its
  // `document:keydown.escape` listener both emit `dismissRequested`, which
  // MoodListFacade.requestCloseModeModal() now guards behind
  // ConfirmationService.confirmDiscardChanges() whenever the form is dirty
  // (mood-list.facade.ts). Unit coverage exists for the shared modal system
  // (modal.component.spec.ts, mode-edit-modal.component.spec.ts), but nothing
  // previously exercised this flow end-to-end.
  const originalName = seedName('mode');

  await modeEditModalPage.navigateTo();
  const id = await modeEditModalPage.createViaUi({ name: originalName, description: 'Original description.' });
  await expect(modeEditModalPage.panel).toBeHidden();

  await modeEditModalPage.openEditById(id);
  await expect(modeEditModalPage.nameInput).toHaveValue(originalName);
  await modeEditModalPage.fillForm({ name: `${originalName}-edited` });

  // Cancelling the discard prompt keeps the modal open with the edit intact.
  await modeEditModalPage.clickBackdrop();
  await expect(confirmationModal.dialog).toBeVisible();
  await expect(confirmationModal.headingText).toHaveText('Discard changes?');
  await confirmationModal.cancel('Keep editing');
  await expect(modeEditModalPage.panel).toBeVisible();
  await expect(modeEditModalPage.nameInput).toHaveValue(`${originalName}-edited`);

  // Confirming it closes the modal and discards the edit.
  await modeEditModalPage.clickBackdrop();
  await expect(confirmationModal.dialog).toBeVisible();
  await confirmationModal.confirm('Discard');
  await expect(modeEditModalPage.panel).toBeHidden();

  await modeEditModalPage.openEditById(id);
  await expect(modeEditModalPage.nameInput).toHaveValue(originalName);
});

test('closes after a successful save', async ({ modeEditModalPage, userWithPersonality }) => {
  // Regression test for PR #289: the modal used to stay open after a
  // successful save instead of closing. There was zero test coverage — not
  // even a unit test — asserting the post-save close. MoodListFacade.saveEditMood()
  // now calls closeModeModal(true) in its success handler.
  const name = seedName('mode');

  await modeEditModalPage.navigateTo();
  const id = await modeEditModalPage.createViaUi({ name, description: 'Original description.' });
  await expect(modeEditModalPage.panel).toBeHidden();

  await modeEditModalPage.openEditById(id);
  await expect(modeEditModalPage.nameInput).toHaveValue(name);
  await modeEditModalPage.fillForm({ description: 'Updated description.' });
  await modeEditModalPage.save();

  await expect(modeEditModalPage.panel).toBeHidden();
});
