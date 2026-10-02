/**
 * Decoder and pixel-comparison tests.
 *
 * This is the code most able to be confidently wrong: a decoder that
 * mishandles one filter type still returns an image, and a comparison that
 * mis-scales still returns a percentage. Both would flow straight into a PR
 * comment as a number nobody could tell was nonsense.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { decodePng, readPngSize } from '../lib/png.mjs';
import { diffPngs, DEFAULT_THRESHOLD } from '../lib/diff.mjs';
import { makePng } from './png-fixture.mjs';

const COLOUR_TYPES = [0, 2, 4, 6];
const FILTERS = [0, 1, 2, 3, 4];
const CARRIES_COLOUR = new Set([2, 6]);
const CARRIES_ALPHA = new Set([4, 6]);

test('every colour type and row filter decodes back to the pixels encoded', () => {
  for (const colourType of COLOUR_TYPES) {
    for (const filter of FILTERS) {
      const image = decodePng(makePng(9, 7, [180, 90, 45, 200], { colourType, filter }));
      assert.ok(image, `colour type ${colourType}, filter ${filter} failed to decode`);
      assert.equal(image.data.length, 9 * 7 * 4, 'output is always widened to RGBA');

      // Sample a pixel away from the edges, where the left/above predictors
      // are all in play rather than clamped to zero.
      const pixel = [...image.data.subarray((4 * 9 + 5) * 4, (4 * 9 + 5) * 4 + 4)];
      assert.deepEqual(
        pixel,
        [...(CARRIES_COLOUR.has(colourType) ? [180, 90, 45] : [180, 180, 180]), CARRIES_ALPHA.has(colourType) ? 200 : 255],
        `colour type ${colourType}, filter ${filter}`,
      );
    }
  }
});

test('shapes outside what a screenshot can be decode to null, not to garbage', () => {
  // Palette (3) needs PLTE to mean anything. Returning null lets callers show
  // "couldn't read this" instead of a plausible-looking wrong image.
  const palette = Buffer.from(makePng(4, 4));
  palette[25] = 3;
  assert.equal(decodePng(palette), null);

  const interlaced = Buffer.from(makePng(4, 4));
  interlaced[28] = 1;
  assert.equal(decodePng(interlaced), null);

  assert.equal(decodePng(Buffer.from('not a png')), null);
  // Truncated mid-IDAT — what a mangled binary merge leaves behind.
  assert.equal(decodePng(makePng(64, 64).subarray(0, 40)), null);
});

test('identical images differ by nothing; inverted ones by everything', () => {
  const black = makePng(20, 10, [0, 0, 0]);
  assert.equal(diffPngs(black, makePng(20, 10, [0, 0, 0])).ratio, 0);
  assert.equal(diffPngs(black, makePng(20, 10, [255, 255, 255])).ratio, 1);
});

test('a change below the sensitivity threshold does not count', () => {
  const base = makePng(20, 10, [100, 100, 100]);
  const cutoff = Math.round(DEFAULT_THRESHOLD * 255);

  // Exactly at the cutoff is not "greater than", so it stays uncounted —
  // this is the boundary the browser's copy of the rule has to match.
  assert.equal(diffPngs(base, makePng(20, 10, [100 + cutoff, 100, 100])).ratio, 0);
  assert.equal(diffPngs(base, makePng(20, 10, [100 + cutoff + 1, 100, 100])).ratio, 1);
});

test('the comparison is per channel, so a shift in one is not diluted by the others', () => {
  const base = makePng(8, 8, [100, 100, 100]);
  // One channel moves well past the threshold while the other two are
  // identical. A distance metric averaged over channels could miss this.
  assert.equal(diffPngs(base, makePng(8, 8, [100, 160, 100])).ratio, 1);
});

test('a taller image counts the region only one side covers as changed', () => {
  const short = makePng(10, 10, [30, 30, 30]);
  const tall = makePng(10, 20, [30, 30, 30]);

  const result = diffPngs(short, tall);

  // The overlapping 10x10 is identical; the 10 rows only the tall image has
  // are the change. Scaling to a common size would have reported 0% and
  // hidden a doubling in page height.
  assert.equal(result.width, 10);
  assert.equal(result.height, 20);
  assert.equal(result.changed, 100);
  assert.equal(result.ratio, 0.5);
});

test('an undecodable side yields null rather than a misleading zero', () => {
  assert.equal(diffPngs(makePng(4, 4), Buffer.from('junk')), null);
});

test('readPngSize and decodePng agree on dimensions', () => {
  const png = makePng(37, 19, [1, 2, 3], { colourType: 2, filter: 4 });
  const decoded = decodePng(png);
  assert.deepEqual(readPngSize(png), { width: decoded.width, height: decoded.height });
});
