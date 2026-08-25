import { test, expect } from '../../../fixtures';

/** Command palette smoke coverage for the CommandPalette POM. */

test('opens from the sidebar and finds a seeded thread', async ({ commandPalette, seed, threadListPanel, userWithPersonality }) => {
  const thread = await seed.thread();

  await threadListPanel.navigateTo();
  await commandPalette.open();

  await expect(commandPalette.dialog).toBeVisible();
  await expect(commandPalette.input).toBeFocused();

  await commandPalette.search(thread.name);
  await expect(commandPalette.option(new RegExp(thread.name))).toBeVisible();
});

test('closes on Escape', async ({ commandPalette, userWithPersonality }) => {
  await commandPalette.open();
  await expect(commandPalette.dialog).toBeVisible();

  await commandPalette.dismiss();
  await expect(commandPalette.dialog).toBeHidden();
});

test('choosing a result navigates to that thread and closes the palette', async ({ commandPalette, seed, threadListPanel, userWithPersonality }) => {
  const thread = await seed.thread();

  await threadListPanel.navigateTo();
  await commandPalette.open();
  await commandPalette.search(thread.name);
  await expect(commandPalette.option(new RegExp(thread.name))).toBeVisible();

  await commandPalette.chooseOption(new RegExp(thread.name));

  await expect(commandPalette.dialog).toBeHidden();
  await expect(userWithPersonality.page).toHaveURL(new RegExp(`/chat/${thread.id}`));
});

test('shows an empty state for a query that matches nothing', async ({ commandPalette, userWithPersonality }) => {
  await commandPalette.open();

  await commandPalette.search('no-entity-matches-this-query-at-all');
  await expect(commandPalette.emptyMessage).toBeVisible();
  await expect(commandPalette.options).toHaveCount(0);
});
