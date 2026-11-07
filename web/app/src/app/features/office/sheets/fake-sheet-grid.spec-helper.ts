import { UniverRange } from '../xlsx/univer-sheet-types';
import { GridValue, SheetGrid } from './sheet-grid';

/** An in-memory SheetGrid for panel and engine specs. */
export class FakeSheetGrid implements SheetGrid {
  readonly hiddenRows = new Map<string, Set<number>>();
  active: string;
  chosen: { sheet: string; range: UniverRange } | null = null;
  private readonly listeners = new Set<() => void>();

  constructor(readonly sheets: Record<string, GridValue[][]>) {
    this.active = Object.keys(sheets)[0];
  }

  sheetNames(): string[] {
    return Object.keys(this.sheets);
  }

  activeSheetName(): string | null {
    return this.active;
  }

  selection(): { sheet: string; range: UniverRange } | null {
    return this.chosen;
  }

  values(sheet: string, r: UniverRange): GridValue[][] {
    const out: GridValue[][] = [];
    for (let row = r.startRow; row <= r.endRow; row++) {
      const line: GridValue[] = [];
      for (let column = r.startColumn; column <= r.endColumn; column++) line.push(this.sheets[sheet]?.[row]?.[column] ?? null);
      out.push(line);
    }
    return out;
  }

  write(sheet: string, row: number, column: number, values: GridValue[][]): void {
    const grid = (this.sheets[sheet] ??= []);
    values.forEach((line, r) => {
      line.forEach((value, c) => {
        (grid[row + r] ??= [])[column + c] = value;
      });
    });
    this.changed();
  }

  clear(sheet: string, r: UniverRange): void {
    for (let row = r.startRow; row <= r.endRow; row++) {
      for (let column = r.startColumn; column <= r.endColumn; column++) if (this.sheets[sheet]?.[row]) this.sheets[sheet][row][column] = null;
    }
    this.changed();
  }

  setRowsHidden(sheet: string, firstRow: number, count: number, hidden: boolean): void {
    const set = this.hiddenRows.get(sheet) ?? new Set<number>();
    for (let i = 0; i < count; i++) {
      if (hidden) set.add(firstRow + i);
      else set.delete(firstRow + i);
    }
    this.hiddenRows.set(sheet, set);
    this.changed();
  }

  isRowHidden(sheet: string, row: number): boolean {
    return this.hiddenRows.get(sheet)?.has(row) ?? false;
  }

  onChange(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /** Test hook: an edit happened. */
  changed(): void {
    this.listeners.forEach(fn => fn());
  }
}
