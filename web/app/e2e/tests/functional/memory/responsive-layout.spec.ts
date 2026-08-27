import type { Locator } from '@playwright/test';
import { test, expect } from '../../../fixtures';

const RESPONSIVE_WIDTHS = [320, 360, 375, 390, 412, 768, 1023, 1024] as const;
const VIEWPORT_HEIGHT = 900;
const LAYOUT_TOLERANCE_PX = 1;

type Rect = NonNullable<Awaited<ReturnType<Locator['boundingBox']>>>;

async function rect(locator: Locator, label: string): Promise<Rect> {
  const box = await locator.boundingBox();
  expect(box, `${label} should have a rendered bounding box`).not.toBeNull();
  return box as Rect;
}

function overlaps(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;
}

function expectNoOverlap(a: Rect, b: Rect, label: string): void {
  expect(overlaps(a, b), label).toBe(false);
}

function expectInsideViewport(box: Rect, viewportWidth: number, label: string): void {
  expect(box.x, `${label} should not extend past the left viewport edge`).toBeGreaterThanOrEqual(-LAYOUT_TOLERANCE_PX);
  expect(box.x + box.width, `${label} should not extend past the right viewport edge`).toBeLessThanOrEqual(
    viewportWidth + LAYOUT_TOLERANCE_PX,
  );
}

test.describe('Memories responsive layout contract', () => {
  test('keeps primary controls unobstructed, in-bounds, and actionable across supported widths', async ({
    memoriesPage,
    seed,
    userWithPersonality,
    page,
  }) => {
    const [memory] = await seed.memories(1);

    for (const width of RESPONSIVE_WIDTHS) {
      await test.step(`${width}px viewport`, async () => {
        await page.setViewportSize({ width, height: VIEWPORT_HEIGHT });
        await memoriesPage.navigateTo();

        await expect(memoriesPage.heading).toBeVisible();
        await expect(memoriesPage.subtitle).toBeVisible();
        await expect(memoriesPage.headerActions).toBeVisible();
        await expect(memoriesPage.filterTabs).toBeVisible();
        await expect(memoriesPage.sortSelect).toBeVisible();
        await expect(memoriesPage.card(memory.content as string)).toBeVisible();

        const headingBox = await rect(memoriesPage.heading, 'Memories heading');
        const subtitleBox = await rect(memoriesPage.subtitle, 'Memories subtitle');
        const headerCopyBox = await rect(memoriesPage.headerCopy, 'Memories header copy');
        const headerActionsBox = await rect(memoriesPage.headerActions, 'Memories header actions');
        const filterTabsBox = await rect(memoriesPage.filterTabs, 'Memory filters');
        const sortBox = await rect(memoriesPage.sortSelect, 'Memory sort control');
        const cardBox = await rect(memoriesPage.card(memory.content as string), 'Memory card');

        expectNoOverlap(headerCopyBox, headerActionsBox, 'Header copy and header actions must not overlap');

        for (const [label, box] of [
          ['Memories heading', headingBox],
          ['Memories subtitle', subtitleBox],
          ['Memories header actions', headerActionsBox],
          ['Memory filters', filterTabsBox],
          ['Memory sort control', sortBox],
          ['Memory card', cardBox],
        ] as const) {
          expectInsideViewport(box, width, label);
        }

        if (width < 1024) {
          await expect(memoriesPage.mobileMenuButton).toBeVisible();
          const menuBox = await rect(memoriesPage.mobileMenuButton, 'Mobile navigation button');
          expectInsideViewport(menuBox, width, 'Mobile navigation button');
          expectNoOverlap(menuBox, headingBox, 'Mobile navigation button must not cover the Memories heading');
          expectNoOverlap(menuBox, subtitleBox, 'Mobile navigation button must not cover the Memories subtitle');
        } else {
          await expect(memoriesPage.mobileMenuButton).toBeHidden();
        }

        await expect(memoriesPage.batchImportButton).toBeDisabled();
        await memoriesPage.mergeHistoryLink.click({ trial: true });
        await memoriesPage.compactionLogLink.click({ trial: true });
        for (const filter of ['All', 'Global', 'Personality', 'Thread', 'Summary'] as const) {
          await memoriesPage.filterTab(filter).click({ trial: true });
        }
        await memoriesPage.sortSelect.click({ trial: true });

        const mainExtent = await memoriesPage.mainContent.evaluate(element => ({
          clientWidth: element.clientWidth,
          scrollWidth: element.scrollWidth,
        }));
        expect(
          mainExtent.scrollWidth,
          `Main content should not require horizontal scrolling at ${width}px`,
        ).toBeLessThanOrEqual(mainExtent.clientWidth + LAYOUT_TOLERANCE_PX);
      });
    }
  });
});
