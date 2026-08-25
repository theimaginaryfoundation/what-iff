import { test, expect } from '../../../fixtures';
import { navSections, type NavSection } from '../../../poms';

/**
 * Sidebar navigation smoke coverage for `AppShell.clickThroughTo()` — including the
 * app/config mode switch it hides, and the mobile off-canvas drawer.
 */

const sections: NavSection[] = ['personalities', 'gallery', 'memories', 'skills', 'tools', 'jobs', 'modes'];

for (const section of sections) {
  test(`navigates to ${section} from the sidebar`, async ({ shell, userWithPersonality }) => {
    await shell.clickThroughTo(section);

    await expect(userWithPersonality.page).toHaveURL(new RegExp(`${navSections[section].route}(/|\\?|$)`));
  });
}

test('reaches the Thread Manager via the Chat quick action', async ({ shell, userWithPersonality }) => {
  await shell.clickThroughTo('memories');
  await shell.clickThroughTo('chat');

  await expect(userWithPersonality.page.getByRole('heading', { name: 'Thread Manager' })).toBeVisible();
});
