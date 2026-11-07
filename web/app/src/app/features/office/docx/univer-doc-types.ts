/**
 * The slice of Univer's IDocumentData the docx converters read and write, typed locally so they
 * carry no runtime dependency on Univer. Numbers for enums are Univer's own values.
 */
import type { UniverStyle } from '../xlsx/xlsx-styles';

/** ITextStyle: the sheet style's text subset plus the document-only flags. */
export interface DocTextStyle extends Pick<UniverStyle, 'ff' | 'fs' | 'it' | 'bl' | 'ul' | 'st' | 'cl' | 'bg' | 'va'> {
  hidden?: boolean;
  caps?: boolean;
  smallCaps?: boolean;
  /** Character spacing, in points. */
  sc?: number;
}

export interface NumberUnit {
  v: number;
  /** NumberUnitType: 0 point, 1 line, 2 character, 3 pixel, 4 percent. */
  u?: number;
}

export interface DocParagraphBorder {
  color: { rgb?: string | null };
  width: number;
  /** DashStyleType: 1 solid, 2 dot, 3 dash. */
  dashStyle: number;
  padding: number;
}

export interface DocParagraphStyle {
  /** HorizontalAlign: 1 left, 2 center, 3 right, 4 justified, 6 distributed. */
  horizontalAlign?: number;
  /** NamedStyleType: 2 title, 3 subtitle, 4..8 heading 1..5. */
  namedStyleType?: number;
  headingId?: string;
  outlineLevel?: number;
  lineSpacing?: number;
  /** SpacingRule: 0 auto (multiple), 1 at least, 2 exact. */
  spacingRule?: number;
  spaceAbove?: NumberUnit;
  spaceBelow?: NumberUnit;
  indentFirstLine?: NumberUnit;
  hanging?: NumberUnit;
  indentStart?: NumberUnit;
  indentEnd?: NumberUnit;
  keepLines?: 0 | 1;
  keepNext?: 0 | 1;
  pageBreakBefore?: 0 | 1;
  widowControl?: 0 | 1;
  contextualSpacing?: 0 | 1;
  shading?: { backgroundColor: { rgb?: string | null } };
  borderTop?: DocParagraphBorder;
  borderBottom?: DocParagraphBorder;
  borderLeft?: DocParagraphBorder;
  borderRight?: DocParagraphBorder;
  /** Text formatting every run in the paragraph starts from (named style text, resolved). */
  textStyle?: DocTextStyle;
  paragraphMarkTextStyle?: DocTextStyle;
  /** TextDirection: 1 LTR, 2 RTL. */
  direction?: number;
}

export interface DocBullet {
  listType: string;
  listId: string;
  nestingLevel: number;
  startNumber?: number;
}

export interface DocParagraph {
  startIndex: number;
  paragraphId: string;
  styleId?: string;
  paragraphStyle?: DocParagraphStyle;
  bullet?: DocBullet;
}

export interface DocTextRun {
  st: number;
  ed: number;
  /** A character style id; its formatting is also baked into ts. */
  sId?: string;
  ts?: DocTextStyle;
}

export interface DocCustomRange {
  startIndex: number;
  endIndex: number;
  rangeId: string;
  /** CustomRangeType: 0 hyperlink, 1 field, 3 bookmark. */
  rangeType: number;
  wholeEntity?: boolean;
  properties?: Record<string, unknown>;
}

export interface DocCustomBlock {
  startIndex: number;
  blockId: string;
  /** BlockType.DRAWING is 0. */
  blockType?: number;
}

export interface DocCustomTable {
  startIndex: number;
  endIndex: number;
  tableId: string;
}

export interface DocRawBlock {
  startIndex: number;
  xml: string;
}

export interface DocSectionBreak extends DocPageSetup {
  sectionId: string;
  startIndex: number;
  /** SectionType: 1 continuous, 3 next page, 4 even, 5 odd. */
  sectionType?: number;
  columnProperties?: { width: number; paddingEnd: number }[];
  /** ColumnSeparatorType: 1 none, 2 between each column. */
  columnSeparatorType?: number;
}

export interface DocBody {
  dataStream: string;
  textRuns?: DocTextRun[];
  paragraphs?: DocParagraph[];
  sectionBreaks?: DocSectionBreak[];
  customBlocks?: DocCustomBlock[];
  customRanges?: DocCustomRange[];
  tables?: DocCustomTable[];
  docxRawBlocks?: DocRawBlock[];
}

export interface DocPageSetup {
  /** 96-dpi pixels. */
  pageSize?: { width: number; height: number };
  /** PageOrientType: 0 portrait, 1 landscape. */
  pageOrient?: number;
  marginTop?: number;
  marginBottom?: number;
  marginLeft?: number;
  marginRight?: number;
  marginHeader?: number;
  marginFooter?: number;
  pageNumberStart?: number;
  defaultHeaderId?: string;
  defaultFooterId?: string;
  firstPageHeaderId?: string;
  firstPageFooterId?: string;
  evenPageHeaderId?: string;
  evenPageFooterId?: string;
  useFirstPageHeaderFooter?: 0 | 1;
  evenAndOddHeaders?: 0 | 1;
}

export interface DocDocumentStyle extends DocPageSetup {
  /** DocumentFlavor.TRADITIONAL is 1 (paginated). */
  documentFlavor?: number;
  defaultTabStop?: number;
  textStyle?: DocTextStyle;
  defaultParagraphStyle?: Omit<DocParagraphStyle, 'headingId' | 'namedStyleType' | 'outlineLevel' | 'textStyle' | 'paragraphMarkTextStyle'>;
}

export interface DocNamedStyle {
  name: string;
  basedOn?: string;
  link?: string;
  /** DocStyleType: 0 character, 1 paragraph. */
  type: 0 | 1;
  textStyle?: DocTextStyle;
  paragraphStyle?: DocParagraphStyle;
}

export interface DocListLevel {
  /** BulletAlignment: 1 start, 2 center, 3 end. */
  bulletAlignment: number;
  /** "%1." for numbers, " %1" with glyphSymbol for bullets. */
  glyphFormat: string;
  /** ListGlyphType: 0 bullet, 1 none, 2 decimal, 4 upper letter, 5 lower letter, 6 upper roman, 7 lower roman. */
  glyphType?: number;
  glyphSymbol?: string;
  startNumber: number;
  paragraphProperties?: DocParagraphStyle;
  textStyle?: DocTextStyle;
}

export interface DocList {
  listType: string;
  nestingLevel: DocListLevel[];
}

export interface DocHeaderFooter {
  body: DocBody;
  tableSource?: Record<string, DocTable>;
  drawings?: Record<string, DocDrawing>;
  drawingsOrder?: string[];
}

export interface DocHeader extends DocHeaderFooter {
  headerId: string;
}

export interface DocFooter extends DocHeaderFooter {
  footerId: string;
}

export interface DocTableCellBorder {
  color: { rgb?: string | null };
  width?: NumberUnit;
  /** DashStyleType: 1 solid, 2 dot, 3 dash. */
  dashStyle?: number;
}

export interface DocTableCellMargin {
  start: NumberUnit;
  end: NumberUnit;
  top: NumberUnit;
  bottom: NumberUnit;
}

export interface DocTableCell {
  margin?: DocTableCellMargin;
  rowSpan?: number;
  columnSpan?: number;
  backgroundColor?: { rgb?: string | null };
  borderLeft?: DocTableCellBorder;
  borderRight?: DocTableCellBorder;
  borderTop?: DocTableCellBorder;
  borderBottom?: DocTableCellBorder;
  size?: { type: number; width: NumberUnit };
  /** VerticalAlignmentType: 2 top, 3 center, 4 bottom. */
  vAlign?: number;
}

export interface DocTableRow {
  tableCells: DocTableCell[];
  /** TableRowHeightRule: 0 auto, 1 at least, 2 exact. */
  trHeight: { val: NumberUnit; hRule: number };
  cantSplit?: 0 | 1;
  repeatHeaderRow?: 0 | 1;
}

export interface DocTable {
  tableId: string;
  tableRows: DocTableRow[];
  tableColumns: { size: { type: number; width: NumberUnit } }[];
  /** TableAlignmentType: 0 start, 1 center, 2 end. */
  align: number;
  indent: NumberUnit;
  /** TableTextWrapType: 0 none, 1 wrap. */
  textWrap: number;
  position: { positionH: { relativeFrom: number; posOffset?: number }; positionV: { relativeFrom: number; posOffset?: number } };
  dist: { distT: number; distB: number; distL: number; distR: number };
  /** TableSizeType: 0 unspecified, 1 specified. */
  size: { type: number; width: NumberUnit };
  cellMargin?: DocTableCellMargin;
  /** TableLayoutType: 0 auto fit, 1 fixed. */
  layout?: number;
  description?: string;
}

export interface DocDrawing {
  unitId: string;
  subUnitId: string;
  drawingId: string;
  /** DrawingTypeEnum.DRAWING_IMAGE is 0. */
  drawingType: number;
  imageSourceType: 'BASE64' | 'URL';
  source: string;
  title?: string;
  description?: string;
  transform?: { left: number; top: number; width: number; height: number; angle: number };
  docTransform: {
    size: { width: number; height: number };
    /** ObjectRelativeFromH: 0 page, 1 column, 2 character, 3 margin. */
    positionH: { relativeFrom: number; posOffset?: number; align?: number };
    /** ObjectRelativeFromV: 0 page, 1 paragraph, 2 line, 3 margin. */
    positionV: { relativeFrom: number; posOffset?: number; align?: number };
    angle: number;
  };
  /** PositionedObjectLayoutType: 0 inline, 1 none, 2 polygon, 3 square, 4 through, 5 tight, 6 top and bottom. */
  layoutType: number;
  behindDoc?: 0 | 1;
  /** WrapTextType: 0 both sides, 1 left, 2 right, 3 largest. */
  wrapText?: number;
  distT?: number;
  distB?: number;
  distL?: number;
  distR?: number;
}

export interface DocDocument {
  id: string;
  title?: string;
  body: DocBody;
  documentStyle: DocDocumentStyle;
  styles?: Record<string, DocNamedStyle>;
  headers?: Record<string, DocHeader>;
  footers?: Record<string, DocFooter>;
  lists?: Record<string, DocList>;
  tableSource?: Record<string, DocTable>;
  drawings?: Record<string, DocDrawing>;
  drawingsOrder?: string[];
  settings?: { zoomRatio?: number };
}

// Data stream tokens (DataStreamTreeTokenType).
export const TOKEN = {
  PARAGRAPH: '\r',
  SECTION_BREAK: '\n',
  TABLE_START: '\u001A',
  TABLE_ROW_START: '\u001B',
  TABLE_CELL_START: '\u001C',
  TABLE_CELL_END: '\u001D',
  TABLE_ROW_END: '\u000E',
  TABLE_END: '\u000F',
  CUSTOM_RANGE_START: '\u001F',
  CUSTOM_RANGE_END: '\u001E',
  COLUMN_BREAK: '\v',
  PAGE_BREAK: '\f',
  TAB: '\t',
  CUSTOM_BLOCK: '\b',
} as const;

export const CUSTOM_RANGE = { HYPERLINK: 0, FIELD: 1, SDT: 2, BOOKMARK: 3 } as const;
export const DOC_HALIGN = { left: 1, center: 2, right: 3, justified: 4, distributed: 6 } as const;
export const NAMED_STYLE = { title: 2, subtitle: 3, heading1: 4, heading2: 5, heading3: 6, heading4: 7, heading5: 8 } as const;
export const GLYPH = {
  bullet: 0,
  none: 1,
  decimal: 2,
  decimalZero: 3,
  upperLetter: 4,
  lowerLetter: 5,
  upperRoman: 6,
  lowerRoman: 7,
} as const;
export const LAYOUT = { inline: 0, none: 1, polygon: 2, square: 3, through: 4, tight: 5, topAndBottom: 6 } as const;
