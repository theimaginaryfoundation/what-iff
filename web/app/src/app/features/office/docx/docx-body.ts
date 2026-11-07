import { linkProperties } from '../shared/safe-link';
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
  NOTE_REFERENCE,
  TOKEN,
} from './univer-doc-types';
import {
  BOUND_CONTROLS_NOTICE,
  CONTROLS_NOTICE,
  REVISIONS_NOTICE,
  fragment,
  isBound,
  markExtras,
  notePlaceholder,
  openTagOf,
  sdtProperties,
} from './docx-revisions';
import {
  ParagraphProps,
  listTypeForNum,
  paragraphPropsFromPPr,
  resolveTextStyle,
  stripRootNamespaces,
  textStyleFromRPr,
} from './docx-styles';

/**
 * Walks WordprocessingML block content (a document body, a header, a footer or a table cell)
 * and accumulates Univer's data stream with its paragraphs, runs, ranges, blocks and tables.
 */

export interface BodyContext {
  unitId: string;
  /** Header and footer drawings are repeated on every page; Univer keeps one transform per page. */
  headerFooter?: boolean;
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
  /** Resolves a footnote or endnote reference (w:id) to the note id its story is kept under; null drops it. */
  noteFor?: (kind: 'footnote' | 'endnote', wordId: string) => string | null;
  /** Whether the package has a comments part; without one, comment marks are dropped. */
  hasComments?: boolean;
}

/** The Office Math namespace. */
const NS_M = 'http://schemas.openxmlformats.org/officeDocument/2006/math';

const MODELLED_PPR = new Set([
  'pStyle',
  'jc',
  'spacing',
  'ind',
  'keepLines',
  'keepNext',
  'pageBreakBefore',
  'widowControl',
  'contextualSpacing',
  'outlineLvl',
  'shd',
  'pBdr',
  'bidi',
  'numPr',
  'rPr',
  'sectPr',
]);
const MODELLED_TCPR = new Set(['tcW', 'gridSpan', 'hMerge', 'vMerge', 'tcBorders', 'shd', 'tcMar', 'vAlign']);
const MODELLED_TRPR = new Set(['trHeight', 'cantSplit', 'tblHeader', 'gridBefore', 'gridAfter']);

/** The children of a properties element the model does not carry, serialised so they can go back verbatim. */
export function extrasXml(el: Element | null | undefined, modelled: Set<string>): string | undefined {
  if (!el) return undefined;
  const out = elementChildren(el)
    .filter(c => c.namespaceURI !== NS.w || !modelled.has(c.localName))
    .map(c => stripRootNamespaces(serializeXml(c)));
  return out.length ? out.join('') : undefined;
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
    docxRawCustomBlocks: [],
  };
  readonly tableSource: Record<string, DocTable> = {};
  readonly drawings: Record<string, DocDrawing> = {};
  readonly drawingsOrder: string[] = [];
  private pendingRun: { start: number; ts?: DocTextStyle; sId?: string } | null = null;
  private readonly openBookmarks = new Map<string, { name: string; start: number }>();
  private readonly openComments = new Map<string, number>();
  private readonly closedComments = new Set<string>();

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
      case 'sdt':
        this.blockSdt(el);
        return;
      case 'bookmarkStart':
      case 'bookmarkEnd':
      case 'commentRangeStart':
      case 'commentRangeEnd':
        this.rangeMark(el);
        return;
      case 'proofErr':
      case 'permStart':
      case 'permEnd':
        return;
      case 'ins':
      case 'moveTo': {
        const start = this.length;
        this.blocks(el);
        this.revisionRange(el, start);
        return;
      }
      case 'del':
      case 'moveFrom':
      case 'moveFromRangeStart':
      case 'moveFromRangeEnd':
      case 'moveToRangeStart':
      case 'moveToRangeEnd':
        this.ctx.fidelity.warn(REVISIONS_NOTICE, this.ctx.where);
        this.body.docxRawBlocks!.push({ startIndex: this.length, xml: fragment(el) });
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
    if (b.docxRawCustomBlocks?.length) out.docxRawCustomBlocks = b.docxRawCustomBlocks;
    // Outermost first at each position, the order Univer writes them in.
    if (b.customRanges?.length) out.customRanges = b.customRanges.slice().sort((x, y) => x.startIndex - y.startIndex || y.endIndex - x.endIndex);
    if (b.tables?.length) out.tables = b.tables;
    if (b.docxRawBlocks?.length) out.docxRawBlocks = b.docxRawBlocks;
    return out;
  }

  // --- paragraphs --------------------------------------------------------------------------------

  private paragraph(p: Element): void {
    const pPr = child(p, NS.w, 'pPr');
    const props = paragraphPropsFromPPr(pPr);
    const baseTs = this.paragraphBaseTextStyle(props);
    this.inlineContent(p, baseTs);
    this.endParagraph(props, extrasXml(pPr, MODELLED_PPR), markExtras(pPr));
    if (props.sectPr) this.ctx.onSection?.(props.sectPr, this.length);
  }

  /** The named style's resolved text formatting, baked in so the renderer shows it. */
  private paragraphBaseTextStyle(props: ParagraphProps): DocTextStyle | undefined {
    const resolved = resolveTextStyle(this.ctx.styles, props.styleId);
    return Object.keys(resolved).length ? resolved : undefined;
  }

  private endParagraph(props: ParagraphProps, extras?: string, mark?: { first?: string; last?: string }): void {
    this.flushRun();
    const paragraph: DocParagraph = { startIndex: this.length, paragraphId: this.ctx.nextId('p') };
    if (extras) paragraph.docxPPr = extras;
    if (mark?.first || mark?.last) {
      paragraph.docxMarkRPr = mark;
      this.ctx.fidelity.warn(REVISIONS_NOTICE, this.ctx.where);
    }
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
      if (el.namespaceURI === NS_M) this.rawInline(serializeXml(el), 'equation');
      else if (el.namespaceURI === NS.mc && el.localName === 'AlternateContent') {
        if (hasPicture(el)) this.inlineContent(child(el, NS.mc, 'Choice') ?? child(el, NS.mc, 'Fallback') ?? el, baseTs);
        else this.rawInline(serializeXml(el), 'shape');
      }
      return;
    }
    switch (el.localName) {
      case 'pPr':
        return;
      case 'bookmarkStart':
      case 'bookmarkEnd':
      case 'commentRangeStart':
      case 'commentRangeEnd':
        this.rangeMark(el);
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
      case 'sdt':
        this.inlineSdt(el, baseTs);
        return;
      case 'smartTag':
      case 'customXml':
      case 'dir':
      case 'bdo':
        this.inlineContent(el, baseTs);
        return;
      case 'ins':
      case 'moveTo': {
        const start = this.length;
        this.inlineContent(el, baseTs);
        this.revisionRange(el, start);
        return;
      }
      case 'del':
      case 'moveFrom':
      case 'moveFromRangeStart':
      case 'moveFromRangeEnd':
      case 'moveToRangeStart':
      case 'moveToRangeEnd':
        this.ctx.fidelity.warn(REVISIONS_NOTICE, this.ctx.where);
        this.rawRun(fragment(el));
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
    // An opaque child (a shape, an object, an equation) is kept as a run of its own, with this run's formatting.
    const runStart = this.length;
    const rawRun = (el: Element, what: string) => this.rawInline(`<w:r>${rPr ? serializeXml(rPr) : ''}${serializeXml(el)}</w:r>`, what);
    for (const el of elementChildren(r)) {
      if (el.namespaceURI === NS.mc && el.localName === 'AlternateContent') {
        if (hasPicture(el))
          for (const c of elementChildren(child(el, NS.mc, 'Choice') ?? child(el, NS.mc, 'Fallback')!)) this.runChild(c, ts, sId, rawRun);
        else rawRun(el, 'shape');
        continue;
      }
      if (el.namespaceURI === NS_M) {
        rawRun(el, 'equation');
        continue;
      }
      if (el.namespaceURI !== NS.w) continue;
      this.runChild(el, ts, sId, rawRun);
    }
    const change = child(rPr, NS.w, 'rPrChange');
    if (change) {
      this.flushRun();
      this.ctx.fidelity.warn(REVISIONS_NOTICE, this.ctx.where);
      if (this.length > runStart) {
        this.range({
          startIndex: runStart,
          endIndex: this.length - 1,
          rangeId: this.ctx.nextId('rev'),
          rangeType: CUSTOM_RANGE.CUSTOM,
          active: false,
          properties: { docxRPrChange: fragment(change), textColorMode: 'text' },
        });
      }
    }
  }

  private runChild(el: Element, ts: DocTextStyle | undefined, sId: string | undefined, rawRun: (el: Element, what: string) => void): void {
    switch (el.localName) {
      case 't':
      case 'delText':
        this.text(el.textContent ?? '', ts, sId);
        break;
      case 'tab':
      case 'ptab':
        this.text(TOKEN.TAB, ts, sId);
        break;
      case 'br':
      case 'cr':
        this.lineOrColumnBreak(el, ts, sId);
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
        if (!this.drawing(el)) rawRun(el, drawingKind(el));
        break;
      case 'pict':
      case 'object':
        rawRun(el, el.localName === 'object' ? 'embedded object' : 'legacy drawing');
        break;
      case 'ruby':
        rawRun(el, 'phonetic guide');
        break;
      case 'footnoteReference':
      case 'endnoteReference':
        this.noteReference(el.localName === 'footnoteReference' ? 'footnote' : 'endnote', attr(el, NS.w, 'id'), ts, sId);
        break;
      case 'commentReference':
        this.commentReference(attr(el, NS.w, 'id'));
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

  /**
   * In a traditional document Univer lays out a bare `\\v` as a soft line break, and only one marked
   * `breakType: column` as a column break (engine-render createWhitespaceGlyph, _isMarkedColumnBreak).
   */
  private lineOrColumnBreak(el: Element, ts: DocTextStyle | undefined, sId: string | undefined): void {
    const type = el.localName === 'br' ? attr(el, NS.w, 'type') : null;
    if (type === 'page') {
      this.text(TOKEN.PAGE_BREAK, ts, sId);
      return;
    }
    this.text(TOKEN.COLUMN_BREAK, ts, sId);
    const at = this.length - 1;
    if (type === 'column') this.breakRange(at, { breakType: 'column' });
    // w:cr and a clearing w:br draw the same line break, and go back as what they were.
    else if (el.localName === 'cr' || attr(el, NS.w, 'clear')) this.breakRange(at, { docxBreakXml: fragment(el), textColorMode: 'text' });
  }

  private breakRange(at: number, properties: Record<string, unknown>): void {
    this.range({ startIndex: at, endIndex: at, rangeId: this.ctx.nextId('br'), rangeType: CUSTOM_RANGE.CUSTOM, wholeEntity: true, active: false, properties });
  }

  private text(text: string, ts: DocTextStyle | undefined, sId: string | undefined): void {
    if (!text) return;
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
    const target = rel?.target ? (anchor ? `${rel.target}#${anchor}` : rel.target) : anchor ? `#${anchor}` : '';
    if (!target) return;
    Object.assign(properties, linkProperties(target));
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
          properties: linkProperties(m[1]),
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
      this.ctx.fidelity.drop('Fields (kept as their last result)', this.ctx.where);
  }

  // --- drawings ----------------------------------------------------------------------------------

  /** Reads a picture into a drawing; false when the drawing is something else (kept raw by the caller). */
  private drawing(el: Element): boolean {
    const inline = child(el, NS.wp, 'inline');
    const anchor = child(el, NS.wp, 'anchor');
    const holder = inline ?? anchor;
    if (!holder) return true;
    const blip = holder.getElementsByTagNameNS(NS.a, 'blip')[0];
    const relId = blip ? attr(blip, NS.r, 'embed') : null;
    const source = relId ? this.ctx.media(relId) : null;
    if (!source) return false;
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
    if (this.ctx.headerFooter) {
      drawing.isMultiTransform = 1;
      drawing.transforms = drawing.transform ? [drawing.transform] : [];
    }
    this.flushRun();
    const block: DocCustomBlock = { startIndex: this.length, blockId: drawingId, blockType: 0 };
    this.body.customBlocks!.push(block);
    this.body.dataStream += TOKEN.CUSTOM_BLOCK;
    this.drawings[drawingId] = drawing;
    this.drawingsOrder.push(drawingId);
    return true;
  }

  // --- notes, bookmarks and comments ------------------------------------------------------------

  /** A note reference is one object character; Univer draws the note's label over it and lays the story out. */
  private noteReference(kind: 'footnote' | 'endnote', wordId: string | null, ts: DocTextStyle | undefined, sId: string | undefined): void {
    const noteId = wordId ? (this.ctx.noteFor?.(kind, wordId) ?? null) : null;
    if (!noteId) {
      this.ctx.fidelity.drop('Footnotes and endnotes', this.ctx.where);
      return;
    }
    const start = this.length;
    // The reference keeps the run's formatting minus the superscript, which Univer adds itself.
    const refTs = ts ? { ...ts } : undefined;
    if (refTs) delete refTs.va;
    this.text(NOTE_REFERENCE, refTs && Object.keys(refTs).length ? refTs : undefined, sId);
    this.range({
      startIndex: start,
      endIndex: start,
      rangeId: this.ctx.nextId('note'),
      rangeType: kind === 'footnote' ? CUSTOM_RANGE.FOOTNOTE : CUSTOM_RANGE.ENDNOTE,
      properties: { noteId },
    });
  }

  /** Bookmark and comment boundaries become ranges over the text between them. */
  private rangeMark(el: Element): void {
    const id = attr(el, NS.w, 'id') ?? '';
    switch (el.localName) {
      case 'bookmarkStart': {
        const name = attr(el, NS.w, 'name') ?? '';
        if (name && name !== '_GoBack') this.openBookmarks.set(id, { name, start: this.length });
        return;
      }
      case 'bookmarkEnd': {
        const open = this.openBookmarks.get(id);
        if (!open) return;
        this.openBookmarks.delete(id);
        this.range({
          startIndex: open.start,
          endIndex: Math.max(open.start, this.length - 1),
          rangeId: this.ctx.nextId('bm'),
          rangeType: CUSTOM_RANGE.BOOKMARK,
          properties: this.length > open.start ? { name: open.name } : { name: open.name, collapsed: true },
        });
        return;
      }
      case 'commentRangeStart':
        if (this.ctx.hasComments) this.openComments.set(id, this.length);
        return;
      case 'commentRangeEnd': {
        const start = this.openComments.get(id);
        if (start === undefined) return;
        this.openComments.delete(id);
        this.closedComments.add(id);
        this.range({
          startIndex: start,
          endIndex: Math.max(start, this.length - 1),
          rangeId: this.ctx.nextId('cm'),
          rangeType: CUSTOM_RANGE.COMMENT,
          properties: this.length > start ? { commentId: id } : { commentId: id, collapsed: true },
        });
        return;
      }
    }
  }

  /** A comment with no range is anchored to the character before its reference. */
  private commentReference(id: string | null): void {
    if (!id || !this.ctx.hasComments) {
      if (!this.ctx.hasComments) this.ctx.fidelity.drop('Comments', this.ctx.where);
      return;
    }
    if (this.closedComments.has(id) || this.openComments.has(id)) return;
    const at = Math.max(0, this.length - 1);
    this.range({
      startIndex: at,
      endIndex: at,
      rangeId: this.ctx.nextId('cm'),
      rangeType: CUSTOM_RANGE.COMMENT,
      properties: { commentId: id, collapsed: true },
    });
  }

  // --- opaque inline content --------------------------------------------------------------------

  /** Run-level content with no Univer counterpart rides on a custom-block sentinel and is written back verbatim. */
  private rawInline(xml: string, what: string): void {
    this.ctx.fidelity.warn(`Kept but not shown: ${what}`, this.ctx.where);
    this.rawRun(xml);
  }

  /** An opaque piece of run content on a sentinel; it moves with edits and Univer draws nothing for it. */
  private rawRun(xml: string): void {
    this.flushRun();
    this.body.docxRawCustomBlocks!.push({
      startIndex: this.length,
      blockId: this.ctx.nextId('raw'),
      blockType: 1,
      docxRawXml: stripRootNamespaces(xml),
    });
    this.body.dataStream += TOKEN.CUSTOM_BLOCK;
  }

  // --- content controls and tracked insertions ----------------------------------------------------

  private inlineSdt(sdt: Element, baseTs: DocTextStyle | undefined): void {
    const start = this.length;
    const content = child(sdt, NS.w, 'sdtContent');
    if (content) this.inlineContent(content, baseTs);
    this.flushRun();
    // A control with nothing in it has nothing for a range to hold on to.
    if (this.length === start) this.rawRun(fragment(sdt));
    else this.sdtRange(sdt, 'inline', start, this.length - 1);
  }

  private blockSdt(sdt: Element): void {
    this.flushRun();
    const start = this.length;
    const content = child(sdt, NS.w, 'sdtContent');
    if (content) this.blocks(content);
    this.flushRun();
    if (this.length === start) this.body.docxRawBlocks!.push({ startIndex: start, xml: fragment(sdt) });
    else this.sdtRange(sdt, 'block', start, this.length - 1);
  }

  private sdtRange(sdt: Element, placement: 'inline' | 'block' | 'row' | 'cell', startIndex: number, endIndex: number): void {
    this.ctx.fidelity.warn(CONTROLS_NOTICE, this.ctx.where);
    if (isBound(sdt)) this.ctx.fidelity.warn(BOUND_CONTROLS_NOTICE, this.ctx.where);
    const range: DocCustomRange = {
      startIndex,
      endIndex,
      rangeId: this.ctx.nextId('sdt'),
      rangeType: CUSTOM_RANGE.SDT,
      properties: sdtProperties(sdt, placement),
    };
    notePlaceholder(this.body.dataStream, range);
    this.range(range);
  }

  /** A tracked insertion: its text stays visible, and Univer marks a CUSTOM range coloured and underlined. */
  private revisionRange(el: Element, start: number): void {
    this.flushRun();
    this.ctx.fidelity.warn(REVISIONS_NOTICE, this.ctx.where);
    if (this.length === start) return;
    this.range({
      startIndex: start,
      endIndex: this.length - 1,
      rangeId: this.ctx.nextId('rev'),
      rangeType: CUSTOM_RANGE.CUSTOM,
      properties: { docxRevisionOpen: openTagOf(el), docxRevisionClose: `</w:${el.localName}>` },
    });
  }

  /** Controls wrapping rows or cells, once every row or cell inside them has its place in the stream. */
  private trackedSdts(): { note(el: Element, boundary: Element, start: number, end: number): void; finish(placement: 'row' | 'cell'): void } {
    const spans = new Map<Element, { start: number; end: number }>();
    return {
      note: (el, boundary, start, end) => {
        for (const sdt of enclosingSdts(el, boundary)) {
          const span = spans.get(sdt);
          if (span) span.end = end;
          else spans.set(sdt, { start, end });
        }
      },
      finish: placement => spans.forEach((span, sdt) => this.sdtRange(sdt, placement, span.start, span.end)),
    };
  }

  // --- tables ------------------------------------------------------------------------------------

  private table(tbl: Element): void {
    this.flushRun();
    const tblPr = child(tbl, NS.w, 'tblPr');
    const grid = children(child(tbl, NS.w, 'tblGrid'), NS.w, 'gridCol').map(c => twipsToPx(num(attr(c, NS.w, 'w')) ?? 1440));
    const rowsEl = tableRows(tbl);
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
    if (tblPr) table.docxTblPr = stripRootNamespaces(serializeXml(tblPr));
    const startIndex = this.length;
    this.body.dataStream += TOKEN.TABLE_START;
    // Vertical merges: a cell with vMerge="restart" spans the following rows whose cell at the same
    // grid column says vMerge (continue); those are emitted as covered cells.
    const pendingVMerge = new Map<number, DocTableCell>();
    const rowSdts = this.trackedSdts();
    rowsEl.forEach((tr, rowIndex) => {
      const rowStart = this.length;
      const cellSdts = this.trackedSdts();
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
      const trExtras = extrasXml(trPr, MODELLED_TRPR);
      if (trExtras) row.docxTrPr = trExtras;
      this.body.dataStream += TOKEN.TABLE_ROW_START;
      let col = 0;
      const gridBefore = num(wval(child(trPr, NS.w, 'gridBefore'))) ?? 0;
      for (let i = 0; i < gridBefore && col < columnCount; i++, col++) this.coveredCell(row);
      for (const tc of tableCells(tr)) {
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
        const cellStart = this.length;
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
        cellSdts.note(tc, tr, cellStart, this.length - 1);
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
      cellSdts.finish('cell');
      rowSdts.note(tr, tbl, rowStart, this.length - 1);
      table.tableRows.push(row);
    });
    rowSdts.finish('row');
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
    const tcExtras = extrasXml(tcPr, MODELLED_TCPR);
    if (tcExtras) cell.docxTcPr = tcExtras;
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
    this.body.docxRawBlocks!.push({ startIndex: this.length, xml: stripRootNamespaces(serializeXml(el)) });
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
  for (const tc of tableCells(tr)) n += Math.max(1, num(wval(child(child(tc, NS.w, 'tcPr'), NS.w, 'gridSpan'))) ?? 1);
  return n;
}

/** Word wraps rows and cells in content controls (w:sdt/w:sdtContent) and tracked changes; look through them. */
function unwrapped(parent: Element, localName: 'tr' | 'tc'): Element[] {
  const out: Element[] = [];
  for (const el of elementChildren(parent)) {
    if (el.namespaceURI !== NS.w) continue;
    if (el.localName === localName) out.push(el);
    else if (el.localName === 'sdt') {
      const content = child(el, NS.w, 'sdtContent');
      if (content) out.push(...unwrapped(content, localName));
    } else if (el.localName === 'ins' || el.localName === 'customXml') out.push(...unwrapped(el, localName));
  }
  return out;
}

/** The content controls between an element and the row or table that holds it, outermost last. */
function enclosingSdts(el: Element, boundary: Element): Element[] {
  const out: Element[] = [];
  for (let p = el.parentElement; p && p !== boundary; p = p.parentElement) {
    if (p.namespaceURI === NS.w && p.localName === 'sdt') out.push(p);
  }
  return out;
}

function tableRows(tbl: Element): Element[] {
  return unwrapped(tbl, 'tr');
}

function tableCells(tr: Element): Element[] {
  return unwrapped(tr, 'tc');
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

/** Whether an mc:AlternateContent holds a picture we read (anything else stays raw). */
function hasPicture(ac: Element): boolean {
  const choice = child(ac, NS.mc, 'Choice') ?? child(ac, NS.mc, 'Fallback');
  return !!choice && choice.getElementsByTagNameNS(NS.pic, 'pic').length > 0;
}

/** What a non-picture drawing holds, for the notice. */
function drawingKind(drawing: Element): string {
  const uri = drawing.getElementsByTagNameNS(NS.a, 'graphicData')[0]?.getAttribute('uri') ?? '';
  if (uri.includes('chart')) return 'chart';
  if (uri.includes('wordprocessingGroup')) return 'group of shapes';
  if (uri.includes('wordprocessingShape')) return 'shape or text box';
  if (uri.includes('diagram')) return 'SmartArt';
  if (uri.includes('picture')) return 'linked picture';
  return 'drawing';
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
