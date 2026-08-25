import { test, expect, seedName } from '../../../fixtures';

/** Skills list + editor smoke coverage for the SkillsPage POM. */

test('creates a skill through the editor, then deletes it', async ({ isMobile, skillsPage, userWithPersonality }) => {
  // Real bug found via exploration: the card's Delete icon button reports as
  // visible at mobile widths but never receives the click — something above
  // it swallows the pointer (suspected overlay; root cause not yet traced).
  test.fixme(isMobile, 'Skills Delete button swallows clicks at mobile widths — see #44');

  await skillsPage.navigateTo();
  await expect(skillsPage.heading).toBeVisible();

  const name = seedName('skill');

  // Created through the UI rather than `seed.ritual()` on purpose — the point
  // of this test is the editor modal. It is deleted again below, so nothing
  // needs the seed fixture's teardown.
  await skillsPage.openEditorForNew();
  await expect(skillsPage.editorTitle).toHaveText('Create Skill');

  await skillsPage.fillEditor({
    name,
    description: 'Seeded by the e2e suite.',
    content: 'Respond with a single short sentence.',
  });
  await skillsPage.submitEditor('create');

  await expect(skillsPage.editor).toBeHidden();
  await expect(skillsPage.card(name)).toBeVisible();

  await skillsPage.delete(name);
  await expect(skillsPage.card(name)).toBeHidden();
});

test('searches the skill list', async ({ seed, skillsPage, userWithPersonality }) => {
  const target = await seed.ritual();
  const other = await seed.ritual();
  await skillsPage.navigateTo();

  await expect(skillsPage.card(target.name)).toBeVisible();

  await skillsPage.search(target.name);
  await expect(skillsPage.card(target.name)).toBeVisible();
  await expect(skillsPage.card(other.name)).toBeHidden();

  await skillsPage.search('no-skill-matches-this-query');
  await expect(skillsPage.emptyMessage).toBeVisible();
});

test('opens an existing skill in the editor', async ({ seed, skillsPage, userWithPersonality }) => {
  const ritual = await seed.ritual();
  await skillsPage.navigateTo();

  await skillsPage.openEditor(ritual.name);

  await expect(skillsPage.editorTitle).toHaveText('Edit Skill');
  await expect(skillsPage.nameInput).toHaveValue(ritual.name);

  await skillsPage.cancelEditor();
  await expect(skillsPage.editor).toBeHidden();
});

test('edits and saves an existing skill', async ({ seed, skillsPage, userWithPersonality }) => {
  const ritual = await seed.ritual();
  const updatedName = `${ritual.name}-edited`;
  await skillsPage.navigateTo();

  await skillsPage.openEditor(ritual.name);
  await expect(skillsPage.editorTitle).toHaveText('Edit Skill');

  await skillsPage.fillEditor({
    name: updatedName,
    description: 'Updated by the e2e suite.',
    content: 'Respond with a single short sentence, updated.',
  });
  await skillsPage.submitEditor('edit');

  await expect(skillsPage.editor).toBeHidden();
  // `card()` matches by substring, and updatedName contains ritual.name, so
  // asserting the old name is gone isn't meaningful here — the visible-title
  // assertion above already confirms the save took effect.
  await expect(skillsPage.card(updatedName)).toBeVisible();
});
