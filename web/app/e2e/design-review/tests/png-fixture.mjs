/**
 * Builds a tiny, valid PNG in memory, for tests.
 *
 * The alternative — copying a committed baseline into a fixture repo —
 * would tie every assertion here to whatever the app's login page happens
 * to look like, so a routine baseline update would break tests that have
 * nothing to do with the app. A generated image lets a test say "8x8, red"
 * and mean exactly that.
 *
 * Encodes the one PNG shape this tool ever reads: 8-bit truecolour with
 * alpha, no interlacing, a single IDAT.
 */

import { deflateSync } from 'node:zlib';

const SIGNATURE = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

/** CRC-32 over type+data, as every PNG chunk requires. */
function crc32(buffer) {
  let crc = ~0;
  for (const byte of buffer) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
  }
  return ~crc >>> 0;
}

function chunk(type, data) {
  const typed = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(typed));
  return Buffer.concat([length, typed, checksum]);
}

/** Channels per colour type, matching the decoder's own table. */
const CHANNELS = { 0: 1, 2: 3, 4: 2, 6: 4 };

/**
 * Applies a PNG row filter, encoding-direction.
 *
 * The decoder's hardest code is the five filter reversals, and a fixture
 * that only ever emitted filter 0 would leave four of them untested while
 * looking like coverage. Real screenshots use all five — Chromium picks per
 * scanline — so the fixture can emit any of them and the tests round-trip
 * each one.
 */
function applyFilter(type, row, previous, bytesPerPixel) {
  const out = Buffer.from(row);
  for (let i = row.length - 1; i >= 0; i--) {
    const left = i >= bytesPerPixel ? row[i - bytesPerPixel] : 0;
    const above = previous ? previous[i] : 0;
    const aboveLeft = previous && i >= bytesPerPixel ? previous[i - bytesPerPixel] : 0;
    let predictor = 0;
    if (type === 1) predictor = left;
    else if (type === 2) predictor = above;
    else if (type === 3) predictor = (left + above) >> 1;
    else if (type === 4) {
      const estimate = left + above - aboveLeft;
      const dLeft = Math.abs(estimate - left);
      const dAbove = Math.abs(estimate - above);
      const dAboveLeft = Math.abs(estimate - aboveLeft);
      predictor = dLeft <= dAbove && dLeft <= dAboveLeft ? left : dAbove <= dAboveLeft ? above : aboveLeft;
    }
    out[i] = (row[i] - predictor) & 0xff;
  }
  return out;
}

/**
 * A solid-colour PNG of the given size.
 *
 * `colourType` defaults to 6 (RGBA); pass 2 to match what Chromium actually
 * writes for a screenshot, which is the path the committed baselines take.
 */
export function makePng(width, height, [red, green, blue, alpha = 255] = [0, 0, 0], { colourType = 6, filter = 0 } = {}) {
  const channels = CHANNELS[colourType];
  if (!channels) throw new Error(`unsupported colour type ${colourType}`);

  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8; // bit depth
  header[9] = colourType;
  // Bytes 10..12 are the compression, filter and interlace methods. All zero
  // — the only combination the spec defines for these colour types.

  const stride = width * channels;
  const samples = channels >= 3 ? [red, green, blue, alpha] : [red, alpha];
  const raw = Buffer.alloc(height * (stride + 1));
  let previous = null;
  for (let row = 0; row < height; row++) {
    const line = Buffer.alloc(stride);
    for (let column = 0; column < width; column++) {
      for (let channel = 0; channel < channels; channel++) line[column * channels + channel] = samples[channel];
    }
    const start = row * (stride + 1);
    raw[start] = filter;
    applyFilter(filter, line, previous, channels).copy(raw, start + 1);
    previous = line;
  }

  return Buffer.concat([SIGNATURE, chunk('IHDR', header), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}
