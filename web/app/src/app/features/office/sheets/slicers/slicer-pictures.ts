import { DrawingRepaint, drawingsRightEdge } from '../charts/sheet-drawings';
import { FidelityCollector } from '../../shared/fidelity';
import { RESOURCE_DRAWINGS, UniverDrawingsForSheet, UniverSheet, UniverSheetImage, UniverWorkbook, readResource, writeResource } from '../../xlsx/univer-sheet-types';
import { PreservedParts } from '../../xlsx/xlsx-preserve';
import { PivotWorkspace } from '../pivots/pivot-workspace';
import { GridValue, SheetGrid } from '../sheet-grid';
import { SlicerAnchor } from '../../xlsx/xlsx-slicers';
import { emuToPx, pxToEmu } from '../../shared/ooxml';
import { slicerSvgDataUrl } from './slicer-svg';
import { SlicerEntry, SlicerPictures, SlicerWorkspace, pictureId } from './slicer-workspace';

interface FacadeImage {
  setSource(source: string, type?: string): boolean;
  getId(): string;
}

interface FacadeSheet {
  getImageById(id: string): FacadeImage | null;
  insertImages(images: UniverSheetImage[]): unknown;
  deleteImages(images: FacadeImage[]): unknown;
  getSheetId(): string;
}

interface FacadeBook {
  getSheetByName(name: string): FacadeSheet | null;
  getId(): string;
  save(): UniverWorkbook;
}

function colLeft(sheet: Pick<UniverSheet, 'columnData' | 'defaultColumnWidth'>, col: number): number {
  let x = 0;
  for (let i = 0; i < col; i++) x += sheet.columnData?.[i]?.w ?? sheet.defaultColumnWidth;
  return x;
}

function rowTop(sheet: Pick<UniverSheet, 'rowData' | 'defaultRowHeight'>, row: number): number {
  let y = 0;
  for (let i = 0; i < row; i++) y += sheet.rowData?.[i]?.h ?? sheet.defaultRowHeight;
  return y;
}

/** A slicer's drawing for the snapshot or the facade, at its anchor. */
export function slicerDrawing(unitId: string, sheet: UniverSheet, entry: Pick<SlicerEntry, 'name' | 'caption' | 'anchor'>, items: readonly { label: string; selected: boolean }[]): UniverSheetImage | null {
  const anchor = entry.anchor;
  if (!anchor) return null;
  const left = colLeft(sheet, anchor.from.col) + emuToPx(anchor.from.colOff);
  const top = rowTop(sheet, anchor.from.row) + emuToPx(anchor.from.rowOff);
  const width = Math.max(60, colLeft(sheet, anchor.to.col) + emuToPx(anchor.to.colOff) - left);
  const height = Math.max(60, rowTop(sheet, anchor.to.row) + emuToPx(anchor.to.rowOff) - top);
  const drawingId = pictureId(entry);
  return {
    unitId,
    subUnitId: sheet.id,
    drawingId,
    drawingType: 0,
    imageSourceType: 'BASE64',
    source: slicerSvgDataUrl(entry.caption, items, width, height),
    transform: { left, top, width, height, angle: 0 },
    sheetTransform: {
      from: { column: anchor.from.col, columnOffset: emuToPx(anchor.from.colOff), row: anchor.from.row, rowOffset: emuToPx(anchor.from.rowOff) },
      to: { column: anchor.to.col, columnOffset: emuToPx(anchor.to.colOff), row: anchor.to.row, rowOffset: emuToPx(anchor.to.rowOff) },
      angle: 0,
    },
    anchorType: '1',
  };
}

/** Slicer pictures over Univer's facade. */
export class FacadeSlicerPictures implements SlicerPictures {
  constructor(
    private readonly book: FacadeBook,
    private readonly repaint?: DrawingRepaint,
  ) {}

  rightEdge(sheet: string): number {
    return drawingsRightEdge(this.book.save(), sheet);
  }

  update(entry: SlicerEntry, items: readonly { label: string; selected: boolean }[]): void {
    const image = this.book.getSheetByName(entry.sheet)?.getImageById(pictureId(entry));
    if (!image) return;
    const snapshot = this.snapshotSheet(entry);
    const drawing = snapshot ? slicerDrawing(this.book.getId(), snapshot, entry, items) : null;
    if (!drawing) return;
    image.setSource(drawing.source, 'BASE64');
    if (snapshot) this.repaint?.({ unitId: this.book.getId(), subUnitId: snapshot.id, drawingId: pictureId(entry) }, drawing.source);
  }

  add(entry: SlicerEntry, items: readonly { label: string; selected: boolean }[]): void {
    const sheet = this.book.getSheetByName(entry.sheet);
    const snapshot = this.snapshotSheet(entry);
    if (!sheet || !snapshot) return;
    const drawing = slicerDrawing(this.book.getId(), snapshot, entry, items);
    if (drawing) sheet.insertImages([drawing]);
  }

  remove(entry: SlicerEntry): void {
    const sheet = this.book.getSheetByName(entry.sheet);
    const image = sheet?.getImageById(pictureId(entry));
    if (sheet && image) sheet.deleteImages([image]);
  }

  /** Where the reader left the picture, read from the snapshot's drawing resource. */
  anchorOf(entry: SlicerEntry): SlicerAnchor | null {
    const data = this.book.save();
    const sheetId = Object.values(data.sheets).find(s => s.name === entry.sheet)?.id;
    const raw = data.resources.find(r => r.name === RESOURCE_DRAWINGS)?.data;
    if (!raw || !sheetId) return entry.anchor;
    const drawing = (JSON.parse(raw) as Record<string, { data: Record<string, UniverSheetImage> }>)[sheetId]?.data[pictureId(entry)];
    if (!drawing) return entry.anchor;
    const { from, to } = drawing.sheetTransform;
    return {
      from: { col: from.column, colOff: pxToEmu(from.columnOffset), row: from.row, rowOff: pxToEmu(from.rowOffset) },
      to: { col: to.column, colOff: pxToEmu(to.columnOffset), row: to.row, rowOff: pxToEmu(to.rowOffset) },
    };
  }

  private snapshotSheet(entry: SlicerEntry): UniverSheet | undefined {
    return Object.values(this.book.save().sheets).find(s => s.name === entry.sheet);
  }
}

/** Read-only cells from the snapshot, for reading state before Univer is up. */
class SnapshotGrid implements SheetGrid {
  constructor(private readonly workbook: UniverWorkbook) {}

  private sheet(name: string): UniverSheet | undefined {
    return Object.values(this.workbook.sheets).find(s => s.name === name);
  }

  sheetNames(): string[] {
    return this.workbook.sheetOrder.map(id => this.workbook.sheets[id].name);
  }

  addSheet(): void {}

  activeSheetName(): string | null {
    return null;
  }

  selection(): null {
    return null;
  }

  values(sheet: string, r: { startRow: number; startColumn: number; endRow: number; endColumn: number }): GridValue[][] {
    const cells = this.sheet(sheet)?.cellData ?? {};
    const out: GridValue[][] = [];
    for (let row = r.startRow; row <= r.endRow; row++) {
      const line: GridValue[] = [];
      for (let column = r.startColumn; column <= r.endColumn; column++) {
        const cell = cells[row]?.[column];
        line.push((cell?.v ?? null) as GridValue);
      }
      out.push(line);
    }
    return out;
  }

  write(): void {}
  clear(): void {}
  setRowsHidden(): void {}

  isRowHidden(): boolean {
    return false;
  }

  onChange(): () => void {
    return () => undefined;
  }
}

/** Draws each slicer where it sits, in the state the file holds, for viewing and editing alike. */
export function addSlicerPictures(workbook: UniverWorkbook, preserved: PreservedParts): void {
  if (!Object.keys(preserved.slicers?.sheetSlicers ?? {}).length) return;
  const grid = new SnapshotGrid(workbook);
  const pivots = new PivotWorkspace(preserved);
  pivots.attach(grid);
  const slicers = new SlicerWorkspace(preserved, pivots);
  slicers.attach(grid, null);
  const drawings = readResource<Record<string, UniverDrawingsForSheet>>(workbook, RESOURCE_DRAWINGS) ?? {};
  for (const entry of slicers.entries()) {
    const sheet = Object.values(workbook.sheets).find(s => s.name === entry.sheet);
    const drawing = sheet ? slicerDrawing(workbook.id, sheet, entry, slicers.itemsFor(entry)) : null;
    if (!sheet || !drawing) continue;
    const forSheet = (drawings[sheet.id] ??= { data: {}, order: [] });
    forSheet.data[drawing.drawingId] = drawing;
    forSheet.order.push(drawing.drawingId);
  }
  writeResource(workbook, RESOURCE_DRAWINGS, drawings);
}

/** Pivots the editor reads and changes need no notice; these say what is different about the rest. */
export function notePivotLimits(preserved: PreservedParts, fidelity: FidelityCollector): void {
  const pivots = new PivotWorkspace(preserved);
  for (const entry of pivots.entries()) {
    if (entry.limits.length) fidelity.warn('Pivot tables with calculated fields, grouped items, value filters or outside data are kept in the file and Excel refreshes them, but they cannot be changed here', entry.sheet);
  }
  if (pivots.entries().some(e => !e.limits.length)) fidelity.warn('Changing a pivot table here uses the compact layout and leaves number and cell formats to Excel, which rebuilds it on open');
}
