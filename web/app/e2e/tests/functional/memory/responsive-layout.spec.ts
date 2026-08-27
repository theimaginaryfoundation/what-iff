import type { Locator, Page } from '@playwright/test';
import { test, expect } from '../../../fixtures';
import type { MemoriesPage } from '../../../poms';

const MOBILE_WIDTHS = [320, 360, 375, 390, 412, 768, 1023] as const;
const DESKTOP_BREAKPOINT_WIDTH = 1024;
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

async function expectNoHorizontalScroll(page: Page, width: number): Promise<void> {
  const main = page.locator('#main-content');
  const mainExtent = await main.evaluate(element => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
  }));
  expect(
    mainExtent.scrollWidth,
    `Main content should not require horizontal scrolling at ${width}px`,
  ).toBeLessThanOrEqual(mainExtent.clientWidth + LAYOUT_TOLERANCE_PX);
}

async function assertCommonLayout(memoriesPage: MemoriesPage, width: number, memoryContent: string): Promise<{
  headingBox: Rect;
  subtitleBox: Rect;
}> {
  await expect(memoriesPage.heading).toBeVisible();
  await expect(memoriesPage.subtitle).toBeVisible();
  await expect(memoriesPage.headerActions).toBeVisible();
  await expect(memoriesPage.filterTabs).toBeVisible();
  await expect(memoriesPage.sortSelect).toBeVisible();
  await expect(memoriesPage.card(memoryContent)).toBeVisible();

  const headingBox = await rect(memoriesPage.heading, 'Memories heading');
  const subtitleBox = await rect(memoriesPage.subtitle, 'Memories subtitle');
  const headerCopyBox = await rect(memoriesPage.headerCopy, 'Memories header copy');
  const headerActionsBox = await rect(memoriesPage.headerActions, 'Memories header actions');
  const filterTabsBox = await rect(memoriesPage.filterTabs, 'Memory filters');
  const sortBox = await rect(memoriesPage.sortSelect, 'Memory sort control');
  const cardBox = await rect(memoriesPage.card(memoryContent), 'Memory card');

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

  return { headingBox, subtitleBox };
}

async function prepareViewport(page: Page, memoriesPage: MemoriesPage, width: number): Promise<void> {
  await page.setViewportSize({ width, height: VIEWPORT_HEIGHT });
  await memoriesPage.navigateTo();
}

test.describe('memories responsive layout contract', () => {
  test('keeps mobile navigation and page controls unobstructed across narrow widths', async ({
    memoriesPage,
    seed,
    userWithPersonality,
    page,
  }) => {
    const [memory] = await seed.memories(1);
    const memoryContent = memory.content as string;

    for (const width of MOBILE_WIDTHS) {
      await test.step(`${width}px viewport`, async () => {
        await prepareViewport(page, memoriesPage, width);
        const { headingBox, subtitleBox } = await assertCommonLayout(memoriesPage, width, memoryContent);

        await expect(memoriesPage.mobileMenuButton).toBeVisible();
        const menuBox = await rect(memoriesPage.mobileMenuButton, 'Mobile navigation button');
        expectInsideViewport(menuBox, width, 'Mobile navigation button');
        expectNoOverlap(menuBox, headingBox, 'Mobile navigation button must not cover the Memories heading');
        expectNoOverlap(menuBox, subtitleBox, 'Mobile navigation button must not cover the Memories subtitle');
      });
    }
  });

  test('keeps the desktop breakpoint layout in-bounds without the mobile navigation control', async ({
    memoriesPage,
    seed,
    userWithPersonality,
    page,
  }) => {
    const [memory] = await seed.memories(1);
    await prepareViewport(page, memoriesPage, DESKTOP_BREAKPOINT_WIDTH);
    await assertCommonLayout(memoriesPage, DESKTOP_BREAKPOINT_WIDTH, memory.content as string);
    await expect(memoriesPage.mobileMenuButton).toBeHidden();
  });
});

test.describe('jobs responsive layout contract', () => {
  test('keeps the mobile navigation clear of the Jobs header across narrow widths', async ({ page, userWithPersonality }) => {
    for (const width of MOBILE_WIDTHS) {
      await test.step(`${width}px viewport`, async () => {
        await page.setViewportSize({ width, height: VIEWPORT_HEIGHT });
        await page.goto('/agent-jobs');

        const heading = page.getByRole('heading', { name: 'Jobs', level: 1 });
        const createJobButton = page.getByRole('button', { name: 'Create job' }).first();
        const mobileMenuButton = page.getByRole('button', { name: 'Open navigation menu' });

        await expect(heading).toBeVisible();
        await expect(createJobButton).toBeVisible();
        await expect(mobileMenuButton).toBeVisible();

        const headingBox = await rect(heading, 'Jobs heading');
        const createJobBox = await rect(createJobButton, 'Create job button');
        const menuBox = await rect(mobileMenuButton, 'Mobile navigation button');

        expectInsideViewport(headingBox, width, 'Jobs heading');
        expectInsideViewport(createJobBox, width, 'Create job button');
        expectInsideViewport(menuBox, width, 'Mobile navigation button');
        expectNoOverlap(menuBox, headingBox, 'Mobile navigation button must not cover the Jobs heading');
        expectNoOverlap(menuBox, createJobBox, 'Mobile navigation button must not cover the Create job button');
        await createJobButton.click({ trial: true });
        await expectNoHorizontalScroll(page, width);
      });
    }
  });
});
