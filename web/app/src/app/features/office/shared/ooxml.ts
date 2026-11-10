import JSZip from 'jszip';

/**
 * The Open Packaging Conventions container and the XML helpers the OOXML converters share:
 * parts, relationships, content types and the unit conversions between Word's twips, EMUs and
 * half-points and Univer's 96-dpi pixels and points.
 */

export const NS = {
  w: 'http://schemas.openxmlformats.org/wordprocessingml/2006/main',
  r: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships',
  wp: 'http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing',
  a: 'http://schemas.openxmlformats.org/drawingml/2006/main',
  pic: 'http://schemas.openxmlformats.org/drawingml/2006/picture',
  rel: 'http://schemas.openxmlformats.org/package/2006/relationships',
  ct: 'http://schemas.openxmlformats.org/package/2006/content-types',
  mc: 'http://schemas.openxmlformats.org/markup-compatibility/2006',
  v: 'urn:schemas-microsoft-com:vml',
} as const;

export const REL_TYPE = {
  officeDocument: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument',
  styles: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles',
  numbering: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering',
  settings: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings',
  header: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/header',
  footer: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer',
  hyperlink: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink',
  image: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/image',
  footnotes: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes',
  endnotes: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/endnotes',
  comments: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments',
  theme: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme',
  fontTable: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable',
  coreProperties: 'http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties',
  extendedProperties: 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties',
} as const;

export interface Relationship {
  id: string;
  type: string;
  target: string;
  external: boolean;
}

/** A zip of parts, read lazily; paths are package-absolute without the leading slash. */
export class OoxmlPackage {
  private constructor(private readonly zip: JSZip) {}

  static async open(bytes: ArrayBuffer | Uint8Array): Promise<OoxmlPackage> {
    return new OoxmlPackage(await JSZip.loadAsync(bytes));
  }

  static empty(): OoxmlPackage {
    return new OoxmlPackage(new JSZip());
  }

  has(path: string): boolean {
    return this.zip.file(normalizePath(path)) !== null;
  }

  paths(): string[] {
    return Object.keys(this.zip.files).filter(p => !this.zip.files[p].dir);
  }

  async text(path: string): Promise<string | null> {
    const file = this.zip.file(normalizePath(path));
    return file ? file.async('string') : null;
  }

  async bytes(path: string): Promise<Uint8Array | null> {
    const file = this.zip.file(normalizePath(path));
    return file ? file.async('uint8array') : null;
  }

  async xml(path: string): Promise<Document | null> {
    const text = await this.text(path);
    return text === null ? null : parseXml(text);
  }

  /** The relationships of a part: "word/document.xml" reads "word/_rels/document.xml.rels". */
  async rels(partPath: string): Promise<Map<string, Relationship>> {
    const out = new Map<string, Relationship>();
    const doc = await this.xml(relsPathFor(partPath));
    if (!doc) return out;
    for (const el of Array.from(doc.getElementsByTagNameNS(NS.rel, 'Relationship'))) {
      const id = el.getAttribute('Id') ?? '';
      out.set(id, {
        id,
        type: el.getAttribute('Type') ?? '',
        target: el.getAttribute('Target') ?? '',
        external: el.getAttribute('TargetMode') === 'External',
      });
    }
    return out;
  }

  put(path: string, content: string | Uint8Array): void {
    // No directory entries: Office's own packages have none.
    this.zip.file(normalizePath(path), content, { createFolders: false });
  }

  async save(mimeType: string): Promise<Uint8Array> {
    return this.zip.generateAsync({ type: 'uint8array', mimeType, compression: 'DEFLATE' });
  }
}

export function normalizePath(path: string): string {
  return path.replace(/^\/+/, '');
}

export function relsPathFor(partPath: string): string {
  const p = normalizePath(partPath);
  const slash = p.lastIndexOf('/');
  const dir = slash === -1 ? '' : p.slice(0, slash + 1);
  const name = slash === -1 ? p : p.slice(slash + 1);
  return `${dir}_rels/${name}.rels`;
}

/** Resolves a relationship target against the part that owns the relationship. */
export function resolveTarget(partPath: string, target: string): string {
  if (target.startsWith('/')) return normalizePath(target);
  const p = normalizePath(partPath);
  const base = p.includes('/') ? p.slice(0, p.lastIndexOf('/') + 1) : '';
  const segments = (base + target).split('/');
  const out: string[] = [];
  for (const s of segments) {
    if (s === '..') out.pop();
    else if (s !== '.' && s !== '') out.push(s);
  }
  return out.join('/');
}

export function parseXml(text: string): Document {
  const doc = new DOMParser().parseFromString(text, 'application/xml');
  const error = doc.getElementsByTagName('parsererror')[0];
  if (error) throw new Error(`Invalid XML: ${error.textContent?.slice(0, 200)}`);
  return doc;
}

export function serializeXml(node: Node): string {
  return new XMLSerializer().serializeToString(node);
}

export function escapeXml(text: string): string {
  return text.replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&apos;' })[ch] as string);
}

/** Direct children with the given namespace and local name. */
export function children(parent: Element | null | undefined, ns: string, localName: string): Element[] {
  if (!parent) return [];
  return Array.from(parent.childNodes).filter(
    (n): n is Element => n.nodeType === 1 && (n as Element).namespaceURI === ns && (n as Element).localName === localName,
  );
}

export function child(parent: Element | null | undefined, ns: string, localName: string): Element | null {
  return children(parent, ns, localName)[0] ?? null;
}

export function elementChildren(parent: Element | null | undefined): Element[] {
  if (!parent) return [];
  return Array.from(parent.childNodes).filter((n): n is Element => n.nodeType === 1);
}

/** A namespaced attribute, such as w:val, or the unprefixed one. */
export function attr(el: Element | null | undefined, ns: string, localName: string): string | null {
  if (!el) return null;
  return el.getAttributeNS(ns, localName) ?? el.getAttribute(localName);
}

export function wval(el: Element | null | undefined): string | null {
  return attr(el, NS.w, 'val');
}

/** w:b, w:i and friends: present means on unless w:val says otherwise. */
export function toggle(el: Element | null | undefined): boolean | undefined {
  if (!el) return undefined;
  const v = wval(el);
  return v === null || v === '1' || v === 'true' || v === 'on';
}

export function num(value: string | null | undefined): number | undefined {
  if (value === null || value === undefined || value === '') return undefined;
  const n = Number(value);
  return Number.isFinite(n) ? n : undefined;
}

// --- units ------------------------------------------------------------------------------------

export const TWIPS_PER_PX = 15; // 1440 twips per inch, 96 px per inch
export const EMU_PER_PX = 9525;

export function twipsToPx(twips: number): number {
  return Math.round((twips / TWIPS_PER_PX) * 100) / 100;
}

export function pxToTwips(px: number): number {
  return Math.round(px * TWIPS_PER_PX);
}

export function emuToPx(emu: number): number {
  return Math.round((emu / EMU_PER_PX) * 100) / 100;
}

export function pxToEmu(px: number): number {
  return Math.round(px * EMU_PER_PX);
}

export function halfPointsToPt(half: number): number {
  return half / 2;
}

export function ptToHalfPoints(pt: number): number {
  return Math.round(pt * 2);
}

/** Border widths are in eighths of a point. */
export function eighthsToPx(eighths: number): number {
  return Math.round((((eighths / 8) * 96) / 72) * 100) / 100;
}

export function pxToEighths(px: number): number {
  return Math.max(2, Math.round((px * 72 * 8) / 96));
}

export function hexColor(value: string | null | undefined): string | null {
  if (!value || value === 'auto') return null;
  const hex = value.replace('#', '');
  return /^[0-9a-fA-F]{6}$/.test(hex) ? `#${hex.toUpperCase()}` : null;
}

export function colorHex(rgb: string | null | undefined): string | null {
  if (!rgb) return null;
  const hex = rgb.replace('#', '').toUpperCase();
  return /^[0-9A-F]{6}$/.test(hex) ? hex : null;
}

const HIGHLIGHTS: Record<string, string> = {
  yellow: '#FFFF00',
  green: '#00FF00',
  cyan: '#00FFFF',
  magenta: '#FF00FF',
  blue: '#0000FF',
  red: '#FF0000',
  darkBlue: '#000080',
  darkCyan: '#008080',
  darkGreen: '#008000',
  darkMagenta: '#800080',
  darkRed: '#800000',
  darkYellow: '#808000',
  darkGray: '#808080',
  lightGray: '#C0C0C0',
  black: '#000000',
  white: '#FFFFFF',
};

export function highlightColor(name: string | null | undefined): string | null {
  return name ? (HIGHLIGHTS[name] ?? null) : null;
}

export function highlightName(rgb: string | null | undefined): string | null {
  const hex = rgb?.toUpperCase();
  return Object.entries(HIGHLIGHTS).find(([, v]) => v === hex)?.[0] ?? null;
}

export function bytesToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(binary);
}

export function base64ToBytes(base64: string): Uint8Array {
  const binary = atob(base64);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

export function mimeForExtension(ext: string): string {
  switch (ext.toLowerCase()) {
    case 'png':
      return 'image/png';
    case 'jpg':
    case 'jpeg':
      return 'image/jpeg';
    case 'gif':
      return 'image/gif';
    case 'bmp':
      return 'image/bmp';
    case 'svg':
      return 'image/svg+xml';
    case 'webp':
      return 'image/webp';
    default:
      return 'application/octet-stream';
  }
}

export function extensionForMime(mime: string): string {
  switch (mime) {
    case 'image/png':
      return 'png';
    case 'image/jpeg':
      return 'jpeg';
    case 'image/gif':
      return 'gif';
    case 'image/bmp':
      return 'bmp';
    case 'image/svg+xml':
      return 'svg';
    case 'image/webp':
      return 'webp';
    default:
      return 'bin';
  }
}

/** Splits a data: URL into its media type and bytes. */
export function parseDataUrl(url: string): { mime: string; bytes: Uint8Array } | null {
  const m = /^data:([^;,]+)(;base64)?,(.*)$/s.exec(url);
  if (!m) return null;
  return { mime: m[1], bytes: m[2] ? base64ToBytes(m[3]) : new TextEncoder().encode(decodeURIComponent(m[3])) };
}
