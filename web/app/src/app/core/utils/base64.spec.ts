import { describe, expect, it } from 'vitest';

import { base64ToBytes, bytesToBase64 } from './base64';

describe('base64', () => {
  it('round-trips bytes, including a buffer larger than one chunk', () => {
    const bytes = new Uint8Array(0x8000 * 2 + 5).map((_, i) => i % 256);
    const text = bytesToBase64(bytes);
    expect(text.startsWith(btoa(String.fromCharCode(0, 1, 2)))).toBe(true);
    expect(base64ToBytes(text)).toEqual(bytes);
  });
});
