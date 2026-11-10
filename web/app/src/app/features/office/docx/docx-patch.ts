import { NS, OoxmlPackage, REL_TYPE, Relationship, bytesToBase64, mimeForExtension, resolveTarget } from '../shared/ooxml';

/**
 * Patch mode for the docx exporter (ADR 0x043, office suite): the saved file starts as the
 * original package, so every part the editor does not model (styles, settings, theme, fonts,
 * comments, custom XML, glossary, document properties) is kept byte for byte. The exporter
 * regenerates only the parts it edits and merges their relationships and content types into
 * what was there, so raw XML kept from the original still points at the right targets.
 */

export interface PartRels {
  entries: { id: string; type: string; target: string; external?: boolean }[];
  next: number;
}

const REL_ID = /^rId(\d+)$/;

export class PatchSource {
  private mediaIndex: Map<string, string> | null = null;
  private contentDefaults = new Map<string, string>();
  private contentOverrides = new Map<string, string>();

  private constructor(
    readonly pkg: OoxmlPackage,
    readonly mainPath: string,
    private readonly mainRels: Map<string, Relationship>,
  ) {}

  static async open(bytes: ArrayBuffer | Uint8Array): Promise<PatchSource> {
    const pkg = await OoxmlPackage.open(bytes);
    const rootRels = await pkg.rels('');
    const main = [...rootRels.values()].find(r => r.type === REL_TYPE.officeDocument);
    const mainPath = main ? resolveTarget('', main.target) : 'word/document.xml';
    const source = new PatchSource(pkg, mainPath, await pkg.rels(mainPath));
    await source.readContentTypes();
    return source;
  }

  /** The part the main document relates to by type, package-absolute. */
  partFor(type: string): string | null {
    const rel = [...this.mainRels.values()].find(r => r.type === type);
    return rel ? resolveTarget(this.mainPath, rel.target) : null;
  }

  /** A part's original relationships as the exporter's mutable list, with the next free id. */
  async partRels(partPath: string): Promise<PartRels> {
    const rels = await this.pkg.rels(partPath);
    let max = 0;
    const entries = [...rels.values()].map(r => {
      const m = REL_ID.exec(r.id);
      if (m) max = Math.max(max, Number(m[1]));
      return { id: r.id, type: r.type, target: r.target, external: r.external };
    });
    return { entries, next: max + 1 };
  }

  /** Reads every media part once, so pictures the document already holds can be found by their bytes. */
  async indexMedia(): Promise<void> {
    if (this.mediaIndex) return;
    this.mediaIndex = new Map();
    for (const path of this.pkg.paths()) {
      if (!/^word\/media\//.test(path)) continue;
      const bytes = await this.pkg.bytes(path);
      if (!bytes) continue;
      const mime = mimeForExtension(path.slice(path.lastIndexOf('.') + 1));
      if (mime !== 'application/octet-stream') this.mediaIndex.set(`data:${mime};base64,${bytesToBase64(bytes)}`, path);
    }
  }

  /** The media part holding exactly these bytes, if the original has one, so a picture is not written twice. */
  mediaPathFor(dataUrl: string): string | null {
    return this.mediaIndex?.get(dataUrl) ?? null;
  }

  /** Media part names already taken, so new pictures get fresh ones. */
  mediaNames(): Set<string> {
    return new Set(
      this.pkg
        .paths()
        .filter(p => /^word\/media\//.test(p))
        .map(p => p.slice('word/media/'.length)),
    );
  }

  private async readContentTypes(): Promise<void> {
    const doc = await this.pkg.xml('[Content_Types].xml');
    if (!doc) return;
    for (const el of Array.from(doc.getElementsByTagNameNS(NS.ct, 'Default'))) {
      const ext = el.getAttribute('Extension');
      const type = el.getAttribute('ContentType');
      if (ext && type) this.contentDefaults.set(ext.toLowerCase(), type);
    }
    for (const el of Array.from(doc.getElementsByTagNameNS(NS.ct, 'Override'))) {
      const part = el.getAttribute('PartName');
      const type = el.getAttribute('ContentType');
      if (part && type) this.contentOverrides.set(part, type);
    }
  }

  hasDefault(extension: string): boolean {
    return this.contentDefaults.has(extension.toLowerCase());
  }

  hasOverride(partName: string): boolean {
    return this.contentOverrides.has(partName);
  }

  /** The original content types plus what the exporter added. */
  contentTypesXml(defaults: { extension: string; contentType: string }[], overrides: { partName: string; contentType: string }[]): string {
    const defaultEntries = new Map(this.contentDefaults);
    for (const d of defaults)
      if (!defaultEntries.has(d.extension.toLowerCase())) defaultEntries.set(d.extension.toLowerCase(), d.contentType);
    const overrideEntries = new Map(this.contentOverrides);
    for (const o of overrides) overrideEntries.set(o.partName, o.contentType);
    const xml =
      [...defaultEntries].map(([ext, type]) => `<Default Extension="${ext}" ContentType="${type}"/>`).join('') +
      [...overrideEntries].map(([part, type]) => `<Override PartName="${part}" ContentType="${type}"/>`).join('');
    return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="${NS.ct}">${xml}</Types>`;
  }
}

/** The original numbering part: which numIds exist, and where new definitions can start. */
export interface SourceNumbering {
  path: string;
  xml: string;
  numIds: Set<string>;
  nextNumId: number;
  nextAbstractId: number;
}

export async function readSourceNumbering(source: PatchSource): Promise<SourceNumbering | null> {
  const path = source.partFor(REL_TYPE.numbering);
  const xml = path ? await source.pkg.text(path) : null;
  if (!path || xml === null) return null;
  const numIds = new Set<string>();
  let maxNum = 0;
  let maxAbstract = -1;
  for (const m of xml.matchAll(/<w:num\s+[^>]*w:numId="(\d+)"/g)) {
    numIds.add(m[1]);
    maxNum = Math.max(maxNum, Number(m[1]));
  }
  for (const m of xml.matchAll(/<w:abstractNum\s+[^>]*w:abstractNumId="(\d+)"/g)) maxAbstract = Math.max(maxAbstract, Number(m[1]));
  return { path, xml, numIds, nextNumId: maxNum + 1, nextAbstractId: maxAbstract + 1 };
}
