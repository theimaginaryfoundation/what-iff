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

/** A solid-colour RGBA PNG of the given size. */
export function makePng(width, height, [red, green, blue, alpha = 255] = [0, 0, 0]) {
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8; // bit depth
  header[9] = 6; // colour type: truecolour with alpha
  // Bytes 10..12 are the compression, filter and interlace methods. All zero
  // — the only combination the spec defines for this colour type.

  // Each scanline carries a leading filter byte; 0 is "no filtering", which
  // keeps both the raw bytes and this encoder trivial.
  const stride = width * 4;
  const raw = Buffer.alloc(height * (stride + 1));
  for (let row = 0; row < height; row++) {
    const start = row * (stride + 1);
    raw[start] = 0;
    for (let column = 0; column < width; column++) {
      const pixel = start + 1 + column * 4;
      raw[pixel] = red;
      raw[pixel + 1] = green;
      raw[pixel + 2] = blue;
      raw[pixel + 3] = alpha;
    }
  }

  return Buffer.concat([SIGNATURE, chunk('IHDR', header), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}
