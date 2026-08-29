import type { Locator, Page } from '@playwright/test';
import { expect } from '@playwright/test';

export const RESPONSIVE_WIDTHS = [320, 360, 375, 390, 412, 768, 1023] as const;
export const DEFAULT_VIEWPORT_HEIGHT = 900;
export const LAYOUT_TOLERANCE_PX = 1;

export type Rect = NonNullable<Awaited<ReturnType<Locator['boundingBox']>>>;

export async function rect(locator: Locator, label: string): Promise<Rect> {
  const box = await locator.boundingBox();
  expect(box, `${label} should have a rendered bounding box`).not.toBeNull();
  return box as Rect;
}

export function expectInsideViewport(
  box: Rect,
  viewportWidth: number,
  label: string,
  viewportHeight?: number,
): void {
  expect(box.x, `${label} should not extend past the left viewport edge`).toBeGreaterThanOrEqual(-LAYOUT_TOLERANCE_PX);
  expect(box.x + box.width, `${label} should not extend past the right viewport edge`).toBeLessThanOrEqual(
    viewportWidth + LAYOUT_TOLERANCE_PX,
  );

  if (viewportHeight !== undefined) {
    expect(box.y, `${label} should not extend past the top viewport edge`).toBeGreaterThanOrEqual(-LAYOUT_TOLERANCE_PX);
    expect(box.y + box.height, `${label} should not extend past the bottom viewport edge`).toBeLessThanOrEqual(
      viewportHeight + LAYOUT_TOLERANCE_PX,
    );
  }
}

export function expectInside(container: Rect, child: Rect, label: string): void {
  expect(child.x, `${label} should stay inside its container on the left`).toBeGreaterThanOrEqual(
    container.x - LAYOUT_TOLERANCE_PX,
  );
  expect(child.x + child.width, `${label} should stay inside its container on the right`).toBeLessThanOrEqual(
    container.x + container.width + LAYOUT_TOLERANCE_PX,
  );
  expect(child.y, `${label} should stay inside its container at the top`).toBeGreaterThanOrEqual(
    container.y - LAYOUT_TOLERANCE_PX,
  );
  expect(child.y + child.height, `${label} should stay inside its container at the bottom`).toBeLessThanOrEqual(
    container.y + container.height + LAYOUT_TOLERANCE_PX,
  );
}

export function expectBalancedHorizontalInsets(
  container: Rect,
  first: Rect,
  last: Rect,
  label: string,
  tolerancePx = 4,
): void {
  const leftInset = first.x - container.x;
  const rightInset = container.x + container.width - (last.x + last.width);

  expect(leftInset, `${label} should have a non-negative left inset`).toBeGreaterThanOrEqual(-LAYOUT_TOLERANCE_PX);
  expect(rightInset, `${label} should have a non-negative right inset`).toBeGreaterThanOrEqual(-LAYOUT_TOLERANCE_PX);
  expect(
    Math.abs(leftInset - rightInset),
    `${label} should remain horizontally balanced within its container`,
  ).toBeLessThanOrEqual(tolerancePx);
}

export async function expectNoHorizontalScroll(page: Page, width: number, selector = '#main-content'): Promise<void> {
  const root = page.locator(selector);
  const extent = await root.evaluate(element => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
  }));

  expect(extent.scrollWidth, `${selector} should not require horizontal scrolling at ${width}px`).toBeLessThanOrEqual(
    extent.clientWidth + LAYOUT_TOLERANCE_PX,
  );
}
