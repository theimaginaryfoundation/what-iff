import { NS, attr, child, children, num, wval } from '../shared/ooxml';
import { cellBorder } from './docx-body';
import { DocTableCellBorder } from './univer-doc-types';

/**
 * Where a table's borders come from in Word, least to most specific: its table style (following
 * `basedOn`), the table's own `tblBorders`, then the style's formatting for the first and last rows
 * and columns where `tblLook` turns it on. A cell's own `tcBorders` then win over all of it.
 */

const SIDES = ['top', 'bottom', 'left', 'right', 'insideH', 'insideV'] as const;
type Side = (typeof SIDES)[number];
type BorderSet = Partial<Record<Side, DocTableCellBorder>>;
type Region = 'firstRow' | 'lastRow' | 'firstCol' | 'lastCol';

interface TableStyle {
  basedOn?: string;
  borders: BorderSet;
  regions: Partial<Record<Region, BorderSet>>;
}

export interface TableStyles {
  styles: Map<string, TableStyle>;
  defaultId?: string;
}

export interface CellPosition {
  isFirstRow: boolean;
  isLastRow: boolean;
  isFirstCol: boolean;
  isLastCol: boolean;
}

export type CellBorders = Record<'top' | 'bottom' | 'left' | 'right', DocTableCellBorder | undefined>;

function bordersFrom(container: Element | null): BorderSet {
  const out: BorderSet = {};
  for (const side of SIDES) {
    const el = child(container, NS.w, side);
    if (el) out[side] = cellBorder(el);
  }
  return out;
}

export function parseTableStyles(doc: Document | null): TableStyles {
  const out: TableStyles = { styles: new Map() };
  if (!doc) return out;
  for (const el of children(doc.documentElement, NS.w, 'style')) {
    if (attr(el, NS.w, 'type') !== 'table') continue;
    const id = attr(el, NS.w, 'styleId');
    if (!id) continue;
    if (attr(el, NS.w, 'default') === '1') out.defaultId = id;
    const regions: TableStyle['regions'] = {};
    for (const conditional of children(el, NS.w, 'tblStylePr')) {
      const type = attr(conditional, NS.w, 'type');
      if (type === 'firstRow' || type === 'lastRow' || type === 'firstCol' || type === 'lastCol') {
        regions[type] = bordersFrom(child(child(conditional, NS.w, 'tcPr'), NS.w, 'tcBorders'));
      }
    }
    out.styles.set(id, {
      basedOn: wval(child(el, NS.w, 'basedOn')) ?? undefined,
      borders: bordersFrom(child(child(el, NS.w, 'tblPr'), NS.w, 'tblBorders')),
      regions,
    });
  }
  return out;
}

/** The style's chain from its root down, so a derived style's values overwrite its base's. */
function styleChain(styles: TableStyles | undefined, id: string | undefined): TableStyle[] {
  const chain: TableStyle[] = [];
  const seen = new Set<string>();
  for (let current = id; current && !seen.has(current); ) {
    seen.add(current);
    const style = styles?.styles.get(current);
    if (!style) break;
    chain.unshift(style);
    current = style.basedOn;
  }
  return chain;
}

/** `tblLook` as attributes, or as the older hex bit mask. */
function lookOf(tblPr: Element | null): Record<Region, boolean> {
  const look = child(tblPr, NS.w, 'tblLook');
  const mask = parseInt(wval(look) ?? '0', 16) || 0;
  const flag = (name: string, bit: number) => {
    const value = attr(look, NS.w, name);
    return value === null || value === undefined ? (mask & bit) !== 0 : value === '1' || value === 'true';
  };
  return { firstRow: flag('firstRow', 0x20), lastRow: flag('lastRow', 0x40), firstCol: flag('firstColumn', 0x80), lastCol: flag('lastColumn', 0x100) };
}

export function tableBordersFor(tblPr: Element | null, styles: TableStyles | undefined): { forCell(position: CellPosition): CellBorders } {
  const chain = styleChain(styles, wval(child(tblPr, NS.w, 'tblStyle')) ?? styles?.defaultId);
  const table: BorderSet = Object.assign({}, ...chain.map(s => s.borders), bordersFrom(child(tblPr, NS.w, 'tblBorders')));
  const look = lookOf(tblPr);
  const region = (name: Region): BorderSet | null => (look[name] ? Object.assign({}, ...chain.map(s => s.regions[name] ?? {})) : null);
  const regions: Record<Region, BorderSet | null> = { firstRow: region('firstRow'), lastRow: region('lastRow'), firstCol: region('firstCol'), lastCol: region('lastCol') };
  const has = (set: BorderSet | null, side: Side) => !!set && side in set;
  return {
    forCell({ isFirstRow, isLastRow, isFirstCol, isLastCol }) {
      const out: CellBorders = {
        top: isFirstRow ? table.top : table.insideH,
        bottom: isLastRow ? table.bottom : table.insideH,
        left: isFirstCol ? table.left : table.insideV,
        right: isLastCol ? table.right : table.insideV,
      };
      // Later regions win, the way Word layers them: columns, then rows.
      const apply = (active: boolean, set: BorderSet | null, edges: { top: boolean; bottom: boolean; left: boolean; right: boolean }) => {
        if (!active || !set) return;
        for (const side of ['top', 'bottom', 'left', 'right'] as const) {
          if (edges[side] ? has(set, side) : has(set, side === 'top' || side === 'bottom' ? 'insideH' : 'insideV')) {
            out[side] = edges[side] ? set[side] : set[side === 'top' || side === 'bottom' ? 'insideH' : 'insideV'];
          }
        }
      };
      apply(isFirstCol, regions.firstCol, { top: isFirstRow, bottom: isLastRow, left: true, right: true });
      apply(isLastCol, regions.lastCol, { top: isFirstRow, bottom: isLastRow, left: true, right: true });
      apply(isFirstRow, regions.firstRow, { top: true, bottom: true, left: isFirstCol, right: isLastCol });
      apply(isLastRow, regions.lastRow, { top: true, bottom: true, left: isFirstCol, right: isLastCol });
      return out;
    },
  };
}
