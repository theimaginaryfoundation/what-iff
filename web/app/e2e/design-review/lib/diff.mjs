/**
 * How much of a screen actually changed.
 *
 * Kept identical, deliberately, to the comparison the rendered report runs
 * on a canvas (`assets/report.js`, `computeDiff`). There are two
 * implementations because they answer the question in two places — Node so a
 * PR comment and the JSON model can quote a number, the browser so the
 * sensitivity slider can move without a round trip — and they must not
 * disagree at the default threshold, or the comment and the report a
 * reviewer opens from it will contradict each other.
 *
 * `DEFAULT_THRESHOLD` is the single value both start from; the browser
 * imports nothing, so the constant is repeated there with a comment pointing
 * here. Any change to the rule below has to be made in both.
 */

import { decodePng } from './png.mjs';

/**
 * 8% of full channel range.
 *
 * Low enough to catch a genuine one-shade colour change, high enough that
 * subpixel antialiasing along a text edge does not light up every glyph on
 * the page and report a restyled paragraph as a redesigned screen.
 */
export const DEFAULT_THRESHOLD = 0.08;

/**
 * Compares two PNG buffers, returning `{ ratio, changed, total, width,
 * height }`, or null if either side cannot be decoded.
 *
 * Images are compared inside the union of their two sizes, each anchored at
 * the top-left. Scaling them to a common size would be the easy thing and
 * the wrong thing: when a change makes a page taller, scaling hides exactly
 * that, and every element below the insertion point reads as restyled
 * because it has been shifted. Anchored at the top-left, the region only one
 * image covers counts as changed — which is true, and is usually the most
 * important part of what happened.
 */
export function diffPngs(beforeBuffer, afterBuffer, threshold = DEFAULT_THRESHOLD) {
  const before = decodePng(beforeBuffer);
  const after = decodePng(afterBuffer);
  if (!before || !after) return null;

  const width = Math.max(before.width, after.width);
  const height = Math.max(before.height, after.height);
  const cutoff = Math.round(threshold * 255);
  let changed = 0;

  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const inBefore = x < before.width && y < before.height;
      const inAfter = x < after.width && y < after.height;
      // A pixel present in only one image is a change by definition — this
      // is what makes a height or width difference show up as a magnitude
      // rather than silently comparing nothing.
      if (!inBefore || !inAfter) {
        changed++;
        continue;
      }
      const a = (y * before.width + x) * 4;
      const b = (y * after.width + x) * 4;
      // Max per channel, not Euclidean distance: the question a designer is
      // asking is "did any channel shift noticeably", and a distance metric
      // lets a large change in one channel hide under small ones elsewhere.
      // Alpha is included so a region that became translucent still counts.
      const delta = Math.max(
        Math.abs(before.data[a] - after.data[b]),
        Math.abs(before.data[a + 1] - after.data[b + 1]),
        Math.abs(before.data[a + 2] - after.data[b + 2]),
        Math.abs(before.data[a + 3] - after.data[b + 3]),
      );
      if (delta > cutoff) changed++;
    }
  }

  return { ratio: changed / (width * height), changed, total: width * height, width, height, threshold };
}
