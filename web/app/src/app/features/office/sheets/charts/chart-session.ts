import { computed, signal } from '@angular/core';

import { chartSvgDataUrl } from '../../shared/chart-svg';
import { UniverDrawingsForSheet } from '../../xlsx/univer-sheet-types';
import { ChartSaveState } from '../../xlsx/xlsx-chart-save';
import { ExcelThemeColors } from '../../xlsx/xlsx-theme';
import { SheetGrid } from '../sheet-grid';
import { ChartSpec, cachedSeries, chartDataFor, liveSeries, specFromSelection } from './chart-spec';
import { SheetDrawings } from './sheet-drawings';

const NEW_CHART_WIDTH_PX = 480;
const NEW_CHART_HEIGHT_PX = 288;
const RESIZE_REDRAW_PX = 2;

/**
 * The charts of the open workbook while it is edited: the specs, their pictures kept in step with the
 * cells, and what a save needs. Whether a chart exists is whether its picture does, so deleting it in
 * the grid, and undoing that, just work.
 */
export class ChartSession {
  private readonly specs = signal<readonly ChartSpec[]>([]);
  /** Ids whose picture is on its sheet. */
  private readonly present = signal<ReadonlySet<string>>(new Set());
  readonly charts = computed(() => this.specs().filter(c => this.present().has(c.id)));

  private readonly originals: readonly ChartSpec[];
  /** What each picture last drew, and at what size. */
  private readonly drawn = new Map<string, { data: string; width?: number; height?: number }>();
  private stop: (() => void) | null = null;
  private nextNew = 1;

  constructor(
    specs: readonly ChartSpec[],
    private readonly baseline: Record<string, UniverDrawingsForSheet>,
    private readonly grid: SheetGrid,
    private readonly drawings: SheetDrawings,
    private readonly theme: ExcelThemeColors,
    private readonly onEdited: () => void,
  ) {
    this.originals = specs;
    this.specs.set(specs);
    this.present.set(new Set(specs.map(s => s.id)));
    for (const spec of specs) this.drawn.set(spec.id, { data: this.dataKey(spec, cachedSeries(spec)) });
  }

  start(): void {
    this.stop = this.grid.onChange(() => this.refresh());
  }

  dispose(): void {
    this.stop?.();
    this.stop = null;
  }

  /** Follows the sheet: charts whose picture went or came back, data that changed, pictures resized. */
  refresh(): void {
    const present = new Set<string>();
    for (const name of new Set(this.specs().map(c => c.sheet))) for (const id of this.drawings.ids(name)) present.add(id);
    this.present.set(present);
    for (const spec of this.specs()) if (present.has(spec.id) && spec.editable) this.redraw(spec);
  }

  /** Edits a chart through the panel. */
  update(id: string, change: (spec: ChartSpec) => ChartSpec): void {
    let updated: ChartSpec | null = null;
    this.specs.update(all =>
      all.map(c => {
        if (c.id !== id || !c.editable) return c;
        updated = change(c);
        return updated;
      }),
    );
    if (!updated) return;
    this.redraw(updated);
    this.onEdited();
  }

  /** A new chart from the selection; false when the selection holds nothing to chart. */
  async addFromSelection(): Promise<ChartSpec | null> {
    const selection = this.grid.selection();
    const sheetId = selection ? this.drawings.sheetId(selection.sheet) : null;
    if (!selection || !sheetId) return null;
    const id = `chart-${sheetId}-n${this.nextNew}`;
    const spec = specFromSelection(id, this.grid, selection.sheet, selection.range, `Chart ${this.specs().length + 1}`);
    if (!spec || !liveSeries(spec, this.grid).some(s => s.values.some(v => v !== null))) return null;
    this.nextNew += 1;
    // Beside the data, and clear of anything already on the sheet.
    const clear = (this.drawings.rightEdge?.(spec.sheet) ?? -1) + 1;
    if (clear > spec.anchor.from.col) {
      const shift = clear - spec.anchor.from.col;
      spec.anchor = { from: { ...spec.anchor.from, col: clear }, to: { ...spec.anchor.to, col: spec.anchor.to.col + shift } };
    }
    const { data } = this.render(spec, NEW_CHART_WIDTH_PX, NEW_CHART_HEIGHT_PX);
    await this.drawings.insert(spec.sheet, {
      id,
      source: data,
      column: spec.anchor.from.col,
      row: spec.anchor.from.row,
      width: NEW_CHART_WIDTH_PX,
      height: NEW_CHART_HEIGHT_PX,
    });
    this.drawn.set(id, { data: this.dataKey(spec, liveSeries(spec, this.grid)), width: NEW_CHART_WIDTH_PX, height: NEW_CHART_HEIGHT_PX });
    this.specs.update(all => [...all, spec]);
    this.present.update(ids => new Set(ids).add(id));
    this.onEdited();
    return spec;
  }

  remove(id: string): void {
    const spec = this.specs().find(c => c.id === id);
    if (!spec) return;
    this.drawings.remove(spec.sheet, id);
    this.present.update(ids => {
      const next = new Set(ids);
      next.delete(id);
      return next;
    });
    this.onEdited();
  }

  saveState(): ChartSaveState {
    return {
      originals: this.originals,
      current: this.charts(),
      live: spec => liveSeries(spec, this.grid),
      baseline: this.baseline,
    };
  }

  private dataKey(spec: ChartSpec, live: ReturnType<typeof liveSeries>): string {
    return JSON.stringify(chartDataFor(spec, live, this.theme));
  }

  private render(spec: ChartSpec, width: number, height: number): { data: string } {
    return { data: chartSvgDataUrl(chartDataFor(spec, liveSeries(spec, this.grid), this.theme), { width, height, background: '#FFFFFF' }) };
  }

  private redraw(spec: ChartSpec): void {
    const size = this.drawings.size(spec.sheet, spec.id);
    const key = this.dataKey(spec, liveSeries(spec, this.grid));
    const last = this.drawn.get(spec.id);
    const resized =
      !!size && last?.width !== undefined && (Math.abs(size.width - last.width) > RESIZE_REDRAW_PX || Math.abs(size.height - (last.height ?? 0)) > RESIZE_REDRAW_PX);
    // The first sight of a picture's size is only noted: it was drawn at that size.
    const noted = { data: key, width: size?.width ?? last?.width, height: size?.height ?? last?.height };
    if (last?.data === key && !resized) {
      this.drawn.set(spec.id, noted);
      return;
    }
    this.drawn.set(spec.id, noted);
    const { width, height } = { width: size?.width ?? NEW_CHART_WIDTH_PX, height: size?.height ?? NEW_CHART_HEIGHT_PX };
    this.drawings.setSource(spec.sheet, spec.id, this.render(spec, width, height).data);
  }
}
