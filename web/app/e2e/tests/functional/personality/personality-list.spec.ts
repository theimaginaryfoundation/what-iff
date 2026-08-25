import { test, expect } from '../../../fixtures';

/**
 * Personalities list page (`/personality`) smoke coverage for the
 * PersonalitiesPage POM's card-grid affordances.
 */

// Regression test for PR #138: previously the only way to change the account's
// default personality was to leave this list for Settings. PR #138 added a
// "Make default" star button directly on each non-default card, plus a
// "Default" badge on whichever card currently holds it. Unit coverage exists
// (personality-list.component.spec.ts's isDefaultPersonality/makeDefaultPersonality),
// but nothing previously drove the real list-page UI end to end.
test('makes a personality the account default from the list page', { tag: '@mutates-account' }, async ({
  personalitiesPage,
  seed,
  userWithPersonality,
}) => {
  // @mutates-account: default_personality_id lives on the account's own
  // UserPreferences, not on the personality — same category as
  // personality-detail.spec.ts's "makes a personality the account default".
  //
  // The fixture's own personality claimed "default" on creation (it's the
  // account's first), so a second, freshly seeded one is guaranteed not to
  // be — the star button only renders for a non-default card
  // (personality-card.component.ts).
  const extra = await seed.personality();
  await personalitiesPage.navigateTo();

  await expect(personalitiesPage.defaultBadge(userWithPersonality.personality.name)).toBeVisible();
  await expect(personalitiesPage.makeDefaultButton(extra.name)).toBeVisible();

  await personalitiesPage.makeDefault(extra.name);

  await expect(personalitiesPage.defaultBadge(extra.name)).toBeVisible();
  await expect(personalitiesPage.makeDefaultButton(userWithPersonality.personality.name)).toBeVisible();
});
