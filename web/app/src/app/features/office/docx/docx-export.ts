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
import { H_ALIGN, H_RELATIVE, V_ALIGN, V_RELATIVE } from './docx-body';
import { defaultNamedStyles, diffTextStyle, numberingXml, pPrXml, rPrXml, resolveTextStyle, stylesXml } from './docx-styles';
import {
  CUSTOM_RANGE,
  DocBody,
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
} from './univer-doc-types';

export interface DocxExportResult {
  bytes: Uint8Array;
  report: FidelityReport;
}

const DOCX_MIME = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';

/** Writes a Univer document snapshot as a .docx; the mirror of docxToDocument. */
export async function documentToDocx(document: DocDocument): Promise<DocxExportResult> {
  const fidelity = new FidelityCollector();
  const pkg = OoxmlPackage.empty();
  const writer = new DocxWriter(document, fidelity);
  writer.write(pkg);
  return { bytes: await pkg.save(DOCX_MIME), report: fidelity.report() };
}

interface PartRels {
  entries: { id: string; type: string; target: string; external?: boolean }[];
  next: number;
}

class DocxWriter {
  private readonly styles: NonNullable<DocDocument['styles']>;
  private readonly numbering: ReturnType<typeof numberingXml>;
  private readonly overrides: string[] = [];
  private readonly mediaCount = { n: 0 };
  private headerCount = 0;
  private footerCount = 0;

  constructor(
    private readonly doc: DocDocument,
    private readonly fidelity: FidelityCollector,
  ) {
    const defaults = defaultNamedStyles();
    this.styles = { ...defaults, ...(doc.styles ?? {}) };
    this.numbering = numberingXml(doc.lists ?? {});
  }

  write(pkg: OoxmlPackage): void {
    const docRels: PartRels = { entries: [], next: 1 };
    const addRel = (rels: PartRels, type: string, target: string, external = false) => {
      const id = `rId${rels.next++}`;
      rels.entries.push({ id, type, target, external });
      return id;
    };
    addRel(docRels, REL_TYPE.styles, 'styles.xml');
    addRel(docRels, REL_TYPE.settings, 'settings.xml');
    if (this.numbering) {
      addRel(docRels, REL_TYPE.numbering, 'numbering.xml');
      pkg.put('word/numbering.xml', this.numbering.xml);
      this.overrides.push(override('/word/numbering.xml', 'application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml'));
    }
    const sectPr = this.sectPrXml(this.doc.documentStyle, (kind, id) => this.headerFooterPart(pkg, docRels, addRel, kind, id));
    const bodyXml = this.bodyXml(this.doc.body, this.doc, pkg, docRels, addRel, 'word/document.xml', sectPr);
    pkg.put(
      'word/document.xml',
      `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
        `<w:document ${NAMESPACES}><w:body>${bodyXml}${sectPr}</w:body></w:document>`,
    );
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
        this.overrides.join('') +
        `</Types>`,
    );
  }

  private headerFooterPart(
    pkg: OoxmlPackage,
    docRels: PartRels,
    addRel: (rels: PartRels, type: string, target: string, external?: boolean) => string,
    kind: 'header' | 'footer',
    id: string,
  ): string | null {
    const source: DocHeaderFooter | undefined = kind === 'header' ? this.doc.headers?.[id] : this.doc.footers?.[id];
    if (!source) return null;
    const n = kind === 'header' ? ++this.headerCount : ++this.footerCount;
    const name = `${kind}${n}.xml`;
    const partRels: PartRels = { entries: [], next: 1 };
    const scope: DocDocument = { ...this.doc, tableSource: source.tableSource ?? {}, drawings: source.drawings ?? {} };
    const xml = this.bodyXml(source.body, scope, pkg, partRels, addRel, `word/${name}`);
    const tag = kind === 'header' ? 'w:hdr' : 'w:ftr';
    pkg.put(`word/${name}`, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><${tag} ${NAMESPACES}>${xml}</${tag}>`);
    if (partRels.entries.length) pkg.put(`word/_rels/${name}.rels`, relsXml(partRels));
    this.overrides.push(override(`/word/${name}`, `application/vnd.openxmlformats-officedocument.wordprocessingml.${kind}+xml`));
    return addRel(docRels, kind === 'header' ? REL_TYPE.header : REL_TYPE.footer, name);
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
    };
    for (const p of body.paragraphs ?? []) ctx.paragraphAt.set(p.startIndex, p);
    for (const t of body.tables ?? []) ctx.tableAt.set(t.startIndex, t);
    for (const b of body.customBlocks ?? []) ctx.blockAt.set(b.startIndex, b.blockId);
    for (const r of body.docxRawBlocks ?? []) ctx.rawAt.set(r.startIndex, [...(ctx.rawAt.get(r.startIndex) ?? []), r.xml]);
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
    while (i < to) {
      emitRaw(i);
      const ch = stream[i];
      if (ch === TOKEN.TABLE_START) {
        const table = ctx.tableAt.get(i);
        if (!table) {
          i++;
          continue;
        }
        out.push(this.tableXml(ctx, table.startIndex, table.endIndex, table.tableId));
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
        out.push(this.paragraphXml(ctx, paragraphStart, i, paragraph, sectXml));
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
  }

  private paragraphXml(ctx: WriteContext, from: number, to: number, paragraph: DocParagraph | undefined, sectPrXml: string): string {
    const style = paragraph?.paragraphStyle ? { ...paragraph.paragraphStyle } : undefined;
    const styleId = paragraph?.styleId && this.styles[paragraph.styleId] ? paragraph.styleId : undefined;
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
    const pPr = pPrXml(style, { styleId, numbering, markTextStyle: paragraph?.paragraphStyle?.paragraphMarkTextStyle, sectPrXml });
    const runs = this.runsXml(ctx, from, to, { ...namedBase, ...(paragraphDirectTs ?? {}) }, paragraphDirectTs);
    return `<w:p>${pPr}${runs}</w:p>`;
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
    const emitText = (start: number, end: number, open: (ts: string, text: string) => string) => {
      let p = start;
      while (p < end) {
        const run = runAt(p);
        const runEnd = Math.min(end, run ? run.ed : (runs.find(r => r.st > p)?.st ?? end));
        const text = stream.slice(p, runEnd);
        const direct = this.directTs(run, baseTs, paragraphDirectTs);
        out.push(open(rPrXml(direct, run?.sId && this.styles[run.sId] ? run.sId : undefined), text));
        p = runEnd;
      }
    };
    const runXml = (rPr: string, text: string): string => {
      const pieces: string[] = [];
      let buf = '';
      const flush = () => {
        if (buf) pieces.push(`<w:t xml:space="preserve">${escapeXml(buf)}</w:t>`);
        buf = '';
      };
      for (const ch of text) {
        if (ch === TOKEN.TAB) {
          flush();
          pieces.push('<w:tab/>');
        } else if (ch === TOKEN.PAGE_BREAK) {
          flush();
          pieces.push('<w:br w:type="page"/>');
        } else if (ch === TOKEN.COLUMN_BREAK) {
          flush();
          pieces.push('<w:br w:type="column"/>');
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
        const blockId = ctx.blockAt.get(i);
        const drawing = blockId ? ctx.scope.drawings?.[blockId] : undefined;
        if (drawing) out.push(this.drawingRunXml(ctx, drawing));
        else this.fidelity.drop('Blocks without a drawing');
        i++;
        continue;
      }
      const range = ranges.find(r => r.startIndex === i);
      if (range && (range.rangeType === CUSTOM_RANGE.HYPERLINK || range.rangeType === CUSTOM_RANGE.FIELD)) {
        const rangeEnd = Math.min(to, range.endIndex + 1);
        const inner: string[] = [];
        const saved = out.length;
        emitText(i, rangeEnd, runXml);
        inner.push(...out.splice(saved));
        if (range.rangeType === CUSTOM_RANGE.HYPERLINK) {
          const url = String(range.properties?.['url'] ?? '');
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
        i = rangeEnd;
        continue;
      }
      const nextStop = Math.min(
        to,
        ...ranges.filter(r => r.startIndex > i).map(r => r.startIndex),
        (() => {
          const b = stream.indexOf(TOKEN.CUSTOM_BLOCK, i);
          return b === -1 || b >= to ? to : b;
        })(),
      );
      emitText(i, nextStop, runXml);
      i = nextStop;
    }
    return out.join('');
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
    const target = `media/image${n}.${ext}`;
    ctx.pkg.put(`word/${target}`, parsed.bytes);
    const relId = ctx.addRel(ctx.rels, REL_TYPE.image, target);
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
    const tblPr = `<w:tblPr>${tblW}${jc}${ind}${layout}${mar}${caption}</w:tblPr>`;
    const grid = `<w:tblGrid>${widths.map(w => `<w:gridCol w:w="${w}"/>`).join('')}</w:tblGrid>`;
    // Walk the stream row by row, cell by cell, in step with tableRows/tableCells.
    const rows: string[] = [];
    let i = from + 1; // past TABLE_START
    let rowIndex = 0;
    // Vertical merges: a cell with rowSpan > 1 marks the covered cells below as continuations.
    const vMergeContinue = new Set<string>();
    while (i < to && stream[i] === TOKEN.TABLE_ROW_START) {
      const row = table.tableRows[rowIndex];
      i++;
      const cells: string[] = [];
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
          cells.push(
            this.cellXml(
              ctx,
              cellStart,
              j - 1,
              cell,
              widths.slice(gridCol, gridCol + span).reduce((a, b) => a + b, 0),
              continues ? 'continue' : cell?.rowSpan && cell.rowSpan > 1 ? 'restart' : null,
            ),
          );
          gridCol += span;
        } else if (vMergeContinue.has(`${rowIndex}:${gridCol}`)) {
          // The covered cell under a vertical merge is written as a continuation cell.
          cells.push(this.cellXml(ctx, cellStart, cellStart, cell, widths[gridCol] ?? 0, 'continue'));
          gridCol += 1;
        } else {
          gridCol += 1;
        }
        cellIndex++;
        i = j + 1;
      }
      if (stream[i] === TOKEN.TABLE_ROW_END) i++;
      const trPr: string[] = [];
      if (row?.trHeight && row.trHeight.hRule !== 0)
        trPr.push(`<w:trHeight w:val="${pxToTwips(row.trHeight.val.v)}" w:hRule="${row.trHeight.hRule === 2 ? 'exact' : 'atLeast'}"/>`);
      if (row?.cantSplit) trPr.push('<w:cantSplit/>');
      if (row?.repeatHeaderRow) trPr.push('<w:tblHeader/>');
      rows.push(`<w:tr>${trPr.length ? `<w:trPr>${trPr.join('')}</w:trPr>` : ''}${cells.join('')}</w:tr>`);
      rowIndex++;
    }
    return `<w:tbl>${tblPr}${grid}${rows.join('')}</w:tbl>`;
  }

  private cellXml(
    ctx: WriteContext,
    from: number,
    to: number,
    cell: DocTableCell | undefined,
    widthTwips: number,
    vMerge: 'restart' | 'continue' | null,
  ): string {
    const tcPr: string[] = [];
    tcPr.push(`<w:tcW w:w="${widthTwips}" w:type="dxa"/>`);
    if (cell?.columnSpan && cell.columnSpan > 1) tcPr.push(`<w:gridSpan w:val="${cell.columnSpan}"/>`);
    if (vMerge === 'restart') tcPr.push('<w:vMerge w:val="restart"/>');
    else if (vMerge === 'continue') tcPr.push('<w:vMerge/>');
    const borders = [
      cellBorderXml('top', cell?.borderTop),
      cellBorderXml('left', cell?.borderLeft),
      cellBorderXml('bottom', cell?.borderBottom),
      cellBorderXml('right', cell?.borderRight),
    ].join('');
    if (borders) tcPr.push(`<w:tcBorders>${borders}</w:tcBorders>`);
    const fill = colorHex(cell?.backgroundColor?.rgb);
    if (fill) tcPr.push(`<w:shd w:val="clear" w:color="auto" w:fill="${fill}"/>`);
    if (cell?.margin)
      tcPr.push(
        `<w:tcMar><w:top w:w="${pxToTwips(cell.margin.top.v)}" w:type="dxa"/><w:left w:w="${pxToTwips(cell.margin.start.v)}" w:type="dxa"/><w:bottom w:w="${pxToTwips(cell.margin.bottom.v)}" w:type="dxa"/><w:right w:w="${pxToTwips(cell.margin.end.v)}" w:type="dxa"/></w:tcMar>`,
      );
    if (cell?.vAlign === 3) tcPr.push('<w:vAlign w:val="center"/>');
    else if (cell?.vAlign === 4) tcPr.push('<w:vAlign w:val="bottom"/>');
    const out: string[] = [];
    if (to > from) {
      const sections = (ctx.body.sectionBreaks ?? []).filter(s => s.startIndex >= from && s.startIndex <= to);
      this.blocksXml(ctx, from, to, out, sections, false);
    }
    if (!out.length) out.push('<w:p/>');
    return `<w:tc><w:tcPr>${tcPr.join('')}</w:tcPr>${out.join('')}</w:tc>`;
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
    return `<w:sectPr>${parts.join('')}</w:sectPr>`;
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
}

const NAMESPACES = `xmlns:w="${NS.w}" xmlns:r="${NS.r}" xmlns:wp="${NS.wp}" xmlns:a="${NS.a}" xmlns:pic="${NS.pic}" xmlns:mc="${NS.mc}" xmlns:v="${NS.v}"`;

function cellBorderXml(name: string, b: DocTableCellBorder | undefined): string {
  if (!b) return '';
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
