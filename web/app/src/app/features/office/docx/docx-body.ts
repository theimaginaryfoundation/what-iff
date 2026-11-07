import { FidelityCollector } from '../shared/fidelity';
import {
  NS,
  Relationship,
  attr,
  child,
  children,
  elementChildren,
  emuToPx,
  eighthsToPx,
  hexColor,
  num,
  serializeXml,
  toggle,
  twipsToPx,
  wval,
} from '../shared/ooxml';
import {
  CUSTOM_RANGE,
  DocBody,
  DocCustomBlock,
  DocCustomRange,
  DocDrawing,
  DocNamedStyle,
  DocParagraph,
  DocParagraphStyle,
  DocSectionBreak,
  DocTable,
  DocTableCell,
  DocTableCellBorder,
  DocTableRow,
  DocTextRun,
  DocTextStyle,
  LAYOUT,
  TOKEN,
} from './univer-doc-types';
import { ParagraphProps, listTypeForNum, paragraphPropsFromPPr, resolveTextStyle, textStyleFromRPr } from './docx-styles';

/**
 * Walks WordprocessingML block content (a document body, a header, a footer or a table cell)
 * and accumulates Univer's data stream with its paragraphs, runs, ranges, blocks and tables.
 */

export interface BodyContext {
  unitId: string;
  /** The part the content came from, for relationship lookups. */
  partPath: string;
  rels: Map<string, Relationship>;
  /** Image bytes by resolved part path, as data: URLs. */
  media: (relId: string) => string | null;
  styles: Record<string, DocNamedStyle>;
  /** Styles whose paragraph chain has numbering: style id to list binding. */
  styleNumbering: Map<string, { numId: string; level: number }>;
  knownLists: Set<string>;
  fidelity: FidelityCollector;
  where: string;
  nextId: (prefix: string) => string;
  /** Called for each w:sectPr met inside a paragraph (a section ending). */
  onSection?: (sectPr: Element, startIndex: number) => void;
}

export class BodyBuilder {
  readonly body: DocBody = {
    dataStream: '',
    textRuns: [],
    paragraphs: [],
    sectionBreaks: [],
    customBlocks: [],
    customRanges: [],
    tables: [],
    docxRawBlocks: [],
  };
  readonly tableSource: Record<string, DocTable> = {};
  readonly drawings: Record<string, DocDrawing> = {};
  readonly drawingsOrder: string[] = [];
  private pendingRun: { start: number; ts?: DocTextStyle; sId?: string } | null = null;

  constructor(private readonly ctx: BodyContext) {}

  get length(): number {
    return this.body.dataStream.length;
  }

  /** Appends every block child of `container` (w:body, w:hdr, w:ftr, w:tc, w:sdtContent). */
  blocks(container: Element): void {
    for (const el of elementChildren(container)) this.block(el);
  }

  block(el: Element): void {
    if (el.namespaceURI !== NS.w) {
      this.raw(el);
      return;
    }
    switch (el.localName) {
      case 'p':
        this.paragraph(el);
        return;
      case 'tbl':
        this.table(el);
        return;
      case 'sectPr':
        this.ctx.onSection?.(el, this.length);
        return;
      case 'sdt': {
        const content = child(el, NS.w, 'sdtContent');
        if (content) this.blocks(content);
        this.ctx.fidelity.warn('Content controls kept as plain content', this.ctx.where);
        return;
      }
      case 'bookmarkStart':
      case 'bookmarkEnd':
      case 'proofErr':
      case 'permStart':
      case 'permEnd':
      case 'commentRangeStart':
      case 'commentRangeEnd':
        return;
      case 'ins':
      case 'moveTo':
        this.blocks(el);
        this.ctx.fidelity.warn('Tracked changes accepted', this.ctx.where);
        return;
      case 'del':
      case 'moveFrom':
        this.ctx.fidelity.warn('Tracked changes accepted', this.ctx.where);
        return;
      case 'customXml':
        this.blocks(el);
        return;
      default:
        this.raw(el);
    }
  }

  /** Ends the body the way Univer expects: a final section break. */
  finish(section?: Partial<DocSectionBreak>): DocBody {
    this.flushRun();
    if (!this.body.dataStream.endsWith(TOKEN.PARAGRAPH) && !this.body.dataStream.endsWith(TOKEN.TABLE_END)) this.endParagraph({});
    this.body.sectionBreaks!.push({ sectionId: this.ctx.nextId('s'), ...section, startIndex: this.length });
    this.body.dataStream += TOKEN.SECTION_BREAK;
    return this.trimmed();
  }

  private trimmed(): DocBody {
    const b = this.body;
    const out: DocBody = { dataStream: b.dataStream, textRuns: b.textRuns, paragraphs: b.paragraphs, sectionBreaks: b.sectionBreaks };
    if (b.customBlocks?.length) out.customBlocks = b.customBlocks;
    if (b.customRanges?.length) out.customRanges = b.customRanges;
    if (b.tables?.length) out.tables = b.tables;
    if (b.docxRawBlocks?.length) out.docxRawBlocks = b.docxRawBlocks;
    return out;
  }

  // --- paragraphs --------------------------------------------------------------------------------

  private paragraph(p: Element): void {
    const props = paragraphPropsFromPPr(child(p, NS.w, 'pPr'));
    const baseTs = this.paragraphBaseTextStyle(props);
    this.inlineContent(p, baseTs);
    this.endParagraph(props);
    if (props.sectPr) this.ctx.onSection?.(props.sectPr, this.length);
  }

  /** The named style's resolved text formatting, baked in so the renderer shows it. */
  private paragraphBaseTextStyle(props: ParagraphProps): DocTextStyle | undefined {
    const resolved = resolveTextStyle(this.ctx.styles, props.styleId);
    return Object.keys(resolved).length ? resolved : undefined;
  }

  private endParagraph(props: ParagraphProps): void {
    this.flushRun();
    const paragraph: DocParagraph = { startIndex: this.length, paragraphId: this.ctx.nextId('p') };
    const style: DocParagraphStyle = { ...(props.style ?? {}) };
    if (props.styleId && this.ctx.styles[props.styleId]) {
      paragraph.styleId = props.styleId;
      const base = this.paragraphBaseTextStyle(props);
      if (base) style.textStyle = base;
    }
    if (props.markTextStyle) style.paragraphMarkTextStyle = props.markTextStyle;
    const numbering = props.numbering ?? (props.styleId ? this.ctx.styleNumbering.get(props.styleId) : undefined);
    if (numbering) {
      const listType = listTypeForNum(numbering.numId);
      if (this.ctx.knownLists.has(listType)) paragraph.bullet = { listType, listId: listType, nestingLevel: numbering.level };
      else this.ctx.fidelity.warn('List numbering without a definition', this.ctx.where);
    }
    if (Object.keys(style).length) paragraph.paragraphStyle = style;
    this.body.paragraphs!.push(paragraph);
    this.body.dataStream += TOKEN.PARAGRAPH;
  }

  // --- inline content ----------------------------------------------------------------------------

  private inlineContent(container: Element, baseTs: DocTextStyle | undefined): void {
    for (const el of elementChildren(container)) this.inline(el, baseTs);
  }

  private inline(el: Element, baseTs: DocTextStyle | undefined): void {
    if (el.namespaceURI !== NS.w) {
      if (el.namespaceURI === 'http://schemas.openxmlformats.org/officeDocument/2006/math')
        this.ctx.fidelity.drop('Equations', this.ctx.where);
      return;
    }
    switch (el.localName) {
      case 'pPr':
        return;
      case 'r':
        this.run(el, baseTs);
        return;
      case 'hyperlink':
        this.hyperlink(el, baseTs);
        return;
      case 'fldSimple':
        this.simpleField(el, baseTs);
        return;
      case 'sdt': {
        const content = child(el, NS.w, 'sdtContent');
        if (content) this.inlineContent(content, baseTs);
        this.ctx.fidelity.warn('Content controls kept as plain content', this.ctx.where);
        return;
      }
      case 'smartTag':
      case 'customXml':
      case 'ins':
      case 'moveTo':
      case 'dir':
      case 'bdo':
        this.inlineContent(el, baseTs);
        return;
      case 'del':
      case 'moveFrom':
        this.ctx.fidelity.warn('Tracked changes accepted', this.ctx.where);
        return;
      default:
        return;
    }
  }

  private run(r: Element, baseTs: DocTextStyle | undefined): void {
    const rPr = child(r, NS.w, 'rPr');
    const sId = wval(child(rPr, NS.w, 'rStyle')) ?? undefined;
    const charStyle = sId ? resolveTextStyle(this.ctx.styles, sId) : {};
    const direct = textStyleFromRPr(rPr);
    const ts = mergeTs(baseTs, charStyle, direct);
    for (const el of elementChildren(r)) {
      if (el.namespaceURI !== NS.w) continue;
      switch (el.localName) {
        case 't':
        case 'delText':
          this.text(el.textContent ?? '', ts, sId);
          break;
        case 'tab':
          this.text(TOKEN.TAB, ts, sId);
          break;
        case 'br': {
          const type = attr(el, NS.w, 'type');
          if (type === 'page') this.text(TOKEN.PAGE_BREAK, ts, sId);
          else if (type === 'column') this.text(TOKEN.COLUMN_BREAK, ts, sId);
          else this.lineBreak();
          break;
        }
        case 'cr':
          this.lineBreak();
          break;
        case 'sym': {
          const code = attr(el, NS.w, 'char');
          const n = code ? parseInt(code, 16) : NaN;
          if (!Number.isNaN(n)) this.text(String.fromCodePoint(n >= 0xf000 ? n - 0xf000 : n), ts, sId);
          break;
        }
        case 'noBreakHyphen':
          this.text('‑', ts, sId);
          break;
        case 'softHyphen':
          this.text('­', ts, sId);
          break;
        case 'drawing':
          this.drawing(el);
          break;
        case 'pict':
        case 'object':
          this.ctx.fidelity.drop('Legacy drawings and embedded objects', this.ctx.where);
          break;
        case 'footnoteReference':
        case 'endnoteReference':
          this.ctx.fidelity.drop('Footnotes and endnotes', this.ctx.where);
          break;
        case 'fldChar':
          this.fieldChar(attr(el, NS.w, 'fldCharType'));
          break;
        case 'instrText':
          if (this.field) this.field.instruction += el.textContent ?? '';
          break;
        default:
          break;
      }
    }
  }

  /** A soft line break has no Univer token, so the paragraph is split and its style carried on. */
  private lineBreak(): void {
    this.ctx.fidelity.warn('Line breaks became paragraphs', this.ctx.where);
    this.text(TOKEN.PARAGRAPH, undefined, undefined, true);
  }

  private text(text: string, ts: DocTextStyle | undefined, sId: string | undefined, paragraphToken = false): void {
    if (!text) return;
    if (paragraphToken) {
      this.flushRun();
      this.body.paragraphs!.push({ startIndex: this.length, paragraphId: this.ctx.nextId('p') });
      this.body.dataStream += text;
      return;
    }
    const style = ts && Object.keys(ts).length ? ts : undefined;
    if (!this.pendingRun || !sameTs(this.pendingRun.ts, style) || this.pendingRun.sId !== sId) {
      this.flushRun();
      if (style || sId) this.pendingRun = { start: this.length, ts: style, sId };
    }
    this.body.dataStream += text;
  }

  private flushRun(): void {
    const run = this.pendingRun;
    this.pendingRun = null;
    if (!run || run.start >= this.length) return;
    const out: DocTextRun = { st: run.start, ed: this.length };
    if (run.ts) out.ts = run.ts;
    if (run.sId) out.sId = run.sId;
    this.body.textRuns!.push(out);
  }

  private hyperlink(el: Element, baseTs: DocTextStyle | undefined): void {
    const relId = attr(el, NS.r, 'id');
    const anchor = attr(el, NS.w, 'anchor');
    const rel = relId ? this.ctx.rels.get(relId) : undefined;
    const start = this.length;
    this.inlineContent(el, baseTs);
    if (this.length === start) return;
    const properties: Record<string, unknown> = {};
    if (rel?.target) properties['url'] = anchor ? `${rel.target}#${anchor}` : rel.target;
    else if (anchor) properties['url'] = `#${anchor}`;
    const tooltip = attr(el, NS.w, 'tooltip');
    if (tooltip) properties['tooltip'] = tooltip;
    this.range({
      startIndex: start,
      endIndex: this.length - 1,
      rangeId: this.ctx.nextId('link'),
      rangeType: CUSTOM_RANGE.HYPERLINK,
      properties,
    });
  }

  private range(range: DocCustomRange): void {
    this.body.customRanges!.push(range);
  }

  // --- fields ------------------------------------------------------------------------------------

  private field: { instruction: string; start: number; inResult: boolean } | null = null;

  private simpleField(el: Element, baseTs: DocTextStyle | undefined): void {
    const instruction = attr(el, NS.w, 'instr')?.trim() ?? '';
    const start = this.length;
    this.inlineContent(el, baseTs);
    this.finishField(instruction, start);
  }

  private fieldChar(type: string | null): void {
    if (type === 'begin') {
      this.flushRun();
      this.field = { instruction: '', start: this.length, inResult: false };
    } else if (type === 'separate') {
      if (this.field) this.field.inResult = true;
    } else if (type === 'end' && this.field) {
      const { instruction, start } = this.field;
      this.field = null;
      this.finishField(instruction.trim(), start);
    }
  }

  /** The cached result stays as text; the instruction rides along as a field range. */
  private finishField(instruction: string, start: number): void {
    if (!instruction) return;
    const fieldType = instruction.split(/\s+/)[0]?.toUpperCase() ?? '';
    if (fieldType === 'HYPERLINK') {
      const m = /HYPERLINK\s+"([^"]+)"/.exec(instruction);
      if (m && this.length > start)
        this.range({
          startIndex: start,
          endIndex: this.length - 1,
          rangeId: this.ctx.nextId('link'),
          rangeType: CUSTOM_RANGE.HYPERLINK,
          properties: { url: m[1] },
        });
      return;
    }
    if (this.length === start) {
      // No cached result: show the field name so the reader sees something and the exporter can rebuild it.
      this.text(fieldType === 'PAGE' ? '1' : fieldType === 'NUMPAGES' ? '1' : '', undefined, undefined);
      if (this.length === start) return;
    }
    this.range({
      startIndex: start,
      endIndex: this.length - 1,
      rangeId: this.ctx.nextId('field'),
      rangeType: CUSTOM_RANGE.FIELD,
      properties: { instruction, fieldType, sourceKind: 'simple', cachedResult: this.body.dataStream.slice(start) },
    });
    if (fieldType !== 'PAGE' && fieldType !== 'NUMPAGES' && fieldType !== 'DATE' && fieldType !== 'TIME')
      this.ctx.fidelity.warn('Fields kept as their last result', this.ctx.where);
  }

  // --- drawings ----------------------------------------------------------------------------------

  private drawing(el: Element): void {
    const inline = child(el, NS.wp, 'inline');
    const anchor = child(el, NS.wp, 'anchor');
    const holder = inline ?? anchor;
    if (!holder) return;
    const blip = holder.getElementsByTagNameNS(NS.a, 'blip')[0];
    const relId = blip ? attr(blip, NS.r, 'embed') : null;
    const source = relId ? this.ctx.media(relId) : null;
    if (!source) {
      const kind = holder.getElementsByTagNameNS(NS.a, 'graphicData')[0]?.getAttribute('uri') ?? '';
      this.ctx.fidelity.drop(
        kind.includes('chart')
          ? 'Charts'
          : kind.includes('wordprocessingShape') || kind.includes('wordprocessingGroup')
            ? 'Shapes'
            : 'Linked or unsupported images',
        this.ctx.where,
      );
      return;
    }
    const extent = child(holder, NS.wp, 'extent');
    const width = emuToPx(num(extent?.getAttribute('cx')) ?? 914400);
    const height = emuToPx(num(extent?.getAttribute('cy')) ?? 914400);
    const docPr = child(holder, NS.wp, 'docPr');
    const drawingId = this.ctx.nextId('img');
    const drawing: DocDrawing = {
      unitId: this.ctx.unitId,
      subUnitId: this.ctx.unitId,
      drawingId,
      drawingType: 0,
      imageSourceType: 'BASE64',
      source,
      title: docPr?.getAttribute('name') ?? '',
      description: docPr?.getAttribute('descr') ?? '',
      docTransform: {
        size: { width, height },
        positionH: { relativeFrom: 1, posOffset: 0 },
        positionV: { relativeFrom: 1, posOffset: 0 },
        angle: 0,
      },
      transform: { left: 0, top: 0, width, height, angle: 0 },
      layoutType: LAYOUT.inline,
      behindDoc: 0,
      wrapText: 0,
      distT: 0,
      distB: 0,
      distL: 0,
      distR: 0,
    };
    if (anchor) {
      drawing.layoutType = anchorLayout(anchor);
      drawing.behindDoc = anchor.getAttribute('behindDoc') === '1' ? 1 : 0;
      drawing.distT = emuToPx(num(anchor.getAttribute('distT')) ?? 0);
      drawing.distB = emuToPx(num(anchor.getAttribute('distB')) ?? 0);
      drawing.distL = emuToPx(num(anchor.getAttribute('distL')) ?? 0);
      drawing.distR = emuToPx(num(anchor.getAttribute('distR')) ?? 0);
      drawing.docTransform.positionH = objectPosition(child(anchor, NS.wp, 'positionH'), H_RELATIVE, H_ALIGN);
      drawing.docTransform.positionV = objectPosition(child(anchor, NS.wp, 'positionV'), V_RELATIVE, V_ALIGN);
      const wrap =
        children(anchor, NS.wp, 'wrapSquare')[0] ?? children(anchor, NS.wp, 'wrapTight')[0] ?? children(anchor, NS.wp, 'wrapThrough')[0];
      const wrapText = wrap?.getAttribute('wrapText');
      drawing.wrapText = wrapText === 'left' ? 1 : wrapText === 'right' ? 2 : wrapText === 'largest' ? 3 : 0;
      drawing.transform = {
        left: drawing.docTransform.positionH.posOffset ?? 0,
        top: drawing.docTransform.positionV.posOffset ?? 0,
        width,
        height,
        angle: 0,
      };
    }
    this.flushRun();
    const block: DocCustomBlock = { startIndex: this.length, blockId: drawingId, blockType: 0 };
    this.body.customBlocks!.push(block);
    this.body.dataStream += TOKEN.CUSTOM_BLOCK;
    this.drawings[drawingId] = drawing;
    this.drawingsOrder.push(drawingId);
  }

  // --- tables ------------------------------------------------------------------------------------

  private table(tbl: Element): void {
    this.flushRun();
    const tblPr = child(tbl, NS.w, 'tblPr');
    const grid = children(child(tbl, NS.w, 'tblGrid'), NS.w, 'gridCol').map(c => twipsToPx(num(attr(c, NS.w, 'w')) ?? 1440));
    const rowsEl = children(tbl, NS.w, 'tr');
    const columnCount = Math.max(grid.length, ...rowsEl.map(tr => gridWidthOfRow(tr)), 1);
    while (grid.length < columnCount) grid.push(96);
    const tableId = this.ctx.nextId('tbl');
    const tableBorders = child(tblPr, NS.w, 'tblBorders');
    const defaults = {
      top: cellBorder(child(tableBorders, NS.w, 'top')),
      bottom: cellBorder(child(tableBorders, NS.w, 'bottom')),
      left: cellBorder(child(tableBorders, NS.w, 'left')),
      right: cellBorder(child(tableBorders, NS.w, 'right')),
      insideH: cellBorder(child(tableBorders, NS.w, 'insideH')),
      insideV: cellBorder(child(tableBorders, NS.w, 'insideV')),
    };
    const tblW = child(tblPr, NS.w, 'tblW');
    const widthType = attr(tblW, NS.w, 'type');
    const widthValue = num(attr(tblW, NS.w, 'w'));
    const gridTotal = grid.reduce((a, b) => a + b, 0);
    const jc = wval(child(tblPr, NS.w, 'jc'));
    const cellMar = child(tblPr, NS.w, 'tblCellMar');
    const marginOf = (name: string, fallback: number) => ({ v: twipsToPx(num(attr(child(cellMar, NS.w, name), NS.w, 'w')) ?? fallback) });
    const table: DocTable = {
      tableId,
      tableRows: [],
      tableColumns: grid.map(w => ({ size: { type: 1, width: { v: w } } })),
      align: jc === 'center' ? 1 : jc === 'right' || jc === 'end' ? 2 : 0,
      indent: { v: twipsToPx(num(attr(child(tblPr, NS.w, 'tblInd'), NS.w, 'w')) ?? 0) },
      textWrap: 0,
      position: { positionH: { relativeFrom: 0, posOffset: 0 }, positionV: { relativeFrom: 0, posOffset: 0 } },
      dist: { distT: 0, distB: 0, distL: 0, distR: 0 },
      size:
        widthType === 'dxa' && widthValue
          ? { type: 1, width: { v: twipsToPx(widthValue) } }
          : widthType === 'pct' && widthValue
            ? { type: 1, width: { v: widthValue / 50, u: 4 } }
            : { type: 0, width: { v: gridTotal } },
      cellMargin: { start: marginOf('left', 108), end: marginOf('right', 108), top: marginOf('top', 0), bottom: marginOf('bottom', 0) },
      layout: wval(child(tblPr, NS.w, 'tblLayout')) === 'fixed' || attr(child(tblPr, NS.w, 'tblLayout'), NS.w, 'type') === 'fixed' ? 1 : 0,
    };
    const caption = wval(child(tblPr, NS.w, 'tblCaption')) ?? wval(child(tblPr, NS.w, 'tblDescription'));
    if (caption) table.description = caption;
    const startIndex = this.length;
    this.body.dataStream += TOKEN.TABLE_START;
    // Vertical merges: a cell with vMerge="restart" spans the following rows whose cell at the same
    // grid column says vMerge (continue); those are emitted as covered cells.
    const pendingVMerge = new Map<number, DocTableCell>();
    rowsEl.forEach((tr, rowIndex) => {
      const trPr = child(tr, NS.w, 'trPr');
      const trHeight = child(trPr, NS.w, 'trHeight');
      const hRule = attr(trHeight, NS.w, 'hRule');
      const row: DocTableRow = {
        tableCells: [],
        trHeight:
          trHeight && num(attr(trHeight, NS.w, 'val')) !== undefined
            ? { val: { v: twipsToPx(num(attr(trHeight, NS.w, 'val'))!) }, hRule: hRule === 'exact' ? 2 : 1 }
            : { val: { v: 30 }, hRule: 0 },
      };
      if (toggle(child(trPr, NS.w, 'tblHeader'))) row.repeatHeaderRow = 1;
      if (toggle(child(trPr, NS.w, 'cantSplit'))) row.cantSplit = 1;
      this.body.dataStream += TOKEN.TABLE_ROW_START;
      let col = 0;
      const gridBefore = num(wval(child(trPr, NS.w, 'gridBefore'))) ?? 0;
      for (let i = 0; i < gridBefore && col < columnCount; i++, col++) this.coveredCell(row);
      for (const tc of children(tr, NS.w, 'tc')) {
        if (col >= columnCount) break;
        const tcPr = child(tc, NS.w, 'tcPr');
        const span = Math.max(1, num(wval(child(tcPr, NS.w, 'gridSpan'))) ?? 1);
        const vMerge = child(tcPr, NS.w, 'vMerge');
        const vMergeVal = vMerge ? (wval(vMerge) ?? 'continue') : null;
        if (vMergeVal === 'continue') {
          const owner = pendingVMerge.get(col);
          if (owner) owner.rowSpan = (owner.rowSpan ?? 1) + 1;
          for (let i = 0; i < span && col < columnCount; i++, col++) this.coveredCell(row);
          continue;
        }
        const isLastRow = rowIndex === rowsEl.length - 1;
        const isFirstRow = rowIndex === 0;
        const isFirstCol = col === 0;
        const isLastCol = col + span >= columnCount;
        const cell = this.cell(
          tc,
          tcPr,
          span,
          {
            top: isFirstRow ? defaults.top : defaults.insideH,
            bottom: isLastRow ? defaults.bottom : defaults.insideH,
            left: isFirstCol ? defaults.left : defaults.insideV,
            right: isLastCol ? defaults.right : defaults.insideV,
          },
          table.cellMargin!,
        );
        if (vMergeVal === 'restart') pendingVMerge.set(col, cell);
        else pendingVMerge.delete(col);
        row.tableCells.push(cell);
        for (let i = 1; i < span && col + i < columnCount; i++) this.coveredCell(row);
        col += span;
      }
      while (col < columnCount) {
        this.coveredCell(row);
        col++;
      }
      this.body.dataStream += TOKEN.TABLE_ROW_END;
      table.tableRows.push(row);
    });
    this.body.dataStream += TOKEN.TABLE_END;
    this.tableSource[tableId] = table;
    this.body.tables!.push({ startIndex, endIndex: this.length, tableId });
  }

  private coveredCell(row: DocTableRow): void {
    row.tableCells.push({ ...coveredCellOf(undefined), rowSpan: 0, columnSpan: 0 });
    this.emptyCellStream();
  }

  private emptyCellStream(): void {
    this.body.dataStream += TOKEN.TABLE_CELL_START;
    this.body.paragraphs!.push({ startIndex: this.length, paragraphId: this.ctx.nextId('p') });
    this.body.dataStream += TOKEN.PARAGRAPH;
    this.body.sectionBreaks!.push({ sectionId: this.ctx.nextId('s'), startIndex: this.length });
    this.body.dataStream += TOKEN.SECTION_BREAK + TOKEN.TABLE_CELL_END;
  }

  private cell(
    tc: Element,
    tcPr: Element | null,
    span: number,
    borders: Record<'top' | 'bottom' | 'left' | 'right', DocTableCellBorder | undefined>,
    margin: NonNullable<DocTable['cellMargin']>,
  ): DocTableCell {
    const cell: DocTableCell = { margin: { ...margin } };
    if (span > 1) cell.columnSpan = span;
    const tcW = child(tcPr, NS.w, 'tcW');
    const w = num(attr(tcW, NS.w, 'w'));
    if (attr(tcW, NS.w, 'type') === 'dxa' && w) cell.size = { type: 1, width: { v: twipsToPx(w) } };
    const shd = hexColor(attr(child(tcPr, NS.w, 'shd'), NS.w, 'fill'));
    if (shd) cell.backgroundColor = { rgb: shd };
    const tcBorders = child(tcPr, NS.w, 'tcBorders');
    const own = (name: 'top' | 'bottom' | 'left' | 'right') =>
      tcBorders && child(tcBorders, NS.w, name) ? cellBorder(child(tcBorders, NS.w, name)) : borders[name];
    const bt = own('top'),
      bb = own('bottom'),
      bl = own('left'),
      br = own('right');
    if (bt) cell.borderTop = bt;
    if (bb) cell.borderBottom = bb;
    if (bl) cell.borderLeft = bl;
    if (br) cell.borderRight = br;
    const tcMar = child(tcPr, NS.w, 'tcMar');
    if (tcMar) {
      const m = (name: string, fallback: { v: number }) => {
        const v = num(attr(child(tcMar, NS.w, name), NS.w, 'w'));
        return v === undefined ? fallback : { v: twipsToPx(v) };
      };
      cell.margin = {
        start: m('left', margin.start),
        end: m('right', margin.end),
        top: m('top', margin.top),
        bottom: m('bottom', margin.bottom),
      };
    }
    const vAlign = wval(child(tcPr, NS.w, 'vAlign'));
    if (vAlign === 'center') cell.vAlign = 3;
    else if (vAlign === 'bottom') cell.vAlign = 4;
    else if (vAlign === 'top') cell.vAlign = 2;
    this.body.dataStream += TOKEN.TABLE_CELL_START;
    const start = this.length;
    for (const el of elementChildren(tc)) {
      if (el.namespaceURI === NS.w && el.localName === 'tcPr') continue;
      this.block(el);
    }
    this.flushRun();
    if (this.length === start || !this.body.dataStream.endsWith(TOKEN.PARAGRAPH)) this.endParagraph({});
    this.body.sectionBreaks!.push({ sectionId: this.ctx.nextId('s'), startIndex: this.length });
    this.body.dataStream += TOKEN.SECTION_BREAK + TOKEN.TABLE_CELL_END;
    return cell;
  }

  // --- raw preservation --------------------------------------------------------------------------

  /** Block content with no Univer counterpart is kept verbatim and written back on export. */
  private raw(el: Element): void {
    this.flushRun();
    const name = el.namespaceURI === NS.w ? el.localName : el.tagName;
    this.ctx.fidelity.warn(`Unsupported "${name}" content kept but not shown`, this.ctx.where);
    this.body.docxRawBlocks!.push({ startIndex: this.length, xml: serializeXml(el) });
  }
}

export function mergeTs(...layers: (DocTextStyle | undefined)[]): DocTextStyle | undefined {
  const out: DocTextStyle = {};
  for (const layer of layers) if (layer) Object.assign(out, layer);
  for (const k of Object.keys(out) as (keyof DocTextStyle)[]) if (out[k] === undefined) delete out[k];
  return Object.keys(out).length ? out : undefined;
}

function sameTs(a: DocTextStyle | undefined, b: DocTextStyle | undefined): boolean {
  if (!a && !b) return true;
  if (!a || !b) return false;
  return JSON.stringify(a) === JSON.stringify(b);
}

function gridWidthOfRow(tr: Element): number {
  const trPr = child(tr, NS.w, 'trPr');
  let n = (num(wval(child(trPr, NS.w, 'gridBefore'))) ?? 0) + (num(wval(child(trPr, NS.w, 'gridAfter'))) ?? 0);
  for (const tc of children(tr, NS.w, 'tc')) n += Math.max(1, num(wval(child(child(tc, NS.w, 'tcPr'), NS.w, 'gridSpan'))) ?? 1);
  return n;
}

function coveredCellOf(margin: DocTable['cellMargin'] | undefined): DocTableCell {
  return { margin: margin ? { ...margin } : { start: { v: 7.2 }, end: { v: 7.2 }, top: { v: 0 }, bottom: { v: 0 } } };
}

const BORDER_DASH: Record<string, number> = { dotted: 2, dashed: 3, dashSmallGap: 3, dotDash: 3, dotDotDash: 3 };

export function cellBorder(el: Element | null): DocTableCellBorder | undefined {
  if (!el) return undefined;
  const val = wval(el);
  if (!val || val === 'nil' || val === 'none') return undefined;
  return {
    color: { rgb: hexColor(attr(el, NS.w, 'color')) ?? '#000000' },
    width: { v: eighthsToPx(num(attr(el, NS.w, 'sz')) ?? 4) },
    dashStyle: BORDER_DASH[val] ?? 1,
  };
}

const H_RELATIVE: Record<string, number> = {
  page: 0,
  column: 1,
  character: 2,
  margin: 3,
  insideMargin: 4,
  outsideMargin: 5,
  leftMargin: 6,
  rightMargin: 7,
};
const V_RELATIVE: Record<string, number> = {
  page: 0,
  paragraph: 1,
  line: 2,
  margin: 3,
  topMargin: 4,
  bottomMargin: 5,
  insideMargin: 6,
  outsideMargin: 7,
};
const H_ALIGN: Record<string, number> = { center: 0, inside: 1, left: 2, outside: 3, right: 4 };
const V_ALIGN: Record<string, number> = { bottom: 0, center: 1, inside: 2, outside: 3, top: 4 };

function objectPosition(
  el: Element | null,
  relatives: Record<string, number>,
  aligns: Record<string, number>,
): { relativeFrom: number; posOffset?: number; align?: number } {
  const relativeFrom = relatives[el?.getAttribute('relativeFrom') ?? ''] ?? 1;
  const offset = child(el, NS.wp, 'posOffset');
  const align = child(el, NS.wp, 'align');
  if (align?.textContent && aligns[align.textContent] !== undefined) return { relativeFrom, align: aligns[align.textContent] };
  return { relativeFrom, posOffset: emuToPx(num(offset?.textContent) ?? 0) };
}

function anchorLayout(anchor: Element): number {
  if (child(anchor, NS.wp, 'wrapNone')) return LAYOUT.none;
  if (child(anchor, NS.wp, 'wrapSquare')) return LAYOUT.square;
  if (child(anchor, NS.wp, 'wrapTight')) return LAYOUT.tight;
  if (child(anchor, NS.wp, 'wrapThrough')) return LAYOUT.through;
  if (child(anchor, NS.wp, 'wrapTopAndBottom')) return LAYOUT.topAndBottom;
  return LAYOUT.square;
}

export { H_RELATIVE, V_RELATIVE, H_ALIGN, V_ALIGN };
