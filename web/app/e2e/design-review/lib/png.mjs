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

import { inflateSync } from 'node:zlib';

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

/**
 * Channel count per colour type, for the non-palette types.
 *
 * Palette (3) is absent deliberately: it needs the PLTE and tRNS chunks to
 * mean anything, no browser emits it for a screenshot, and supporting it
 * would be code that never runs in production and is therefore never known
 * to work. `decodePng` returns null for it, and callers already handle null.
 */
const CHANNELS = { 0: 1, 2: 3, 4: 2, 6: 4 };

/**
 * Reverses one scanline's filter, in place.
 *
 * The five filter types are defined in terms of the byte to the left (`a`),
 * the byte above (`b`) and the byte above-left (`c`), where anything off the
 * edge of the image is zero. Working directly on the output buffer means the
 * already-unfiltered previous row is exactly where `b` needs to read from.
 */
function unfilter(type, row, previous, bytesPerPixel) {
  const width = row.length;
  switch (type) {
    case 0:
      return;
    case 1: // Sub — predict from the pixel to the left.
      for (let i = bytesPerPixel; i < width; i++) row[i] = (row[i] + row[i - bytesPerPixel]) & 0xff;
      return;
    case 2: // Up — predict from the pixel above.
      if (previous) for (let i = 0; i < width; i++) row[i] = (row[i] + previous[i]) & 0xff;
      return;
    case 3: // Average — the mean of left and above, rounded down.
      for (let i = 0; i < width; i++) {
        const left = i >= bytesPerPixel ? row[i - bytesPerPixel] : 0;
        const above = previous ? previous[i] : 0;
        row[i] = (row[i] + ((left + above) >> 1)) & 0xff;
      }
      return;
    case 4: // Paeth — whichever of left/above/above-left is closest to their linear estimate.
      for (let i = 0; i < width; i++) {
        const left = i >= bytesPerPixel ? row[i - bytesPerPixel] : 0;
        const above = previous ? previous[i] : 0;
        const aboveLeft = previous && i >= bytesPerPixel ? previous[i - bytesPerPixel] : 0;
        const estimate = left + above - aboveLeft;
        const dLeft = Math.abs(estimate - left);
        const dAbove = Math.abs(estimate - above);
        const dAboveLeft = Math.abs(estimate - aboveLeft);
        const predicted = dLeft <= dAbove && dLeft <= dAboveLeft ? left : dAbove <= dAboveLeft ? above : aboveLeft;
        row[i] = (row[i] + predicted) & 0xff;
      }
      return;
    default:
      throw new Error(`unknown PNG filter type ${type}`);
  }
}

/**
 * Decodes a PNG to `{ width, height, data }` with `data` as RGBA bytes, or
 * null for anything outside the shape screenshots actually take (8-bit,
 * non-interlaced, non-palette).
 *
 * This exists so the changed-pixel figure can be computed in Node instead of
 * only in the browser. Without it the only place a diff ratio existed was
 * inside the rendered report, which meant a PR comment could say that a
 * screen changed but not by how much — and "something changed" without a
 * magnitude is the kind of notification people learn to ignore.
 *
 * Chromium writes colour type 2 (no alpha) for screenshots, so that is the
 * path that matters; the others are handled because doing so is a few lines
 * and the alternative is a null return that reads like a bug.
 */
export function decodePng(buffer) {
  const size = readPngSize(buffer);
  if (!size) return null;

  const bitDepth = buffer[24];
  const colourType = buffer[25];
  const interlace = buffer[28];
  const channels = CHANNELS[colourType];
  // Interlaced PNGs store seven separate reduced images; screenshots are
  // never interlaced and handling Adam7 for a case that does not arise
  // would be untested code by construction.
  if (bitDepth !== 8 || !channels || interlace !== 0) return null;

  // IDAT may be split across any number of chunks, and zlib requires them
  // concatenated before inflation — a per-chunk inflate fails on the second
  // chunk of any image large enough to have one, which is most of them.
  const parts = [];
  let offset = 8;
  while (offset + 8 <= buffer.length) {
    const length = buffer.readUInt32BE(offset);
    const type = buffer.subarray(offset + 4, offset + 8).toString('ascii');
    if (type === 'IDAT') parts.push(buffer.subarray(offset + 8, offset + 8 + length));
    if (type === 'IEND') break;
    offset += 12 + length; // length + type + data + CRC
  }
  if (!parts.length) return null;

  let raw;
  try {
    raw = inflateSync(Buffer.concat(parts));
  } catch {
    // A corrupt or truncated image: the report should say so on that one
    // screen rather than abort the run.
    return null;
  }

  const { width, height } = size;
  const bytesPerPixel = channels;
  const stride = width * channels;
  if (raw.length < height * (stride + 1)) return null;

  const data = Buffer.alloc(width * height * 4);
  let previous = null;
  const row = Buffer.alloc(stride);
  for (let y = 0; y < height; y++) {
    const start = y * (stride + 1);
    raw.copy(row, 0, start + 1, start + 1 + stride);
    unfilter(raw[start], row, previous, bytesPerPixel);

    // Widen to RGBA so every caller compares the same four channels,
    // whatever the source encoding was.
    for (let x = 0; x < width; x++) {
      const from = x * channels;
      const to = (y * width + x) * 4;
      if (channels >= 3) {
        data[to] = row[from];
        data[to + 1] = row[from + 1];
        data[to + 2] = row[from + 2];
        data[to + 3] = channels === 4 ? row[from + 3] : 255;
      } else {
        // Greyscale, with or without alpha.
        data[to] = data[to + 1] = data[to + 2] = row[from];
        data[to + 3] = channels === 2 ? row[from + 1] : 255;
      }
    }
    previous = Buffer.from(row);
  }

  return { width, height, data };
}
