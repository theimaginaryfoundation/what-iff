import { test, expect, seedName } from '../../../fixtures';
import {
  DEFAULT_VIEWPORT_HEIGHT,
  RESPONSIVE_WIDTHS,
  expectHorizontallyInside,
  expectInsideViewport,
  rect,
} from '../../../helpers/responsive-layout';

/** Mode/mood editor (`/mode`) coverage for the ModeEditModalPage POM. */

test('prompts to discard unsaved changes on backdrop dismissal', async ({ modeEditModalPage, confirmationModal, userWithPersonality }) => {
  const originalName = seedName('mode');

  await modeEditModalPage.navigateTo();
  const id = await modeEditModalPage.createViaUi({ name: originalName, description: 'Original description.' });
  await expect(modeEditModalPage.panel).toBeHidden();

  await modeEditModalPage.openEditById(id);
  await expect(modeEditModalPage.nameInput).toHaveValue(originalName);
  await modeEditModalPage.fillForm({ name: `${originalName}-edited` });

  await modeEditModalPage.clickBackdrop();
  await expect(confirmationModal.dialog).toBeVisible();
  await expect(confirmationModal.headingText).toHaveText('Discard changes?');
  await confirmationModal.cancel('Keep editing');
  await expect(modeEditModalPage.panel).toBeVisible();
  await expect(modeEditModalPage.nameInput).toHaveValue(`${originalName}-edited`);

  await modeEditModalPage.clickBackdrop();
  await expect(confirmationModal.dialog).toBeVisible();
  await confirmationModal.confirm('Discard');
  await expect(modeEditModalPage.panel).toBeHidden();

  await modeEditModalPage.openEditById(id);
  await expect(modeEditModalPage.nameInput).toHaveValue(originalName);
});

test('closes after a successful save', async ({ modeEditModalPage, userWithPersonality }) => {
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

test('keeps the create modal and its controls contained across mobile widths', async ({
  modeEditModalPage,
  page,
  userWithPersonality,
}) => {
  await page.setViewportSize({ width: RESPONSIVE_WIDTHS[0], height: DEFAULT_VIEWPORT_HEIGHT });
  await modeEditModalPage.navigateTo();

  for (const width of RESPONSIVE_WIDTHS) {
    await test.step(`${width}px viewport`, async () => {
      await page.setViewportSize({ width, height: DEFAULT_VIEWPORT_HEIGHT });
      await modeEditModalPage.openCreate();
      await expect(modeEditModalPage.panel).toBeVisible();

      const panelBox = await rect(modeEditModalPage.panel, 'Mode create dialog');
      expectInsideViewport(panelBox, width, 'Mode create dialog', DEFAULT_VIEWPORT_HEIGHT);

      const controls = modeEditModalPage.panel.locator('input:visible, textarea:visible, select:visible, button:visible');
      const controlCount = await controls.count();
      for (let index = 0; index < controlCount; index += 1) {
        const controlBox = await rect(controls.nth(index), `Mode dialog control ${index + 1}`);
        expectHorizontallyInside(panelBox, controlBox, `Mode dialog control ${index + 1}`);
        expectInsideViewport(controlBox, width, `Mode dialog control ${index + 1}`);
      }

      await modeEditModalPage.closeButton.click();
      await expect(modeEditModalPage.panel).toBeHidden();
    });
  }
});

test('keeps the create modal usable when mobile viewport height is constrained', async ({
  modeEditModalPage,
  page,
  userWithPersonality,
}) => {
  const width = 390;
  const height = 560;

  await page.setViewportSize({ width, height });
  await modeEditModalPage.navigateTo();
  await modeEditModalPage.openCreate();
  await expect(modeEditModalPage.panel).toBeVisible();
  await modeEditModalPage.descriptionInput.focus();

  const panelBox = await rect(modeEditModalPage.panel, 'Mode create dialog');
  expectInsideViewport(panelBox, width, 'Mode create dialog', height);

  const overflow = await modeEditModalPage.panel.evaluate(element => ({
    clientHeight: element.clientHeight,
    scrollHeight: element.scrollHeight,
    overflowY: getComputedStyle(element).overflowY,
  }));
  expect(overflow.scrollHeight).toBeGreaterThan(overflow.clientHeight);
  expect(['auto', 'scroll']).toContain(overflow.overflowY);
  await modeEditModalPage.saveButton.scrollIntoViewIfNeeded();
  await modeEditModalPage.saveButton.click({ trial: true });
});
