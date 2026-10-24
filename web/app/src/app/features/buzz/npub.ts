/**
 * Nostr public keys as people see them: NIP-19 `npub` (bech32) for the 32-byte hex key the API
 * stores. Pure functions, no dependencies; only what the Buzz tab needs (npub only, no nsec or
 * TLV forms).
 */

const CHARSET = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l';
const GENERATOR = [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3];
const HRP = 'npub';
const HEX_KEY = /^[0-9a-f]{64}$/;

function polymod(values: readonly number[]): number {
  let chk = 1;
  for (const v of values) {
    const top = chk >>> 25;
    chk = ((chk & 0x1ffffff) << 5) ^ v;
    for (let i = 0; i < 5; i++) if ((top >>> i) & 1) chk ^= GENERATOR[i];
  }
  return chk;
}

function hrpExpand(hrp: string): number[] {
  const codes = [...hrp].map(c => c.charCodeAt(0));
  return [...codes.map(c => c >> 5), 0, ...codes.map(c => c & 31)];
}

/** Regroups bits (8 → 5 to encode, 5 → 8 to decode); null when the padding is not valid. */
function convertBits(data: readonly number[], from: number, to: number, pad: boolean): number[] | null {
  let acc = 0;
  let bits = 0;
  const out: number[] = [];
  const max = (1 << to) - 1;
  for (const value of data) {
    acc = (acc << from) | value;
    bits += from;
    while (bits >= to) {
      bits -= to;
      out.push((acc >> bits) & max);
    }
    acc &= (1 << bits) - 1;
  }
  if (pad) {
    if (bits > 0) out.push((acc << (to - bits)) & max);
  } else if (bits >= from || ((acc << (to - bits)) & max) !== 0) {
    return null;
  }
  return out;
}

/** The npub for a 64-character hex public key; null when it is not one. */
export function hexToNpub(hex: string): string | null {
  const key = hex.trim().toLowerCase();
  if (!HEX_KEY.test(key)) return null;
  const bytes = key.match(/../g)!.map(b => parseInt(b, 16));
  const data = convertBits(bytes, 8, 5, true)!;
  const mod = polymod([...hrpExpand(HRP), ...data, 0, 0, 0, 0, 0, 0]) ^ 1;
  const checksum = Array.from({ length: 6 }, (_, i) => (mod >> (5 * (5 - i))) & 31);
  return HRP + '1' + [...data, ...checksum].map(d => CHARSET[d]).join('');
}

/** The hex public key in an npub; null when it is not a valid npub. */
export function npubToHex(npub: string): string | null {
  const s = npub.trim();
  if (s !== s.toLowerCase() && s !== s.toUpperCase()) return null;
  const lower = s.toLowerCase().replace(/^nostr:/, '');
  if (!lower.startsWith(HRP + '1')) return null;
  const data: number[] = [];
  for (const c of lower.slice(HRP.length + 1)) {
    const d = CHARSET.indexOf(c);
    if (d < 0) return null;
    data.push(d);
  }
  if (data.length < 6 || polymod([...hrpExpand(HRP), ...data]) !== 1) return null;
  const bytes = convertBits(data.slice(0, -6), 5, 8, false);
  if (!bytes || bytes.length !== 32) return null;
  return bytes.map(b => b.toString(16).padStart(2, '0')).join('');
}

/**
 * A public key typed or pasted as an npub (optionally `nostr:`-prefixed) or hex, as lowercase
 * hex; null when it is neither. The server normalises the same way.
 */
export function normalizePubkey(input: string): string | null {
  const s = input.trim();
  if (HEX_KEY.test(s.toLowerCase())) return s.toLowerCase();
  return npubToHex(s);
}

/** "npub1abcdefgh…wxyz" for a hex key or npub, for chips and labels (the full npub belongs in a title). */
export function shortNpub(key: string): string {
  const npub = key.startsWith(HRP + '1') ? key : (hexToNpub(key) ?? key);
  return npub.length > 20 ? `${npub.slice(0, 12)}…${npub.slice(-4)}` : npub;
}

/** The full npub for a hex key (or the input unchanged when it is not one). */
export function fullNpub(key: string): string {
  return hexToNpub(key) ?? key;
}
