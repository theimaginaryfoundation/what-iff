import { fullNpub, hexToNpub, normalizePubkey, npubToHex, shortNpub } from './npub';

// NIP-19's own examples.
const HEX = '3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d';
const NPUB = 'npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6';
const HEX2 = '7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e';
const NPUB2 = 'npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg';

describe('npub', () => {
  it('encodes and decodes the NIP-19 examples', () => {
    expect(hexToNpub(HEX)).toBe(NPUB);
    expect(npubToHex(NPUB)).toBe(HEX);
    expect(hexToNpub(HEX2)).toBe(NPUB2);
    expect(npubToHex(NPUB2)).toBe(HEX2);
  });

  it('accepts upper-case hex and npub, and a nostr: prefix', () => {
    expect(hexToNpub(HEX.toUpperCase())).toBe(NPUB);
    expect(npubToHex(NPUB.toUpperCase())).toBe(HEX);
    expect(npubToHex(`nostr:${NPUB}`)).toBe(HEX);
  });

  it('rejects what is not a key', () => {
    expect(hexToNpub('abc')).toBeNull();
    expect(hexToNpub(HEX + '00')).toBeNull();
    expect(npubToHex('npub1')).toBeNull();
    expect(npubToHex(NPUB.slice(0, -1) + (NPUB.endsWith('q') ? 'p' : 'q'))).toBeNull(); // bad checksum
    expect(npubToHex('nsec180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6')).toBeNull();
    expect(npubToHex('npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6W6')).toBeNull(); // mixed case
    expect(npubToHex('npub1bbbb')).toBeNull();
  });

  it('normalises either form to lowercase hex', () => {
    expect(normalizePubkey(` ${NPUB} `)).toBe(HEX);
    expect(normalizePubkey(HEX.toUpperCase())).toBe(HEX);
    expect(normalizePubkey('alice')).toBeNull();
  });

  it('shortens keys for display and gives the full npub for a title', () => {
    expect(shortNpub(HEX)).toBe('npub180cvv07…h6w6');
    expect(shortNpub(NPUB)).toBe('npub180cvv07…h6w6');
    expect(shortNpub('short')).toBe('short');
    expect(fullNpub(HEX)).toBe(NPUB);
    expect(fullNpub('not-a-key')).toBe('not-a-key');
  });
});
