import { RESOURCE_DRAWINGS } from '../../xlsx/univer-sheet-types';

/**
 * The pictures over the grid, as the chart session needs them. Charts are pictures Univer can move and
 * resize, redrawn when their data changes. A plain interface, so the session is testable without Univer.
 */
export interface SheetDrawings {
  sheetId(sheet: string): string | null;
  /** The drawing ids on a sheet. */
  ids(sheet: string): string[];
  /** A picture's current size in pixels. */
  size(sheet: string, id: string): { width: number; height: number } | null;
  setSource(sheet: string, id: string, source: string): void;
  insert(sheet: string, image: { id: string; source: string; column: number; row: number; width: number; height: number }): Promise<void>;
  remove(sheet: string, id: string): void;
  /** The last column any picture on the sheet reaches, or -1; new objects go to the right of it. */
  rightEdge?(sheet: string): number;
}

/** Paints a changed picture source straight away (see UniverSheetsKit.repaintDrawing). */
export type DrawingRepaint = (drawing: { unitId: string; subUnitId: string; drawingId: string }, source: string) => void;

/** The last column the pictures on a sheet reach, from a snapshot's drawing resource; -1 with none. */
export function drawingsRightEdge(data: { sheets: Record<string, { id: string; name: string }>; resources: { name: string; data: string }[] }, sheet: string): number {
  const sheetId = Object.values(data.sheets).find(s => s.name === sheet)?.id;
  const raw = data.resources.find(r => r.name === RESOURCE_DRAWINGS)?.data;
  if (!raw || !sheetId) return -1;
  const drawings = (JSON.parse(raw) as Record<string, { data?: Record<string, { sheetTransform?: { to?: { column?: number } } }> }>)[sheetId]?.data ?? {};
  return Math.max(-1, ...Object.values(drawings).map(d => d.sheetTransform?.to?.column ?? -1));
}

interface FacadeImage {
  getId(): string;
  getPlacement?(): { width?: number; height?: number } | null;
  setSource(source: string, type?: string): boolean;
  remove(): boolean;
}

interface FacadeImageBuilder {
  setImage(image: Record<string, unknown>): FacadeImageBuilder;
  setColumn(column: number): FacadeImageBuilder;
  setRow(row: number): FacadeImageBuilder;
  setWidth(width: number): FacadeImageBuilder;
  setHeight(height: number): FacadeImageBuilder;
  buildAsync(): Promise<unknown>;
}

interface FacadeSheet {
  getSheetId(): string;
  getImages(): FacadeImage[];
  getImageById(id: string): FacadeImage | null;
  newOverGridImage(): FacadeImageBuilder;
  insertImages(images: unknown[]): unknown;
}

interface FacadeWorkbook {
  getId(): string;
  getSheetByName(name: string): FacadeSheet | null;
  save?(): Parameters<typeof drawingsRightEdge>[0];
}

/** SheetDrawings over the drawing facade of `@univerjs/sheets-drawing`. */
export class FacadeSheetDrawings implements SheetDrawings {
  constructor(
    private readonly workbook: FacadeWorkbook,
    private readonly repaint?: DrawingRepaint,
  ) {}

  rightEdge(sheet: string): number {
    const data = this.workbook.save?.();
    return data ? drawingsRightEdge(data, sheet) : -1;
  }

  sheetId(sheet: string): string | null {
    return this.workbook.getSheetByName(sheet)?.getSheetId() ?? null;
  }

  ids(sheet: string): string[] {
    return this.workbook.getSheetByName(sheet)?.getImages().map(i => i.getId()) ?? [];
  }

  size(sheet: string, id: string): { width: number; height: number } | null {
    const placement = this.workbook.getSheetByName(sheet)?.getImageById(id)?.getPlacement?.();
    return placement && typeof placement.width === 'number' && typeof placement.height === 'number' ? { width: placement.width, height: placement.height } : null;
  }

  setSource(sheet: string, id: string, source: string): void {
    const target = this.workbook.getSheetByName(sheet);
    if (!target?.getImageById(id)?.setSource(source, 'BASE64')) return;
    this.repaint?.({ unitId: this.workbook.getId(), subUnitId: target.getSheetId(), drawingId: id }, source);
  }

  async insert(sheet: string, image: { id: string; source: string; column: number; row: number; width: number; height: number }): Promise<void> {
    const target = this.workbook.getSheetByName(sheet);
    if (!target) return;
    const built = await target
      .newOverGridImage()
      .setImage({
        drawingId: image.id,
        drawingType: 0,
        imageSourceType: 'BASE64',
        source: image.source,
        unitId: this.workbook.getId(),
        subUnitId: target.getSheetId(),
      })
      .setColumn(image.column)
      .setRow(image.row)
      .setWidth(image.width)
      .setHeight(image.height)
      .buildAsync();
    target.insertImages([built]);
  }

  remove(sheet: string, id: string): void {
    this.workbook.getSheetByName(sheet)?.getImageById(id)?.remove();
  }
}
