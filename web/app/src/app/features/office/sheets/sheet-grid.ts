import { UniverRange } from '../xlsx/univer-sheet-types';

export type GridValue = string | number | boolean | null;

/**
 * What the chart, pivot and slicer panels need from the open sheet, and nothing of Univer's: read and
 * write cells, know the selection, hide rows, and hear about edits. A plain interface, so the panels'
 * logic can be tested against an in-memory grid (see fake-sheet-grid.spec-helper.ts).
 */
export interface SheetGrid {
  sheetNames(): string[];
  activeSheetName(): string | null;
  /** The current selection, when there is one. */
  selection(): { sheet: string; range: UniverRange } | null;
  /** Calculated values, row by row; an empty cell is null. */
  values(sheet: string, range: UniverRange): GridValue[][];
  /** Writes values from a top-left cell; null clears a cell's value. */
  write(sheet: string, row: number, column: number, values: GridValue[][]): void;
  clear(sheet: string, range: UniverRange): void;
  setRowsHidden(sheet: string, firstRow: number, count: number, hidden: boolean): void;
  isRowHidden(sheet: string, row: number): boolean;
  /** Called, debounced, after anything changes the sheet's content. Returns the way to stop listening. */
  onChange(listener: () => void): () => void;
}

interface FacadeRange {
  getValues(): (GridValue | undefined)[][];
  setValues(values: GridValue[][]): unknown;
  clear(options?: { contentsOnly?: boolean }): unknown;
  getRange(): UniverRange;
}

interface FacadeSheet {
  getSheetName(): string;
  getRange(row: number, column: number, rows: number, columns: number): FacadeRange;
  hideRows(row: number, count?: number): unknown;
  showRows(row: number, count?: number): unknown;
  getRowHidden?(row: number): boolean;
  isRowHiddenByUser?(row: number): boolean;
}

interface FacadeWorkbook {
  getSheets(): FacadeSheet[];
  getActiveSheet(): FacadeSheet | null;
  getSheetByName(name: string): FacadeSheet | null;
  getActiveRange(): FacadeRange | null;
}

const CHANGE_DEBOUNCE_MS = 200;
const MUTATION = 2;
const VIEW_ONLY = /scroll|selection|zoom|skeleton|active|viewport|freeze-render|render|cursor|focus/i;

/** SheetGrid over Univer's facade. */
export class FacadeSheetGrid implements SheetGrid {
  constructor(
    private readonly workbook: FacadeWorkbook,
    private readonly api: {
      Event: { CommandExecuted: string };
      addEvent(name: string, fn: (event: { id?: string; type?: number }) => void): { dispose(): void };
    },
  ) {}

  sheetNames(): string[] {
    return this.workbook.getSheets().map(s => s.getSheetName());
  }

  activeSheetName(): string | null {
    return this.workbook.getActiveSheet()?.getSheetName() ?? null;
  }

  selection(): { sheet: string; range: UniverRange } | null {
    const range = this.workbook.getActiveRange();
    const sheet = this.activeSheetName();
    return range && sheet ? { sheet, range: range.getRange() } : null;
  }

  values(sheet: string, range: UniverRange): GridValue[][] {
    const values = this.range(sheet, range)?.getValues() ?? [];
    return values.map(row => row.map(v => (v === undefined ? null : v)));
  }

  write(sheet: string, row: number, column: number, values: GridValue[][]): void {
    if (!values.length || !values[0].length) return;
    this.sheet(sheet)?.getRange(row, column, values.length, values[0].length).setValues(values);
  }

  clear(sheet: string, range: UniverRange): void {
    this.range(sheet, range)?.clear({ contentsOnly: true });
  }

  setRowsHidden(sheet: string, firstRow: number, count: number, hidden: boolean): void {
    const target = this.sheet(sheet);
    if (hidden) target?.hideRows(firstRow, count);
    else target?.showRows(firstRow, count);
  }

  isRowHidden(sheet: string, row: number): boolean {
    const target = this.sheet(sheet);
    return target?.isRowHiddenByUser?.(row) ?? target?.getRowHidden?.(row) ?? false;
  }

  onChange(listener: () => void): () => void {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const subscription = this.api.addEvent(this.api.Event.CommandExecuted, event => {
      if (event.type !== MUTATION || VIEW_ONLY.test(event.id ?? '')) return;
      if (timer) clearTimeout(timer);
      timer = setTimeout(listener, CHANGE_DEBOUNCE_MS);
    });
    return () => {
      if (timer) clearTimeout(timer);
      subscription.dispose();
    };
  }

  private sheet(name: string): FacadeSheet | null {
    return this.workbook.getSheetByName(name);
  }

  private range(sheet: string, r: UniverRange): FacadeRange | undefined {
    return this.sheet(sheet)?.getRange(r.startRow, r.startColumn, r.endRow - r.startRow + 1, r.endColumn - r.startColumn + 1);
  }
}
