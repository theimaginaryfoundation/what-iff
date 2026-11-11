import { savedLinkUrl } from '../shared/safe-link';
import { FidelityCollector, FidelityReport } from '../shared/fidelity';
import {
  NS,
  OoxmlPackage,
  REL_TYPE,
  colorHex,
  escapeXml,
  extensionForMime,
  parseDataUrl,
  pxToEighths,
  pxToEmu,
  pxToTwips,
} from '../shared/ooxml';
import { paragraphSegments, sdtTags, withRunExtras } from './docx-revisions';
import { H_ALIGN, H_RELATIVE, V_ALIGN, V_RELATIVE } from './docx-body';
import { notesPartXml, wordNoteIds } from './docx-notes';
import {
  COMMENT_PARTS,
  CommentWritePlan,
  DocxCommentThread,
  WordComments,
  planCommentParts,
  readWordComments,
  wordThreads,
} from './docx-comments';
import { PartRels, PatchSource, SourceNumbering, readSourceNumbering } from './docx-patch';
import {
  SECTPR_ORDER,
  TCPR_ORDER,
  TRPR_ORDER,
  defaultNamedStyles,
  diffTextStyle,
  numberingXml,
  orderedProperties,
  pPrXml,
  rPrXml,
  resolveTextStyle,
  stylesXml,
} from './docx-styles';
import {
  CUSTOM_DECORATION_COMMENT,
  CUSTOM_RANGE,
  DocBody,
  DocCustomRange,
  DocDocument,
  DocDrawing,
  DocHeaderFooter,
  DocPageSetup,
  DocParagraph,
  DocSectionBreak,
  DocTable,
  DocTableCell,
  DocTableCellBorder,
  DocTextRun,
  DocTextStyle,
  LAYOUT,
  TOKEN,
  isNoBorder,
} from './univer-doc-types';

export interface DocxExportOptions {
  /**
   * The comment threads to save (ADR 0x045), each placed by the comment decoration with its id.
   * Left out, the file's own comments are kept as they were.
   */
  comments?: readonly DocxCommentThread[];
}

export interface DocxExportResult {
  bytes: Uint8Array;
  report: FidelityReport;
}

const DOCX_MIME = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
const CT_NUMBERING = 'application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml';

/**
 * Writes a Univer document snapshot as a .docx; the mirror of docxToDocument. With the original
 * file's bytes it works in patch mode (docx-patch.ts): the original package is the base and only
 * the edited parts are regenerated, so nothing the editor does not model is lost.
 */
export async function documentToDocx(
  document: DocDocument,
  original?: ArrayBuffer | Uint8Array,
  opts: DocxExportOptions = {},
): Promise<DocxExportResult> {
  const fidelity = new FidelityCollector();
  const source = original ? await PatchSource.open(original) : null;
  const pkg = source ? source.pkg : OoxmlPackage.empty();
  const writer = new DocxWriter(document, fidelity, source, opts.comments);
  await writer.write(pkg);
  return { bytes: await pkg.save(DOCX_MIME), report: fidelity.report() };
}

/** How list types map to numIds: the original part's ids for lists that came from it, fresh ones for the rest. */
interface NumberingPlan {
  numIdFor(listType: string): string;
  /** The whole part to write (from scratch), or the definitions to append to the original. */
  xml: string | null;
  append: boolean;
}

class DocxWriter {
  private readonly styles: NonNullable<DocDocument['styles']>;
  private numbering: NumberingPlan | null = null;
  private readonly overrides: { partName: string; contentType: string }[] = [];
  private readonly defaults: { extension: string; contentType: string }[] = [];
  private readonly mediaCount = { n: 0 };
  private mediaNames = new Set<string>();
  private headerCount = 0;
  private footerCount = 0;
  private bookmarkCount = 0;
  /** Note ids referenced from the body, by kind, in reference order; only these stories are written. */
  private readonly referencedNotes: Record<'footnote' | 'endnote', string[]> = { footnote: [], endnote: [] };
  private noteWordIds = new Map<string, string>();
  /** Word ids of each thread's comments, first comment first, by thread id. */
  private commentWordIds = new Map<string, string[]>();

  constructor(
    private readonly doc: DocDocument,
    private readonly fidelity: FidelityCollector,
    private readonly source: PatchSource | null,
    private readonly comments?: readonly DocxCommentThread[],
  ) {
    const defaults = defaultNamedStyles();
    this.styles = { ...defaults, ...(doc.styles ?? {}) };
  }

  async write(pkg: OoxmlPackage): Promise<void> {
    const addRel = (rels: PartRels, type: string, target: string, external = false) => {
      const same = rels.entries.find(r => r.type === type && r.target === target && !!r.external === external);
      if (same) return same.id;
      const id = `rId${rels.next++}`;
      rels.entries.push({ id, type, target, external });
      return id;
    };
    const mainPath = this.source?.mainPath ?? 'word/document.xml';
    const docRels: PartRels = this.source ? await this.source.partRels(mainPath) : { entries: [], next: 1 };
    if (this.source) {
      await this.source.indexMedia();
      this.mediaNames = this.source.mediaNames();
    } else {
      addRel(docRels, REL_TYPE.styles, 'styles.xml');
      addRel(docRels, REL_TYPE.settings, 'settings.xml');
    }
    this.numbering = await this.planNumbering();
    if (this.numbering?.xml) {
      const path = (this.source && this.source.partFor(REL_TYPE.numbering)) ?? 'word/numbering.xml';
      if (this.numbering.append) {
        pkg.put(path, appendNumbering((await pkg.text(path)) ?? '', this.numbering.xml));
      } else {
        pkg.put(path, this.numbering.xml);
        addRel(docRels, REL_TYPE.numbering, relativeTo(mainPath, path));
        this.overrides.push({ partName: `/${path}`, contentType: CT_NUMBERING });
      }
    }
    this.noteWordIds = wordNoteIds(Object.keys(this.doc.notes ?? {}));
    const commentParts = await this.planComments(mainPath);
    if (this.source) {
      for (const part of [...Object.values(this.doc.headers ?? {}), ...Object.values(this.doc.footers ?? {})]) {
        if (part.docxPath && this.source.pkg.has(part.docxPath)) {
          this.preloadedRels.set(part.docxPath, await this.source.partRels(part.docxPath));
          this.preloadedXml.set(part.docxPath, await this.source.pkg.text(part.docxPath));
        }
      }
    }
    const sectPr = this.sectPrXml(this.doc.documentStyle, (kind, id) => this.headerFooterPart(pkg, docRels, addRel, kind, id));
    const bodyXml = this.bodyXml(this.doc.body, this.doc, pkg, docRels, addRel, mainPath, sectPr);
    const mainRoot = rootNamespaces(this.source ? await this.source.pkg.text(mainPath) : null);
    pkg.put(
      mainPath,
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
        `<w:document ${mainRoot}><w:body>${bodyXml}${sectPr}</w:body></w:document>`,
    );
    await this.notesParts(pkg, docRels, addRel, mainPath);
    for (const part of COMMENT_PARTS) {
      const xml = commentParts.parts?.[part.key];
      if (xml === undefined) continue;
      const path = commentParts.paths[part.key] ?? `${dirOf(mainPath)}${part.file}`;
      pkg.put(path, xml);
      addRel(docRels, part.rel, relativeTo(mainPath, path));
      if (!this.source?.hasOverride(`/${path}`)) this.overrides.push({ partName: `/${path}`, contentType: part.contentType });
    }
    if (this.source) {
      pkg.put(`${dirOf(mainPath)}_rels/${baseOf(mainPath)}.rels`, relsXml(docRels));
      pkg.put('[Content_Types].xml', this.source.contentTypesXml(this.defaults, this.overrides));
      return;
    }
    pkg.put('word/styles.xml', stylesXml(this.styles, this.doc.documentStyle.textStyle, this.doc.documentStyle.defaultParagraphStyle));
    pkg.put('word/settings.xml', this.settingsXml());
    pkg.put('word/_rels/document.xml.rels', relsXml(docRels));
    pkg.put(
      '_rels/.rels',
      relsXml({
        entries: [
          { id: 'rId1', type: REL_TYPE.officeDocument, target: 'word/document.xml' },
          { id: 'rId2', type: REL_TYPE.coreProperties, target: 'docProps/core.xml' },
          { id: 'rId3', type: REL_TYPE.extendedProperties, target: 'docProps/app.xml' },
        ],
        next: 4,
      }),
    );
    pkg.put('docProps/core.xml', coreXml(this.doc.title ?? ''));
    pkg.put(
      'docProps/app.xml',
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>WhatIff</Application></Properties>`,
    );
    pkg.put(
      '[Content_Types].xml',
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="${NS.ct}">` +
        `<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
        `<Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="gif" ContentType="image/gif"/><Default Extension="bmp" ContentType="image/bmp"/><Default Extension="svg" ContentType="image/svg+xml"/><Default Extension="webp" ContentType="image/webp"/>` +
        override('/word/document.xml', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml') +
        override('/word/styles.xml', 'application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml') +
        override('/word/settings.xml', 'application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml') +
        override('/docProps/core.xml', 'application/vnd.openxmlformats-package.core-properties+xml') +
        override('/docProps/app.xml', 'application/vnd.openxmlformats-officedocument.extended-properties+xml') +
        this.overrides.map(o => override(o.partName, o.contentType)).join('') +
        `</Types>`,
    );
  }

  /**
   * Which Word ids each thread's comments get, and the comment parts to write: none when the
   * threads are the file's own, unchanged (docx-comments.ts).
   */
  private async planComments(mainPath: string): Promise<CommentWritePlan & { paths: WordComments['paths'] }> {
    const original = this.source ? await readWordComments(this.source.pkg, mainPath, await this.source.pkg.rels(mainPath)) : null;
    const threads =
      this.comments ?? [...(original ? wordThreads(original).threads.values() : [])].map(t => ({ ...t, anchor: { segmentId: '' } }));
    // A thread is written only where its decoration still marks the text.
    const marked = new Set(
      [this.doc.body, ...Object.values(this.doc.headers ?? {}).map(h => h.body), ...Object.values(this.doc.footers ?? {}).map(f => f.body)]
        .flatMap(b => b.customDecorations ?? [])
        .filter(d => d.type === CUSTOM_DECORATION_COMMENT)
        .map(d => d.id),
    );
    const plan = planCommentParts(
      threads.filter(t => marked.has(t.id)),
      original,
      { styles: { commentText: !!this.styles['CommentText'], commentReference: !!this.styles['CommentReference'] } },
    );
    this.commentWordIds = plan.wordIds;
    return { ...plan, paths: original?.paths ?? {} };
  }

  /** Lists from the original keep their numIds; lists the editor made are appended after them. */
  private async planNumbering(): Promise<NumberingPlan | null> {
    const lists = this.doc.lists ?? {};
    const original: SourceNumbering | null = this.source ? await readSourceNumbering(this.source) : null;
    if (!original) {
      const fresh = numberingXml(lists);
      return fresh ? { numIdFor: fresh.numIdFor, xml: fresh.xml, append: false } : null;
    }
    const added: Record<string, NonNullable<DocDocument['lists']>[string]> = {};
    for (const [type, list] of Object.entries(lists)) {
      const m = /^num(\d+)$/.exec(type);
      if (m && original.numIds.has(m[1])) continue;
      added[type] = list;
    }
    const fragment = numberingXml(added, { firstNumId: original.nextNumId, firstAbstractId: original.nextAbstractId, fragment: true });
    return {
      numIdFor: (listType: string) => {
        const m = /^num(\d+)$/.exec(listType);
        if (m && original.numIds.has(m[1])) return m[1];
        return fragment ? fragment.numIdFor(listType) : ([...original.numIds][0] ?? '1');
      },
      xml: fragment?.xml ?? null,
      append: true,
    };
  }

  /** Footnote and endnote stories the body refers to, as parts; in patch mode at their original paths. */
  private async notesParts(
    pkg: OoxmlPackage,
    docRels: PartRels,
    addRel: (rels: PartRels, type: string, target: string, external?: boolean) => string,
    mainPath: string,
  ): Promise<void> {
    for (const kind of ['footnote', 'endnote'] as const) {
      const ids = this.referencedNotes[kind];
      if (!ids.length) continue;
      const relType = kind === 'footnote' ? REL_TYPE.footnotes : REL_TYPE.endnotes;
      const path = (this.source && this.source.partFor(relType)) ?? `word/${kind}s.xml`;
      const partRels: PartRels = this.source ? await this.source.partRels(path) : { entries: [], next: 1 };
      const refStyle = this.styles[kind === 'footnote' ? 'FootnoteReference' : 'EndnoteReference']
        ? `<w:rStyle w:val="${kind === 'footnote' ? 'FootnoteReference' : 'EndnoteReference'}"/>`
        : '<w:vertAlign w:val="superscript"/>';
      const stories = ids.map(noteId => {
        const note = this.doc.notes![noteId];
        const scope: DocDocument = {
          ...this.doc,
          tableSource: note.tableSource ?? {},
          drawings: { ...(this.doc.drawings ?? {}), ...(note.drawings ?? {}) },
        };
        let xml = this.bodyXml(note.body, scope, pkg, partRels, addRel, path);
        // Word draws the note's number from a reference run at the start of its first paragraph.
        const mark = `<w:r><w:rPr>${refStyle}</w:rPr><w:${kind}Ref/></w:r>`;
        const pPrEnd = xml.indexOf('</w:pPr>');
        const pStart = xml.indexOf('<w:p>');
        if (pPrEnd !== -1 && (pStart === -1 || pPrEnd < xml.indexOf('</w:p>')))
          xml = xml.slice(0, pPrEnd + 8) + mark + xml.slice(pPrEnd + 8);
        else if (pStart !== -1) xml = xml.slice(0, pStart + 5) + mark + xml.slice(pStart + 5);
        return { wordId: this.noteWordIds.get(noteId) ?? '1', xml };
      });
      pkg.put(path, notesPartXml(kind, stories, NAMESPACES));
      if (partRels.entries.length) pkg.put(`${dirOf(path)}_rels/${baseOf(path)}.rels`, relsXml(partRels));
      addRel(docRels, relType, relativeTo(mainPath, path));
      const contentType = `application/vnd.openxmlformats-officedocument.wordprocessingml.${kind}s+xml`;
      if (!this.source?.hasOverride(`/${path}`)) this.overrides.push({ partName: `/${path}`, contentType });
    }
  }

  private headerFooterPart(
    pkg: OoxmlPackage,
    docRels: PartRels,
    addRel: (rels: PartRels, type: string, target: string, external?: boolean) => string,
    kind: 'header' | 'footer',
    id: string,
  ): string | null {
    const source: (DocHeaderFooter & { docxPath?: string }) | undefined =
      kind === 'header' ? this.doc.headers?.[id] : this.doc.footers?.[id];
    if (!source) return null;
    const mainPath = this.source?.mainPath ?? 'word/document.xml';
    // In patch mode a header goes back to the part it came from, with that part's relationships kept.
    const original = this.source && source.docxPath && this.source.pkg.has(source.docxPath) ? source.docxPath : null;
    let path = original;
    if (!path) {
      let n = kind === 'header' ? ++this.headerCount : ++this.footerCount;
      while (this.source?.pkg.has(`${dirOf(mainPath)}${kind}${n}.xml`)) n = kind === 'header' ? ++this.headerCount : ++this.footerCount;
      path = `${dirOf(mainPath)}${kind}${n}.xml`;
    }
    const partRels: PartRels = original && this.source ? this.partRelsSync(original) : { entries: [], next: 1 };
    const scope: DocDocument = {
      ...this.doc,
      tableSource: source.tableSource ?? {},
      drawings: { ...(this.doc.drawings ?? {}), ...(source.drawings ?? {}) },
    };
    const xml = this.bodyXml(source.body, scope, pkg, partRels, addRel, path);
    const tag = kind === 'header' ? 'w:hdr' : 'w:ftr';
    const root = rootNamespaces(this.preloadedXml.get(path));
    pkg.put(path, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><${tag} ${root}>${xml}</${tag}>`);
    if (partRels.entries.length) pkg.put(`${dirOf(path)}_rels/${baseOf(path)}.rels`, relsXml(partRels));
    if (!this.source?.hasOverride(`/${path}`))
      this.overrides.push({
        partName: `/${path}`,
        contentType: `application/vnd.openxmlformats-officedocument.wordprocessingml.${kind}+xml`,
      });
    return addRel(docRels, kind === 'header' ? REL_TYPE.header : REL_TYPE.footer, relativeTo(mainPath, path));
  }

  /** Header and footer rels were read ahead of time (sectPrXml is synchronous). */
  private readonly preloadedRels = new Map<string, PartRels>();
  /** The original header and footer parts, for their root namespaces. */
  private readonly preloadedXml = new Map<string, string | null>();
  private partRelsSync(path: string): PartRels {
    return this.preloadedRels.get(path) ?? { entries: [], next: 1 };
  }

  // --- body ----------------------------------------------------------------------------------

  private bodyXml(
    body: DocBody,
    scope: DocDocument,
    pkg: OoxmlPackage,
    rels: PartRels,
    addRel: (rels: PartRels, type: string, target: string, external?: boolean) => string,
    partPath: string,
    finalSectPr = '',
  ): string {
    const ctx: WriteContext = {
      body,
      scope,
      pkg,
      rels,
      addRel,
      partPath,
      paragraphAt: new Map(),
      tableAt: new Map(),
      blockAt: new Map(),
      rawAt: new Map(),
      rawRunAt: new Map(),
      noteAt: new Map(),
      before: new Map(),
      after: new Map(),
      outerOpen: new Map(),
      outerClose: new Map(),
      breakAt: new Map(),
      rPrChanges: [],
      blockSdts: [],
      rowSdts: [],
      cellSdts: [],
    };
    for (const p of body.paragraphs ?? []) ctx.paragraphAt.set(p.startIndex, p);
    for (const t of body.tables ?? []) ctx.tableAt.set(t.startIndex, t);
    for (const b of body.customBlocks ?? []) {
      if (b.docxRawXml) ctx.rawRunAt.set(b.startIndex, b.docxRawXml);
      else ctx.blockAt.set(b.startIndex, b.blockId);
    }
    for (const b of body.docxRawCustomBlocks ?? []) if (b.docxRawXml) ctx.rawRunAt.set(b.startIndex, b.docxRawXml);
    for (const r of body.docxRawBlocks ?? []) ctx.rawAt.set(r.startIndex, [...(ctx.rawAt.get(r.startIndex) ?? []), r.xml]);
    this.planMarkers(ctx, body, partPath === (this.source?.mainPath ?? 'word/document.xml'));
    const out: string[] = [];
    const sections = (body.sectionBreaks ?? []).slice().sort((a, b) => a.startIndex - b.startIndex);
    const end = body.dataStream.length;
    this.blocksXml(ctx, 0, end, out, sections, finalSectPr !== '');
    return out.join('');
  }

  /** Emits the blocks of [from, to): paragraphs, tables and raw XML, in stream order. */
  private blocksXml(
    ctx: WriteContext,
    from: number,
    to: number,
    out: string[],
    sections: DocSectionBreak[],
    hasFinalSectPr: boolean,
  ): void {
    const stream = ctx.body.dataStream;
    let i = from;
    let paragraphStart = from;
    const emitRaw = (at: number) => {
      const raw = ctx.rawAt.get(at);
      if (raw) out.push(...raw);
    };
    // Controls around whole blocks open with the first block that holds their start, and close after
    // the block that holds their end, so an edit that moves either end still leaves them balanced.
    const pending = ctx.blockSdts
      .filter(r => r.startIndex >= from && r.startIndex <= to)
      .sort((a, b) => a.startIndex - b.startIndex || b.endIndex - a.endIndex);
    const open: { end: number; close: string }[] = [];
    const openFor = (first: number, last: number) => {
      // Those before this block lie inside a table already written, whose cells opened their own.
      while (pending.length && pending[0].startIndex < first) pending.shift();
      while (pending.length && pending[0].startIndex >= first && pending[0].startIndex <= last) {
        const range = pending.shift()!;
        const tags = sdtTags(stream, range);
        out.push(tags.open);
        open.push({ end: range.endIndex, close: tags.close });
      }
    };
    const closeThrough = (last: number) => {
      while (open.length && open[open.length - 1].end <= last) out.push(open.pop()!.close);
    };
    while (i < to) {
      emitRaw(i);
      const ch = stream[i];
      if (ch === TOKEN.TABLE_START) {
        const table = ctx.tableAt.get(i);
        if (!table) {
          i++;
          continue;
        }
        openFor(i, i);
        out.push(this.tableXml(ctx, table.startIndex, table.endIndex, table.tableId));
        closeThrough(table.endIndex - 1);
        i = table.endIndex;
        paragraphStart = i;
        continue;
      }
      if (ch === TOKEN.PARAGRAPH) {
        const paragraph = ctx.paragraphAt.get(i);
        // A section break right after this paragraph ends a section: its sectPr goes in the pPr.
        const next = stream[i + 1];
        const section = next === TOKEN.SECTION_BREAK ? sections.find(s => s.startIndex === i + 1) : undefined;
        const isFinal = section && i + 2 >= to && hasFinalSectPr;
        const sectXml = section && !isFinal && i + 2 < to ? this.sectPrXml(section, () => null) : '';
        openFor(paragraphStart, i);
        out.push(this.paragraphXml(ctx, paragraphStart, i, paragraph, sectXml));
        closeThrough(i);
        i += section ? 2 : 1;
        paragraphStart = i;
        continue;
      }
      if (ch === TOKEN.SECTION_BREAK) {
        i++;
        paragraphStart = i;
        continue;
      }
      i++;
    }
    emitRaw(to);
    while (open.length) out.push(open.pop()!.close);
  }

  private paragraphXml(ctx: WriteContext, from: number, to: number, paragraph: DocParagraph | undefined, sectPrXml: string): string {
    const style = paragraph?.paragraphStyle ? { ...paragraph.paragraphStyle } : undefined;
    const styleId = this.namedStyleOf(paragraph) ?? (paragraph?.styleId && this.styles[paragraph.styleId] ? paragraph.styleId : undefined);
    const namedBase = resolveTextStyle(this.styles, styleId);
    // The baked named-style text formatting comes back out of the paragraph; what remains is direct.
    const paragraphDirectTs = diffTextStyle(style?.textStyle, namedBase);
    if (style) {
      delete style.textStyle;
      delete style.paragraphMarkTextStyle;
      delete style.namedStyleType;
    }
    let numbering: { numId: string; level: number } | undefined;
    if (paragraph?.bullet && this.numbering)
      numbering = { numId: this.numbering.numIdFor(paragraph.bullet.listType), level: paragraph.bullet.nestingLevel };
    const pPr = pPrXml(style, {
      styleId,
      numbering,
      markTextStyle: paragraph?.paragraphStyle?.paragraphMarkTextStyle,
      sectPrXml,
      extras: paragraph?.docxPPr,
      markExtras: paragraph?.docxMarkRPr,
    });
    const runs = this.runsXml(ctx, from, to, { ...namedBase, ...(paragraphDirectTs ?? {}) }, paragraphDirectTs);
    return `<w:p>${pPr}${runs}</w:p>`;
  }

  /**
   * The style for a heading applied in the editor, which sets `namedStyleType` and leaves `styleId`
   * alone. In patch mode only styles the document has count: its styles.xml is kept as it was.
   */
  private namedStyleOf(paragraph: DocParagraph | undefined): string | undefined {
    const named = paragraph?.paragraphStyle?.namedStyleType;
    if (named === undefined) return undefined;
    if (paragraph?.styleId && this.styles[paragraph.styleId]?.paragraphStyle?.namedStyleType === named) return undefined;
    const known = this.source ? (this.doc.styles ?? {}) : this.styles;
    return Object.entries(known).find(([, s]) => s.type === 1 && s.paragraphStyle?.namedStyleType === named)?.[0];
  }

  /** The runs of one paragraph: text split by run style, hyperlinks, fields and special characters. */
  private runsXml(ctx: WriteContext, from: number, to: number, baseTs: DocTextStyle, paragraphDirectTs: DocTextStyle | undefined): string {
    const stream = ctx.body.dataStream;
    const runs = (ctx.body.textRuns ?? []).filter(r => r.ed > from && r.st < to).sort((a, b) => a.st - b.st);
    const ranges = (ctx.body.customRanges ?? [])
      .filter(r => r.endIndex >= from && r.startIndex < to)
      .sort((a, b) => a.startIndex - b.startIndex);
    const out: string[] = [];
    let i = from;
    const runAt = (pos: number): DocTextRun | undefined => runs.find(r => r.st <= pos && pos < r.ed);
    const flush = (map: Map<number, string[]>, at: number) => {
      const marks = map.get(at);
      if (marks) {
        out.push(...marks);
        map.delete(at);
      }
    };
    // Bookmark and comment marks sit between runs: a run is cut where one falls.
    const nextMark = (p: number, end: number): number => {
      let q = end;
      for (const k of ctx.before.keys()) if (k > p && k < q) q = k;
      for (const k of ctx.after.keys()) if (k + 1 > p && k + 1 < q) q = k + 1;
      return q;
    };
    const rPrChangeAt = (pos: number): string =>
      String(ctx.rPrChanges.find(r => r.startIndex <= pos && pos <= r.endIndex)?.properties?.['docxRPrChange'] ?? '');
    const emitText = (start: number, end: number, open: (ts: string, text: string, at: number) => string) => {
      let p = start;
      while (p < end) {
        flush(ctx.before, p);
        const run = runAt(p);
        const runEnd = Math.min(end, run ? run.ed : (runs.find(r => r.st > p)?.st ?? end), nextMark(p, end));
        const text = stream.slice(p, runEnd);
        const direct = this.directTs(run, baseTs, paragraphDirectTs);
        const rPr = rPrXml(direct, run?.sId && this.styles[run.sId] ? run.sId : undefined);
        out.push(open(withRunExtras(rPr, '', rPrChangeAt(p)), text, p));
        flush(ctx.after, runEnd - 1);
        p = runEnd;
      }
    };
    const runXml = (rPr: string, text: string, at: number): string => {
      const pieces: string[] = [];
      let buf = '';
      const flush = () => {
        if (buf) pieces.push(`<w:t xml:space="preserve">${escapeXml(buf)}</w:t>`);
        buf = '';
      };
      let pos = at - 1;
      for (const ch of text) {
        pos += ch.length;
        if (ch === TOKEN.TAB) {
          flush();
          pieces.push('<w:tab/>');
        } else if (ch === TOKEN.PAGE_BREAK) {
          flush();
          pieces.push('<w:br w:type="page"/>');
        } else if (ch === TOKEN.COLUMN_BREAK) {
          flush();
          // A bare \\v is a soft line break; see BodyBuilder.lineOrColumnBreak.
          pieces.push(ctx.breakAt.get(pos) ?? '<w:br/>');
        } else if (
          ch === TOKEN.CUSTOM_RANGE_START ||
          ch === TOKEN.CUSTOM_RANGE_END ||
          ch === TOKEN.SECTION_BREAK ||
          ch === TOKEN.PARAGRAPH
        ) {
          // Structural tokens never reach a run.
        } else if (ch.charCodeAt(0) < 0x20 && ch !== '\t') {
          // Other control characters are not valid XML.
        } else buf += ch;
      }
      flush();
      return pieces.length ? `<w:r>${rPr}${pieces.join('')}</w:r>` : '';
    };
    while (i < to) {
      const ch = stream[i];
      if (ch === TOKEN.CUSTOM_BLOCK) {
        flush(ctx.before, i);
        const raw = ctx.rawRunAt.get(i);
        const blockId = ctx.blockAt.get(i);
        const drawing = blockId ? ctx.scope.drawings?.[blockId] : undefined;
        if (raw) out.push(raw);
        else if (drawing) out.push(this.drawingRunXml(ctx, drawing));
        else this.fidelity.drop('Blocks without a drawing');
        flush(ctx.after, i);
        i++;
        continue;
      }
      const note = ctx.noteAt.get(i);
      if (note) {
        flush(ctx.before, i);
        out.push(note);
        flush(ctx.after, i);
        i++;
        continue;
      }
      const range = ranges.find(r => r.startIndex === i && (r.rangeType === CUSTOM_RANGE.HYPERLINK || r.rangeType === CUSTOM_RANGE.FIELD));
      if (range) {
        const rangeEnd = Math.min(to, range.endIndex + 1);
        flush(ctx.outerOpen, i);
        const inner: string[] = [];
        const saved = out.length;
        emitText(i, rangeEnd, runXml);
        inner.push(...out.splice(saved));
        if (range.rangeType === CUSTOM_RANGE.HYPERLINK) {
          const url = savedLinkUrl(range.properties);
          const tooltip = range.properties?.['tooltip'] ? ` w:tooltip="${escapeXml(String(range.properties['tooltip']))}"` : '';
          if (url.startsWith('#')) out.push(`<w:hyperlink w:anchor="${escapeXml(url.slice(1))}"${tooltip}>${inner.join('')}</w:hyperlink>`);
          else {
            const relId = ctx.addRel(ctx.rels, REL_TYPE.hyperlink, url, true);
            out.push(`<w:hyperlink r:id="${relId}"${tooltip}>${inner.join('')}</w:hyperlink>`);
          }
        } else {
          const instruction = String(range.properties?.['instruction'] ?? 'PAGE');
          out.push(
            `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ${escapeXml(instruction)} </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>${inner.join('')}<w:r><w:fldChar w:fldCharType="end"/></w:r>`,
          );
        }
        flush(ctx.outerClose, rangeEnd - 1);
        i = rangeEnd;
        continue;
      }
      const nextStop = Math.min(
        to,
        ...ranges
          .filter(r => r.startIndex > i && (r.rangeType === CUSTOM_RANGE.HYPERLINK || r.rangeType === CUSTOM_RANGE.FIELD))
          .map(r => r.startIndex),
        ...[...ctx.noteAt.keys()].filter(k => k > i),
        (() => {
          const b = stream.indexOf(TOKEN.CUSTOM_BLOCK, i);
          return b === -1 || b >= to ? to : b;
        })(),
      );
      emitText(i, nextStop, runXml);
      i = nextStop;
    }
    // Marks on the paragraph mark itself (a collapsed bookmark at the end of a paragraph).
    flush(ctx.before, to);
    flush(ctx.after, to);
    return out.join('');
  }

  /**
   * Bookmarks, comments and note references have no run of their own: they are marks emitted
   * before or after the character they sit on. Comments need the package's comments part, which
   * only patch mode keeps; a fresh file drops them.
   */
  private planMarkers(ctx: WriteContext, body: DocBody, isMain: boolean): void {
    const push = (map: Map<number, string[]>, at: number, xml: string) => map.set(at, [...(map.get(at) ?? []), xml]);
    const pushFirst = (map: Map<number, string[]>, at: number, xml: string) => map.set(at, [xml, ...(map.get(at) ?? [])]);
    const isFlowRange = (r: DocCustomRange) => r.rangeType === CUSTOM_RANGE.HYPERLINK || r.rangeType === CUSTOM_RANGE.FIELD;
    /** Wraps run-level content, staying outside a hyperlink or field it holds whole so the XML nests. */
    const wrap = (start: number, end: number, open: string, close: string) => {
      const flows = (body.customRanges ?? []).filter(isFlowRange);
      if (flows.some(h => h.startIndex === start && h.endIndex <= end)) push(ctx.outerOpen, start, open);
      else push(ctx.before, start, open);
      if (flows.some(h => h.endIndex === end && h.startIndex >= start)) pushFirst(ctx.outerClose, end, close);
      else pushFirst(ctx.after, end, close);
    };
    // A comment thread: every comment in it is marked over the same text, as Word marks replies.
    for (const decoration of body.customDecorations ?? []) {
      const wordIds = decoration.type === CUSTOM_DECORATION_COMMENT ? this.commentWordIds.get(decoration.id) : undefined;
      if (!wordIds?.length) continue;
      const reference = this.styles['CommentReference'] ? '<w:rPr><w:rStyle w:val="CommentReference"/></w:rPr>' : '';
      for (const id of wordIds) push(ctx.before, decoration.startIndex, `<w:commentRangeStart w:id="${id}"/>`);
      for (const id of wordIds)
        push(ctx.after, decoration.endIndex, `<w:commentRangeEnd w:id="${id}"/><w:r>${reference}<w:commentReference w:id="${id}"/></w:r>`);
    }
    const sorted = (body.customRanges ?? []).slice().sort((a, b) => a.startIndex - b.startIndex || b.endIndex - a.endIndex);
    for (const range of sorted) {
      const collapsed = !!range.properties?.['collapsed'];
      switch (range.rangeType) {
        case CUSTOM_RANGE.SDT: {
          const placement = range.properties?.['placement'];
          if (placement === 'block') ctx.blockSdts.push(range);
          else if (placement === 'row') ctx.rowSdts.push(range);
          else if (placement === 'cell') ctx.cellSdts.push(range);
          else {
            paragraphSegments(body.dataStream, range.startIndex, range.endIndex).forEach((segment, n) => {
              const tags = sdtTags(body.dataStream, range, n === 0);
              wrap(segment.start, segment.end, tags.open, tags.close);
            });
          }
          break;
        }
        case CUSTOM_RANGE.CUSTOM: {
          const props = range.properties ?? {};
          if (props['docxRevisionOpen']) {
            for (const segment of paragraphSegments(body.dataStream, range.startIndex, range.endIndex))
              wrap(segment.start, segment.end, String(props['docxRevisionOpen']), String(props['docxRevisionClose']));
          } else if (props['docxRPrChange']) {
            ctx.rPrChanges.push(range);
            // A run is cut where the change's formatting starts and stops.
            push(ctx.before, range.startIndex, '');
            push(ctx.after, range.endIndex, '');
          } else if (props['breakType'] === 'column') ctx.breakAt.set(range.startIndex, '<w:br w:type="column"/>');
          else if (props['docxBreakXml']) ctx.breakAt.set(range.startIndex, String(props['docxBreakXml']));
          break;
        }
        case CUSTOM_RANGE.BOOKMARK: {
          const id = this.bookmarkCount++;
          const name = escapeXml(String(range.properties?.['name'] ?? `bm${id}`));
          push(ctx.before, range.startIndex, `<w:bookmarkStart w:id="${id}" w:name="${name}"/>`);
          if (collapsed) push(ctx.before, range.startIndex, `<w:bookmarkEnd w:id="${id}"/>`);
          else push(ctx.after, range.endIndex, `<w:bookmarkEnd w:id="${id}"/>`);
          break;
        }
        case CUSTOM_RANGE.FOOTNOTE:
        case CUSTOM_RANGE.ENDNOTE: {
          const kind = range.rangeType === CUSTOM_RANGE.FOOTNOTE ? 'footnote' : 'endnote';
          const noteId = String(range.properties?.['noteId'] ?? '');
          if (!isMain || !this.doc.notes?.[noteId]) {
            this.fidelity.drop('Footnotes and endnotes');
            break;
          }
          if (!this.referencedNotes[kind].includes(noteId)) this.referencedNotes[kind].push(noteId);
          const wordId = this.noteWordIds.get(noteId) ?? '1';
          const styleId = kind === 'footnote' ? 'FootnoteReference' : 'EndnoteReference';
          const rPr = this.styles[styleId]
            ? `<w:rPr><w:rStyle w:val="${styleId}"/></w:rPr>`
            : '<w:rPr><w:vertAlign w:val="superscript"/></w:rPr>';
          ctx.noteAt.set(range.startIndex, `<w:r>${rPr}<w:${kind}Reference w:id="${wordId}"/></w:r>`);
          break;
        }
      }
    }
  }

  /** A run's own formatting, relative to the paragraph's named style plus direct paragraph text style. */
  private directTs(
    run: DocTextRun | undefined,
    baseTs: DocTextStyle,
    paragraphDirectTs: DocTextStyle | undefined,
  ): DocTextStyle | undefined {
    const charBase = run?.sId ? resolveTextStyle(this.styles, run.sId) : {};
    const base: DocTextStyle = { ...baseTs, ...charBase };
    const diff = diffTextStyle(run?.ts, base) ?? {};
    // Direct paragraph text formatting has no OOXML home; it rides on every run.
    return Object.keys({ ...(paragraphDirectTs ?? {}), ...diff }).length ? { ...(paragraphDirectTs ?? {}), ...diff } : undefined;
  }

  // --- drawings ------------------------------------------------------------------------------

  private drawingRunXml(ctx: WriteContext, drawing: DocDrawing): string {
    const parsed = drawing.imageSourceType === 'BASE64' ? parseDataUrl(drawing.source) : null;
    if (!parsed) {
      this.fidelity.drop('Linked images');
      return '';
    }
    const ext = extensionForMime(parsed.mime);
    if (ext === 'bin') {
      this.fidelity.drop('Images of an unsupported type');
      return '';
    }
    const n = ++this.mediaCount.n;
    const existing = this.source?.mediaPathFor(drawing.source) ?? null;
    let relId: string;
    if (existing) {
      relId = ctx.addRel(ctx.rels, REL_TYPE.image, relativeTo(ctx.partPath, existing));
    } else {
      let name = `image${n}.${ext}`;
      for (let k = n; this.mediaNames.has(name); k++) name = `image${k + 1}.${ext}`;
      this.mediaNames.add(name);
      const dir = `${dirOf(ctx.partPath)}media/`;
      ctx.pkg.put(`${dir}${name}`, parsed.bytes);
      relId = ctx.addRel(ctx.rels, REL_TYPE.image, `media/${name}`);
      if (this.source && !this.source.hasDefault(ext)) this.defaults.push({ extension: ext, contentType: parsed.mime });
    }
    const { width, height } = drawing.docTransform.size;
    const cx = pxToEmu(width);
    const cy = pxToEmu(height);
    const name = escapeXml(drawing.title || `Picture ${n}`);
    const descr = escapeXml(drawing.description ?? '');
    const graphic =
      `<a:graphic xmlns:a="${NS.a}"><a:graphicData uri="${NS.pic}"><pic:pic xmlns:pic="${NS.pic}">` +
      `<pic:nvPicPr><pic:cNvPr id="${n}" name="${name}" descr="${descr}"/><pic:cNvPicPr/></pic:nvPicPr>` +
      `<pic:blipFill><a:blip r:embed="${relId}"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>` +
      `<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="${cx}" cy="${cy}"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>` +
      `</pic:pic></a:graphicData></a:graphic>`;
    const docPr = `<wp:docPr id="${n}" name="${name}" descr="${descr}"/>`;
    if (drawing.layoutType === LAYOUT.inline) {
      return `<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="${cx}" cy="${cy}"/><wp:effectExtent l="0" t="0" r="0" b="0"/>${docPr}<wp:cNvGraphicFramePr/>${graphic}</wp:inline></w:drawing></w:r>`;
    }
    const pos = (
      p: { relativeFrom: number; posOffset?: number; align?: number },
      relatives: Record<string, number>,
      aligns: Record<string, number>,
      tag: string,
    ) => {
      const rel = Object.entries(relatives).find(([, v]) => v === p.relativeFrom)?.[0] ?? 'column';
      const alignName = p.align !== undefined ? Object.entries(aligns).find(([, v]) => v === p.align)?.[0] : undefined;
      return `<wp:${tag} relativeFrom="${rel}">${alignName ? `<wp:align>${alignName}</wp:align>` : `<wp:posOffset>${pxToEmu(p.posOffset ?? 0)}</wp:posOffset>`}</wp:${tag}>`;
    };
    const wrapText = drawing.wrapText === 1 ? 'left' : drawing.wrapText === 2 ? 'right' : drawing.wrapText === 3 ? 'largest' : 'bothSides';
    const wrap =
      drawing.layoutType === LAYOUT.none
        ? '<wp:wrapNone/>'
        : drawing.layoutType === LAYOUT.topAndBottom
          ? '<wp:wrapTopAndBottom/>'
          : drawing.layoutType === LAYOUT.tight
            ? `<wp:wrapTight wrapText="${wrapText}"><wp:wrapPolygon edited="0"><wp:start x="0" y="0"/><wp:lineTo x="0" y="21600"/><wp:lineTo x="21600" y="21600"/><wp:lineTo x="21600" y="0"/><wp:lineTo x="0" y="0"/></wp:wrapPolygon></wp:wrapTight>`
            : `<wp:wrapSquare wrapText="${wrapText}"/>`;
    return (
      `<w:r><w:drawing><wp:anchor distT="${pxToEmu(drawing.distT ?? 0)}" distB="${pxToEmu(drawing.distB ?? 0)}" distL="${pxToEmu(drawing.distL ?? 0)}" distR="${pxToEmu(drawing.distR ?? 0)}" simplePos="0" relativeHeight="${251658240 + n}" behindDoc="${drawing.behindDoc ?? 0}" locked="0" layoutInCell="1" allowOverlap="1">` +
      `<wp:simplePos x="0" y="0"/>${pos(drawing.docTransform.positionH, H_RELATIVE, H_ALIGN, 'positionH')}${pos(drawing.docTransform.positionV, V_RELATIVE, V_ALIGN, 'positionV')}` +
      `<wp:extent cx="${cx}" cy="${cy}"/><wp:effectExtent l="0" t="0" r="0" b="0"/>${wrap}${docPr}<wp:cNvGraphicFramePr/>${graphic}</wp:anchor></w:drawing></w:r>`
    );
  }

  // --- tables --------------------------------------------------------------------------------

  private tableXml(ctx: WriteContext, from: number, to: number, tableId: string): string {
    const table = ctx.scope.tableSource?.[tableId];
    const stream = ctx.body.dataStream;
    if (!table) {
      this.fidelity.drop('Tables without a definition');
      return '';
    }
    const widths = table.tableColumns.map(c => pxToTwips(c.size.width.v));
    const tblW =
      table.size.type === 1
        ? table.size.width.u === 4
          ? `<w:tblW w:w="${Math.round(table.size.width.v * 50)}" w:type="pct"/>`
          : `<w:tblW w:w="${pxToTwips(table.size.width.v)}" w:type="dxa"/>`
        : '<w:tblW w:w="0" w:type="auto"/>';
    const jc = table.align === 1 ? '<w:jc w:val="center"/>' : table.align === 2 ? '<w:jc w:val="right"/>' : '';
    const ind = table.indent?.v ? `<w:tblInd w:w="${pxToTwips(table.indent.v)}" w:type="dxa"/>` : '';
    const layout = table.layout === 1 ? '<w:tblLayout w:type="fixed"/>' : '';
    const mar = table.cellMargin
      ? `<w:tblCellMar><w:top w:w="${pxToTwips(table.cellMargin.top.v)}" w:type="dxa"/><w:left w:w="${pxToTwips(table.cellMargin.start.v)}" w:type="dxa"/><w:bottom w:w="${pxToTwips(table.cellMargin.bottom.v)}" w:type="dxa"/><w:right w:w="${pxToTwips(table.cellMargin.end.v)}" w:type="dxa"/></w:tblCellMar>`
      : '';
    const caption = table.description ? `<w:tblCaption w:val="${escapeXml(table.description)}"/>` : '';
    // A table from a file keeps its own properties (style, look, borders); the editor cannot change them.
    const tblPr = table.docxTblPr ?? `<w:tblPr>${tblW}${jc}${ind}${layout}${mar}${caption}</w:tblPr>`;
    const grid = `<w:tblGrid>${widths.map(w => `<w:gridCol w:w="${w}"/>`).join('')}</w:tblGrid>`;
    // Walk the stream row by row, cell by cell, in step with tableRows/tableCells.
    const rows: { xml: string; start: number; end: number }[] = [];
    let i = from + 1; // past TABLE_START
    let rowIndex = 0;
    // Vertical merges: a cell with rowSpan > 1 marks the covered cells below as continuations.
    const vMergeContinue = new Set<string>();
    while (i < to && stream[i] === TOKEN.TABLE_ROW_START) {
      const row = table.tableRows[rowIndex];
      i++;
      const rowStart = i - 1;
      const cells: { xml: string; start: number; end: number }[] = [];
      let cellIndex = 0;
      let gridCol = 0;
      while (i < to && stream[i] === TOKEN.TABLE_CELL_START) {
        const cellStart = i + 1;
        let depth = 0;
        let j = cellStart;
        // Find the matching cell end, skipping nested tables.
        while (j < to) {
          const c = stream[j];
          if (c === TOKEN.TABLE_START) depth++;
          else if (c === TOKEN.TABLE_END) depth--;
          else if (c === TOKEN.TABLE_CELL_END && depth === 0) break;
          j++;
        }
        const cell = row?.tableCells[cellIndex];
        const covered = cell && (cell.rowSpan === 0 || cell.columnSpan === 0);
        if (!covered) {
          const span = cell?.columnSpan ?? 1;
          const continues = vMergeContinue.has(`${rowIndex}:${gridCol}`);
          if (cell?.rowSpan && cell.rowSpan > 1) for (let r = 1; r < cell.rowSpan; r++) vMergeContinue.add(`${rowIndex + r}:${gridCol}`);
          cells.push({
            start: cellStart - 1,
            end: j,
            xml: this.cellXml(
              ctx,
              cellStart,
              j - 1,
              cell,
              widths.slice(gridCol, gridCol + span).reduce((a, b) => a + b, 0),
              continues ? 'continue' : cell?.rowSpan && cell.rowSpan > 1 ? 'restart' : null,
            ),
          });
          gridCol += span;
        } else if (vMergeContinue.has(`${rowIndex}:${gridCol}`)) {
          // The covered cell under a vertical merge is written as a continuation cell.
          cells.push({
            start: cellStart - 1,
            end: j,
            xml: this.cellXml(ctx, cellStart, cellStart, cell, widths[gridCol] ?? 0, 'continue'),
          });
          gridCol += 1;
        } else {
          gridCol += 1;
        }
        cellIndex++;
        i = j + 1;
      }
      const rowEnd = i;
      if (stream[i] === TOKEN.TABLE_ROW_END) i++;
      const trPr: { name: string; xml: string }[] = [];
      if (row?.trHeight && row.trHeight.hRule !== 0)
        trPr.push({
          name: 'trHeight',
          xml: `<w:trHeight w:val="${pxToTwips(row.trHeight.val.v)}" w:hRule="${row.trHeight.hRule === 2 ? 'exact' : 'atLeast'}"/>`,
        });
      if (row?.cantSplit) trPr.push({ name: 'cantSplit', xml: '<w:cantSplit/>' });
      if (row?.repeatHeaderRow) trPr.push({ name: 'tblHeader', xml: '<w:tblHeader/>' });
      const trPrXml = orderedProperties(trPr, row?.docxTrPr, TRPR_ORDER);
      wrapControls(stream, cells, ctx.cellSdts);
      rows.push({
        start: rowStart,
        end: rowEnd,
        xml: `<w:tr>${trPrXml ? `<w:trPr>${trPrXml}</w:trPr>` : ''}${cells.map(c => c.xml).join('')}</w:tr>`,
      });
      rowIndex++;
    }
    wrapControls(stream, rows, ctx.rowSdts);
    return `<w:tbl>${tblPr}${grid}${rows.map(r => r.xml).join('')}</w:tbl>`;
  }

  private cellXml(
    ctx: WriteContext,
    from: number,
    to: number,
    cell: DocTableCell | undefined,
    widthTwips: number,
    vMerge: 'restart' | 'continue' | null,
  ): string {
    const tcPr: { name: string; xml: string }[] = [];
    const add = (name: string, xml: string) => tcPr.push({ name, xml });
    add('tcW', `<w:tcW w:w="${widthTwips}" w:type="dxa"/>`);
    if (cell?.columnSpan && cell.columnSpan > 1) add('gridSpan', `<w:gridSpan w:val="${cell.columnSpan}"/>`);
    if (vMerge === 'restart') add('vMerge', '<w:vMerge w:val="restart"/>');
    else if (vMerge === 'continue') add('vMerge', '<w:vMerge/>');
    const borders = [
      cellBorderXml('top', cell?.borderTop),
      cellBorderXml('left', cell?.borderLeft),
      cellBorderXml('bottom', cell?.borderBottom),
      cellBorderXml('right', cell?.borderRight),
    ].join('');
    if (borders) add('tcBorders', `<w:tcBorders>${borders}</w:tcBorders>`);
    const fill = colorHex(cell?.backgroundColor?.rgb);
    if (fill) add('shd', `<w:shd w:val="clear" w:color="auto" w:fill="${fill}"/>`);
    if (cell?.margin)
      add(
        'tcMar',
        `<w:tcMar><w:top w:w="${pxToTwips(cell.margin.top.v)}" w:type="dxa"/><w:left w:w="${pxToTwips(cell.margin.start.v)}" w:type="dxa"/><w:bottom w:w="${pxToTwips(cell.margin.bottom.v)}" w:type="dxa"/><w:right w:w="${pxToTwips(cell.margin.end.v)}" w:type="dxa"/></w:tcMar>`,
      );
    if (cell?.vAlign === 3) add('vAlign', '<w:vAlign w:val="center"/>');
    else if (cell?.vAlign === 4) add('vAlign', '<w:vAlign w:val="bottom"/>');
    const out: string[] = [];
    if (to > from) {
      const sections = (ctx.body.sectionBreaks ?? []).filter(s => s.startIndex >= from && s.startIndex <= to);
      this.blocksXml(ctx, from, to, out, sections, false);
    }
    if (!out.length) out.push('<w:p/>');
    return `<w:tc><w:tcPr>${orderedProperties(tcPr, cell?.docxTcPr, TCPR_ORDER)}</w:tcPr>${out.join('')}</w:tc>`;
  }

  // --- sections ------------------------------------------------------------------------------

  private sectPrXml(
    setup: DocPageSetup & Partial<Pick<DocSectionBreak, 'sectionType' | 'columnProperties' | 'columnSeparatorType'>>,
    refFor: (kind: 'header' | 'footer', id: string) => string | null,
  ): string {
    const parts: string[] = [];
    const ref = (kind: 'header' | 'footer', type: 'default' | 'first' | 'even', id: string | undefined) => {
      if (!id) return;
      const relId = refFor(kind, id);
      if (relId) parts.push(`<w:${kind}Reference w:type="${type}" r:id="${relId}"/>`);
    };
    ref('header', 'default', setup.defaultHeaderId);
    ref('header', 'first', setup.firstPageHeaderId);
    ref('header', 'even', setup.evenPageHeaderId);
    ref('footer', 'default', setup.defaultFooterId);
    ref('footer', 'first', setup.firstPageFooterId);
    ref('footer', 'even', setup.evenPageFooterId);
    if (setup.sectionType !== undefined && setup.sectionType !== 3)
      parts.push(
        `<w:type w:val="${setup.sectionType === 1 ? 'continuous' : setup.sectionType === 4 ? 'evenPage' : setup.sectionType === 5 ? 'oddPage' : 'nextPage'}"/>`,
      );
    const size = setup.pageSize ?? { width: 794, height: 1123 };
    parts.push(
      `<w:pgSz w:w="${pxToTwips(size.width)}" w:h="${pxToTwips(size.height)}"${setup.pageOrient === 1 ? ' w:orient="landscape"' : ''}/>`,
    );
    parts.push(
      `<w:pgMar w:top="${pxToTwips(setup.marginTop ?? 96)}" w:right="${pxToTwips(setup.marginRight ?? 96)}" w:bottom="${pxToTwips(setup.marginBottom ?? 96)}" w:left="${pxToTwips(setup.marginLeft ?? 96)}" w:header="${pxToTwips(setup.marginHeader ?? 48)}" w:footer="${pxToTwips(setup.marginFooter ?? 48)}" w:gutter="0"/>`,
    );
    if (setup.pageNumberStart !== undefined) parts.push(`<w:pgNumType w:start="${setup.pageNumberStart}"/>`);
    if (setup.columnProperties && setup.columnProperties.length > 1)
      parts.push(
        `<w:cols w:num="${setup.columnProperties.length}" w:space="${pxToTwips(setup.columnProperties[0].paddingEnd)}"${setup.columnSeparatorType === 2 ? ' w:sep="1"' : ''}/>`,
      );
    if (setup.useFirstPageHeaderFooter) parts.push('<w:titlePg/>');
    if (!setup.docxSectPr) return `<w:sectPr>${parts.join('')}</w:sectPr>`;
    return `<w:sectPr>${orderedProperties(splitTop(parts.join('')), setup.docxSectPr, SECTPR_ORDER)}</w:sectPr>`;
  }

  private settingsXml(): string {
    const parts: string[] = [];
    if (this.doc.documentStyle.defaultTabStop !== undefined)
      parts.push(`<w:defaultTabStop w:val="${pxToTwips(this.doc.documentStyle.defaultTabStop)}"/>`);
    if (this.doc.documentStyle.evenAndOddHeaders) parts.push('<w:evenAndOddHeaders/>');
    parts.push(
      '<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>',
    );
    return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:settings xmlns:w="${NS.w}">${parts.join('')}</w:settings>`;
  }
}

/**
 * Wraps the rows or cells a content control spans. One whose ends no longer fall on a row or cell
 * (an edit took them away) is left out: the content stays, the control goes.
 */
function wrapControls(stream: string, items: { xml: string; start: number; end: number }[], controls: DocCustomRange[]): void {
  const here = controls
    .filter(r => items.some(it => it.start === r.startIndex) && items.some(it => it.end === r.endIndex))
    .sort((a, b) => a.endIndex - a.startIndex - (b.endIndex - b.startIndex));
  for (const range of here) {
    const first = items.find(it => it.start === range.startIndex)!;
    const last = items.find(it => it.end === range.endIndex)!;
    const tags = sdtTags(stream, range);
    first.xml = tags.open + first.xml;
    last.xml += tags.close;
  }
}

interface WriteContext {
  body: DocBody;
  scope: DocDocument;
  pkg: OoxmlPackage;
  rels: PartRels;
  addRel: (rels: PartRels, type: string, target: string, external?: boolean) => string;
  partPath: string;
  paragraphAt: Map<number, DocParagraph>;
  tableAt: Map<number, { startIndex: number; endIndex: number; tableId: string }>;
  blockAt: Map<number, string>;
  rawAt: Map<number, string[]>;
  /** Opaque runs kept from the original, by the sentinel they sit on. */
  rawRunAt: Map<number, string>;
  /** Footnote and endnote reference runs, by the object character they replace. */
  noteAt: Map<number, string>;
  /** Marks (bookmarks, comment ranges) emitted before the character at a position, and after it. */
  before: Map<number, string[]>;
  after: Map<number, string[]>;
  /** Tags that go outside a hyperlink or field starting or ending here. */
  outerOpen: Map<number, string[]>;
  outerClose: Map<number, string[]>;
  /** The XML of a line or column break that is not a plain `<w:br/>`. */
  breakAt: Map<number, string>;
  rPrChanges: DocCustomRange[];
  blockSdts: DocCustomRange[];
  rowSdts: DocCustomRange[];
  cellSdts: DocCustomRange[];
}

function dirOf(path: string): string {
  return path.slice(0, path.lastIndexOf('/') + 1);
}

function baseOf(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1);
}

/** A relationship target from one part to another (both package-absolute, no leading slash). */
function relativeTo(fromPart: string, toPart: string): string {
  const fromDir = dirOf(fromPart).split('/').filter(Boolean);
  const to = toPart.split('/');
  let i = 0;
  while (i < fromDir.length && i < to.length - 1 && fromDir[i] === to[i]) i++;
  return [...Array(fromDir.length - i).fill('..'), ...to.slice(i)].join('/');
}

/** Appends new abstract numbering definitions before the first w:num and the new nums before the end. */
function appendNumbering(original: string, fragment: string): string {
  const split = fragment.indexOf('<w:num ');
  const abstracts = split === -1 ? fragment : fragment.slice(0, split);
  const nums = split === -1 ? '' : fragment.slice(split);
  let xml = original;
  const firstNum = xml.search(/<w:num[\s>]/);
  const end = xml.lastIndexOf('</w:numbering>');
  if (end === -1) return original;
  if (firstNum !== -1) xml = xml.slice(0, firstNum) + abstracts + xml.slice(firstNum);
  const end2 = xml.lastIndexOf('</w:numbering>');
  return xml.slice(0, end2) + (firstNum === -1 ? abstracts : '') + nums + xml.slice(end2);
}

/** The top-level elements of a fragment we built, named, for ordering with kept extras. */
function splitTop(xml: string): { name: string; xml: string }[] {
  const out: { name: string; xml: string }[] = [];
  const re = /<w:(\w+)\b[^>]*?(?:\/>|>[\s\S]*?<\/w:\1>)/g;
  for (const m of xml.matchAll(re)) out.push({ name: m[1], xml: m[0] });
  return out;
}

const NAMESPACES = `xmlns:w="${NS.w}" xmlns:r="${NS.r}" xmlns:wp="${NS.wp}" xmlns:a="${NS.a}" xmlns:pic="${NS.pic}" xmlns:mc="${NS.mc}" xmlns:v="${NS.v}"`;

/**
 * The root element's namespace declarations: ours, plus every one the original part declared and
 * its mc:Ignorable list. Kept content (text boxes as mc:Choice Requires="wps", w14 attributes)
 * names those prefixes, and Markup Compatibility requires them declared on the root.
 */
export function rootNamespaces(originalXml: string | null | undefined): string {
  const attrs = new Map<string, string>();
  const start = originalXml ? (/<(?:\w+:)?(?:document|hdr|ftr)\b([^>]*)>/.exec(originalXml)?.[1] ?? '') : '';
  for (const m of start.matchAll(/\s(xmlns:\w+|mc:Ignorable)="([^"]*)"/g)) attrs.set(m[1], m[2]);
  for (const m of NAMESPACES.matchAll(/(xmlns:\w+)="([^"]*)"/g)) attrs.set(m[1], m[2]);
  return [...attrs].map(([name, value]) => `${name}="${escapeXml(value)}"`).join(' ');
}

function cellBorderXml(name: string, b: DocTableCellBorder | undefined): string {
  // Unset is a table made in the editor, which Univer draws with its default line: say so, so the
  // saved file looks the same in Word, where an unset side has none.
  if (!b) return `<w:${name} w:val="single" w:sz="4" w:space="0" w:color="C7C9CC"/>`;
  if (isNoBorder(b)) return `<w:${name} w:val="nil"/>`;
  const val = b.dashStyle === 2 ? 'dotted' : b.dashStyle === 3 ? 'dashed' : 'single';
  return `<w:${name} w:val="${val}" w:sz="${pxToEighths(b.width?.v ?? 0.5)}" w:space="0" w:color="${colorHex(b.color.rgb) ?? 'auto'}"/>`;
}

function override(partName: string, contentType: string): string {
  return `<Override PartName="${partName}" ContentType="${contentType}"/>`;
}

function relsXml(rels: PartRels): string {
  const entries = rels.entries
    .map(r => `<Relationship Id="${r.id}" Type="${r.type}" Target="${escapeXml(r.target)}"${r.external ? ' TargetMode="External"' : ''}/>`)
    .join('');
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="${NS.rel}">${entries}</Relationships>`;
}

function coreXml(title: string): string {
  const now = new Date().toISOString();
  return (
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
    `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
    `<dc:title>${escapeXml(title)}</dc:title><dcterms:created xsi:type="dcterms:W3CDTF">${now}</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">${now}</dcterms:modified></cp:coreProperties>`
  );
}

export { DOCX_MIME };
export type { DocTable };
