import { RESOURCE_DRAWINGS, UniverDrawingsForSheet, UniverSheetImage, UniverWorkbook, readResource, writeResource } from '../../xlsx/univer-sheet-types';
import { SheetDrawings } from './sheet-drawings';

/** SheetDrawings over a workbook snapshot's drawing resource, the way Univer's snapshot would read after the same calls. */
export class FakeSheetDrawings implements SheetDrawings {
  readonly sources: { id: string; source: string }[] = [];

  constructor(readonly workbook: UniverWorkbook) {}

  private all(): Record<string, UniverDrawingsForSheet> {
    return readResource<Record<string, UniverDrawingsForSheet>>(this.workbook, RESOURCE_DRAWINGS) ?? {};
  }

  sheetId(sheet: string): string | null {
    return this.workbook.sheetOrder.find(id => this.workbook.sheets[id].name === sheet) ?? null;
  }

  ids(sheet: string): string[] {
    const id = this.sheetId(sheet);
    return id ? (this.all()[id]?.order ?? []) : [];
  }

  size(sheet: string, id: string): { width: number; height: number } | null {
    const image = this.all()[this.sheetId(sheet) ?? '']?.data[id];
    return image ? { width: image.transform.width, height: image.transform.height } : null;
  }

  setSource(sheet: string, id: string, source: string): void {
    this.sources.push({ id, source });
    this.patch(sheet, set => (set.data[id].source = source));
  }

  async insert(sheet: string, image: { id: string; source: string; column: number; row: number; width: number; height: number }): Promise<void> {
    this.patch(sheet, set => {
      const drawing: UniverSheetImage = {
        unitId: this.workbook.id,
        subUnitId: this.sheetId(sheet) as string,
        drawingId: image.id,
        drawingType: 0,
        imageSourceType: 'BASE64',
        source: image.source,
        transform: { left: 0, top: 0, width: image.width, height: image.height, angle: 0 },
        sheetTransform: {
          from: { column: image.column, columnOffset: 0, row: image.row, rowOffset: 0 },
          to: { column: image.column + 6, columnOffset: 0, row: image.row + 14, rowOffset: 0 },
          angle: 0,
        },
        anchorType: '1',
      };
      set.data[image.id] = drawing;
      set.order.push(image.id);
    });
  }

  remove(sheet: string, id: string): void {
    this.patch(sheet, set => {
      delete set.data[id];
      set.order = set.order.filter(x => x !== id);
    });
  }

  /** Test hook: the user dragged a picture. */
  move(sheet: string, id: string, column: number, row: number): void {
    this.patch(sheet, set => {
      const t = set.data[id].sheetTransform;
      const span = { c: t.to.column - t.from.column, r: t.to.row - t.from.row };
      t.from = { ...t.from, column, row };
      t.to = { ...t.to, column: column + span.c, row: row + span.r };
    });
  }

  private patch(sheet: string, change: (set: UniverDrawingsForSheet) => void): void {
    const all = this.all();
    const id = this.sheetId(sheet) as string;
    const set = (all[id] ??= { data: {}, order: [] });
    change(set);
    writeResource(this.workbook, RESOURCE_DRAWINGS, all);
  }
}
