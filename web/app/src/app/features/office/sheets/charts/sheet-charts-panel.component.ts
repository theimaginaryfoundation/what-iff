import { ChangeDetectionStrategy, Component, computed, input, signal } from '@angular/core';

import { rangeA1 } from '../../shared/a1';
import { SheetGrid } from '../sheet-grid';
import { ChartSession } from './chart-session';
import { ChartGrouping, ChartKind, ChartSeriesSpec, ChartSpec, LegendPosition, parseChartRef, seriesRef } from './chart-spec';

const KINDS: readonly { value: ChartKind; label: string }[] = [
  { value: 'column', label: 'Column' },
  { value: 'bar', label: 'Bar' },
  { value: 'line', label: 'Line' },
  { value: 'area', label: 'Area' },
  { value: 'pie', label: 'Pie' },
  { value: 'doughnut', label: 'Doughnut' },
  { value: 'scatter', label: 'Scatter' },
  { value: 'radar', label: 'Radar' },
];
const GROUPINGS: readonly { value: ChartGrouping; label: string }[] = [
  { value: 'standard', label: 'Side by side' },
  { value: 'stacked', label: 'Stacked' },
  { value: 'percentStacked', label: '100% stacked' },
];
const LEGENDS: readonly { value: LegendPosition; label: string }[] = [
  { value: 'none', label: 'None' },
  { value: 'r', label: 'Right' },
  { value: 'l', label: 'Left' },
  { value: 't', label: 'Top' },
  { value: 'b', label: 'Bottom' },
];

type RefField = 'nameRef' | 'categories' | 'values';

/** The charts of the workbook: pick one to edit, or make one from the selected cells. */
@Component({
  selector: 'app-sheet-charts-panel',
  standalone: true,
  templateUrl: './sheet-charts-panel.component.html',
  styleUrl: './sheet-charts-panel.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SheetChartsPanelComponent {
  readonly session = input.required<ChartSession>();
  readonly grid = input.required<SheetGrid>();

  readonly kinds = KINDS;
  readonly groupings = GROUPINGS;
  readonly legends = LEGENDS;
  readonly selectedId = signal<string | null>(null);
  readonly message = signal<string | null>(null);
  readonly charts = computed(() => this.session().charts());
  readonly selected = computed(() => this.charts().find(c => c.id === this.selectedId()) ?? null);
  readonly stackable = computed(() => {
    const kind = this.selected()?.kind;
    return kind === 'column' || kind === 'bar' || kind === 'line' || kind === 'area';
  });
  readonly round = computed(() => {
    const kind = this.selected()?.kind;
    return kind === 'pie' || kind === 'doughnut';
  });

  select(id: string): void {
    this.message.set(null);
    this.selectedId.set(this.selectedId() === id ? null : id);
  }

  async addChart(): Promise<void> {
    const spec = await this.session().addFromSelection();
    if (spec) {
      this.message.set(null);
      this.selectedId.set(spec.id);
    } else {
      this.message.set('Select the cells to chart first, with numbers in them.');
    }
  }

  deleteSelected(): void {
    const id = this.selectedId();
    if (id) this.session().remove(id);
    this.selectedId.set(null);
  }

  update(change: Partial<ChartSpec>): void {
    const id = this.selectedId();
    if (id) this.session().update(id, spec => ({ ...spec, ...change }));
  }

  setKind(kind: ChartKind): void {
    const stackable = kind === 'column' || kind === 'bar' || kind === 'line' || kind === 'area';
    // A round chart draws one series.
    const round = kind === 'pie' || kind === 'doughnut';
    const id = this.selectedId();
    if (!id) return;
    this.session().update(id, spec => ({
      ...spec,
      kind,
      grouping: stackable ? spec.grouping : 'standard',
      series: round ? spec.series.slice(0, 1) : spec.series,
    }));
  }

  private updateSeries(index: number, change: (series: ChartSeriesSpec) => ChartSeriesSpec): void {
    const id = this.selectedId();
    if (id) this.session().update(id, spec => ({ ...spec, series: spec.series.map((s, i) => (i === index ? change(s) : s)) }));
  }

  setSeriesName(index: number, text: string): void {
    this.updateSeries(index, s => ({ ...s, nameLiteral: text, nameRef: null }));
  }

  setSeriesColor(index: number, color: string): void {
    this.updateSeries(index, s => ({ ...s, color }));
  }

  /** A typed reference; `Sheet1!A2:A9`, or just `A2:A9` on the chart's sheet. */
  setSeriesRef(index: number, field: RefField, text: string): void {
    const chart = this.selected();
    if (!chart) return;
    const trimmed = text.trim();
    if (!trimmed) {
      if (field === 'values') return this.message.set('A series needs its values.');
      this.message.set(null);
      return this.updateSeries(index, s => ({ ...s, [field]: null, ...(field === 'nameRef' && !s.nameLiteral ? { nameLiteral: `Series ${index + 1}` } : {}) }));
    }
    const cells = parseChartRef(trimmed.includes('!') ? trimmed : `'${chart.sheet.replace(/'/g, "''")}'!${trimmed}`);
    if (!cells || !this.grid().sheetNames().includes(cells.sheet)) return this.message.set(`"${trimmed}" is not a range on a sheet here.`);
    this.message.set(null);
    this.updateSeries(index, s => ({ ...s, [field]: seriesRef(cells), ...(field === 'nameRef' ? { nameLiteral: null } : {}) }));
  }

  useSelection(index: number, field: RefField): void {
    const selection = this.grid().selection();
    if (!selection) return this.message.set('Select some cells first.');
    this.setSeriesRef(index, field, `'${selection.sheet.replace(/'/g, "''")}'!${rangeA1(selection.range)}`);
  }

  addSeriesFromSelection(): void {
    const selection = this.grid().selection();
    const id = this.selectedId();
    if (!selection || !id) return this.message.set('Select the cells for the new series first.');
    this.message.set(null);
    this.session().update(id, spec => ({
      ...spec,
      series: [
        ...spec.series,
        {
          sourceIndex: null,
          nameLiteral: `Series ${spec.series.length + 1}`,
          nameRef: null,
          categories: spec.series[0]?.categories ?? null,
          values: seriesRef({ sheet: selection.sheet, range: selection.range }),
          color: null,
          cached: { name: '', labels: [], values: [] },
        },
      ],
    }));
  }

  removeSeries(index: number): void {
    const id = this.selectedId();
    if (id) this.session().update(id, spec => (spec.series.length > 1 ? { ...spec, series: spec.series.filter((_, i) => i !== index) } : spec));
  }

  /** How the panel shows a reference it can read, or could not. */
  refText(ref: ChartSeriesSpec['values']): string {
    return ref?.formula ?? '';
  }

  /** A colour input needs a value; a series with none takes the theme's. */
  colorOf(series: ChartSeriesSpec): string {
    return series.color ?? '#4472C4';
  }
}
