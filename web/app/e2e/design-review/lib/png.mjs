/**
 * Just enough PNG to know how big a baseline is.
 *
 * The report needs every screenshot's pixel dimensions so the HTML can
 * reserve the right aspect ratio before the image decodes (otherwise a
 * gallery of a dozen screens reflows as each one lands) and so a
 * desktop/mobile pair can be told apart by shape rather than by trusting
 * the filename.
 *
 * That is the *only* thing needed from the format, so this reads the IHDR
 * header rather than pulling in a decoder. Playwright writes plain
 * truecolour-with-alpha PNGs whose IHDR is always the first chunk, which
 * the spec requires of every encoder, so the fixed offsets below are not a
 * shortcut around a general case — there is no general case to handle.
 *
 * Actual pixel comparison deliberately does NOT happen here; it happens in
 * the browser on a canvas (see render.mjs). Diffing in Node would mean a
 * full PNG decoder as a new dependency, and the result would be a static
 * image; doing it client-side gets the slider, the onion-skin and the
 * adjustable threshold for free from data the report has already loaded.
 */

const SIGNATURE = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

/**
 * Reads `{ width, height }` from a PNG buffer, or returns null for anything
 * that isn't one.
 *
 * Returns null rather than throwing: a malformed or truncated file here is
 * a baseline that someone's git-lfs or merge tool mangled, and the report
 * is far more useful if it renders that screen with a "couldn't read this
 * image" note than if the whole run dies on it.
 */
export function readPngSize(buffer) {
  // 8 signature + 4 length + 4 "IHDR" + 8 (width, height) = 24 bytes minimum.
  if (!Buffer.isBuffer(buffer) || buffer.length < 24) return null;
  if (!buffer.subarray(0, 8).equals(SIGNATURE)) return null;
  if (buffer.subarray(12, 16).toString('ascii') !== 'IHDR') return null;

  const width = buffer.readUInt32BE(16);
  const height = buffer.readUInt32BE(20);
  // A zero dimension is legal to encode but not legal PNG; treat it as
  // unreadable rather than emitting an aspect ratio that divides by zero.
  if (width === 0 || height === 0) return null;
  return { width, height };
}
