import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';

import { CheckboxComponent } from '../../../../shared/ui/checkbox/checkbox.component';
import { rangeA1 } from '../../shared/a1';
import { SheetGrid } from '../sheet-grid';
import { AGGREGATES, Aggregate, PivotConfig, valueCaption } from './pivot-engine';
import { ApplyOutcome, PivotEntry, PivotWorkspace, parseCell } from './pivot-workspace';

type Area = 'none' | 'rows' | 'columns' | 'values' | 'filters';

/** Build and change pivot tables: a list of them, then fields assigned to areas, as in Excel's field list. */
@Component({
  selector: 'app-sheet-pivots-panel',
  standalone: true,
  imports: [CheckboxComponent],
  templateUrl: './sheet-pivots-panel.component.html',
  styleUrl: './sheet-pivots-panel.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SheetPivotsPanelComponent {
  readonly workspace = input.required<PivotWorkspace>();
  readonly grid = input.required<SheetGrid>();
  readonly changed = output<void>();

  readonly aggregates = AGGREGATES;
  readonly areas: readonly { id: Area; label: string }[] = [
    { id: 'none', label: 'Not used' },
    { id: 'rows', label: 'Rows' },
    { id: 'columns', label: 'Columns' },
    { id: 'values', label: 'Values' },
    { id: 'filters', label: 'Filter' },
  ];
  readonly selectedId = signal<string | null>(null);
  readonly filterField = signal<string | null>(null);
  readonly creating = signal(false);
  readonly message = signal<string | null>(null);
  /** A layout that would overwrite cells, waiting for the reader's answer. */
  readonly pending = signal<{ id: string; config: PivotConfig; where: string } | null>(null);
  readonly sheets = computed(() => this.grid().sheetNames());
  readonly newSheet = signal('');
  readonly newCell = signal('');

  readonly selected = computed(() => {
    const list = this.workspace().entries();
    return list.find(e => e.id === this.selectedId()) ?? (list.length === 1 ? list[0] : null);
  });
  readonly fields = computed(() => {
    const entry = this.selected();
    return entry ? this.workspace().fieldsOf(entry) : [];
  });
  readonly filterItems = computed(() => {
    const entry = this.selected();
    const field = this.filterField();
    if (!entry || !field) return [];
    const hidden = new Set(entry.config.hidden[field] ?? []);
    return this.workspace().itemsOf(entry, field).map(label => ({ label, shown: !hidden.has(label) }));
  });

  areaOf(entry: PivotEntry, field: string): Area {
    const c = entry.config;
    if (c.rows.includes(field)) return 'rows';
    if (c.columns.includes(field)) return 'columns';
    if (c.filters.includes(field)) return 'filters';
    if (c.values.some(v => v.field === field)) return 'values';
    return 'none';
  }

  caption = valueCaption;

  setArea(entry: PivotEntry, field: string, area: string): void {
    const c = entry.config;
    const next: PivotConfig = {
      ...c,
      rows: c.rows.filter(f => f !== field),
      columns: c.columns.filter(f => f !== field),
      filters: c.filters.filter(f => f !== field),
      values: c.values.filter(v => v.field !== field),
    };
    if (area === 'rows') next.rows.push(field);
    else if (area === 'columns') next.columns.push(field);
    else if (area === 'filters') next.filters.push(field);
    else if (area === 'values') next.values.push({ field, aggregate: this.defaultAggregate(entry, field) });
    this.applyConfig(entry.id, next);
  }

  /** Numbers sum and anything else counts, as Excel chooses. */
  private defaultAggregate(entry: PivotEntry, field: string): Aggregate {
    const table = this.workspace().sourceTable(entry.cachePath);
    const column = table?.[0].indexOf(field) ?? -1;
    if (!table || column < 0) return 'sum';
    return table.slice(1).every(r => r[column] === null || typeof r[column] === 'number') ? 'sum' : 'count';
  }

  setAggregate(entry: PivotEntry, index: number, aggregate: string): void {
    const values = entry.config.values.map((v, i) => {
      if (i !== index) return v;
      const { name: _name, ...rest } = v;
      return { ...rest, aggregate: aggregate as Aggregate };
    });
    this.applyConfig(entry.id, { ...entry.config, values });
  }

  toggleItem(entry: PivotEntry, field: string, label: string, shown: boolean): void {
    const current = new Set(entry.config.hidden[field] ?? []);
    if (shown) current.delete(label);
    else current.add(label);
    const hidden = { ...entry.config.hidden };
    if (current.size) hidden[field] = [...current];
    else delete hidden[field];
    this.applyConfig(entry.id, { ...entry.config, hidden });
  }

  setGrand(entry: PivotEntry, which: 'rowGrandTotals' | 'colGrandTotals', value: boolean): void {
    this.applyConfig(entry.id, { ...entry.config, [which]: value });
  }

  applyConfig(id: string, config: PivotConfig): void {
    this.report(id, config, this.workspace().apply(id, config));
  }

  confirmOverwrite(): void {
    const pending = this.pending();
    if (!pending) return;
    this.pending.set(null);
    this.report(pending.id, pending.config, this.workspace().apply(pending.id, pending.config, { force: true }));
  }

  cancelOverwrite(): void {
    this.pending.set(null);
  }

  private report(id: string, config: PivotConfig, outcome: ApplyOutcome): void {
    this.message.set(null);
    if (outcome.kind === 'conflict') this.pending.set({ id, config, where: outcome.where });
    else if (outcome.kind === 'error') this.message.set(outcome.message);
    else this.changed.emit();
  }

  refreshAll(): void {
    this.workspace().refreshAll();
    this.changed.emit();
  }

  refresh(entry: PivotEntry): void {
    this.report(entry.id, entry.config, this.workspace().refresh(entry.id));
  }

  remove(entry: PivotEntry): void {
    this.workspace().remove(entry.id);
    this.selectedId.set(null);
    this.changed.emit();
  }

  startCreate(): void {
    const selection = this.grid().selection();
    this.newSheet.set(selection?.sheet ?? this.grid().activeSheetName() ?? '');
    if (selection) this.newCell.set(rangeA1({ startRow: selection.range.startRow, startColumn: selection.range.endColumn + 2, endRow: selection.range.startRow, endColumn: selection.range.endColumn + 2 }));
    this.message.set(selection ? null : 'Select the source cells, with their header row, first.');
    this.creating.set(true);
  }

  create(): void {
    const selection = this.grid().selection();
    if (!selection) {
      this.message.set('Select the source cells, with their header row, first.');
      return;
    }
    let sheet = this.newSheet();
    let cell = parseCell(this.newCell());
    if (sheet === '__new__') {
      sheet = this.freeSheetName();
      this.grid().addSheet(sheet);
      cell = { row: 2, column: 0 };
    }
    if (!cell) {
      this.message.set('Enter a cell such as H3 for where the pivot table goes.');
      return;
    }
    const made = this.workspace().create(selection, { sheet, row: cell.row, column: cell.column });
    if ('error' in made) {
      this.message.set(made.error);
      return;
    }
    this.creating.set(false);
    this.message.set(null);
    this.selectedId.set(made.entry.id);
    this.changed.emit();
  }

  private freeSheetName(): string {
    const names = new Set(this.grid().sheetNames());
    let n = 1;
    while (names.has(`Pivot${n}`)) n++;
    return `Pivot${n}`;
  }
}
