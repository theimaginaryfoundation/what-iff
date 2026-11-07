import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { csvDelimiterFor, previewKindFor } from '../../gallery/helpers/file-preview.helpers';
import { FidelityReport } from '../shared/fidelity';
import { FidelityNoticeComponent } from '../shared/fidelity-notice.component';
import { UniverEditorBase, UniverKit, UniverUnit } from '../univer/univer-editor-base';
import { FUniver, UNIVER_SHEETS_LOADER, UniverSheetsKit } from '../univer/univer-loader';
import { UniverWorkbook } from '../xlsx/univer-sheet-types';
import { sheetToCsv, workbookToXlsx } from '../xlsx/xlsx-export';
import { delimitedTextToWorkbook, xlsxToWorkbook } from '../xlsx/xlsx-import';
import { ChartSession } from './charts/chart-session';
import { ChartSpec } from './charts/chart-spec';
import { DrawingRepaint, FacadeSheetDrawings } from './charts/sheet-drawings';
import { SheetChartsPanelComponent } from './charts/sheet-charts-panel.component';
import { PivotWorkspace } from './pivots/pivot-workspace';
import { SheetPivotsPanelComponent } from './pivots/sheet-pivots-panel.component';
import { SheetSlicersPanelComponent } from './slicers/sheet-slicers-panel.component';
import { FacadeSlicerPictures } from './slicers/slicer-pictures';
import { SlicerWorkspace } from './slicers/slicer-workspace';
import { FacadeSheetGrid, SheetGrid } from './sheet-grid';
import { applyChartChanges, chartSpecsFromPreserved, drawingBaseline } from '../xlsx/xlsx-chart-save';
import { ExcelThemeColors, parseThemeColors } from '../xlsx/xlsx-theme';
import { UniverDrawingsForSheet } from '../xlsx/univer-sheet-types';
import { PreservedParts, XLSM_MIME, XLSX_MIME, hasMacros } from '../xlsx/xlsx-preserve';

export { MUTATION } from '../univer/univer-editor-base';

export type SheetPanel = 'charts' | 'pivots' | 'slicers';
export { XLSX_MIME } from '../xlsx/xlsx-preserve';

interface Workbook extends UniverUnit<UniverWorkbook> {
  setEditable(value: boolean): unknown;
}

const PANELS: readonly { id: SheetPanel; label: string }[] = [
  { id: 'charts', label: 'Charts' },
  { id: 'pivots', label: 'Pivot tables' },
  { id: 'slicers', label: 'Slicers' },
];

/**
 * The spreadsheet editor (ADR 0x043, office suite): Univer Sheets over the workbook our xlsx
 * converter reads. Saving hands back an .xlsx, or CSV for a CSV file.
 */
@Component({
  selector: 'app-sheets-editor',
  standalone: true,
  imports: [SheetChartsPanelComponent, SheetPivotsPanelComponent, SheetSlicersPanelComponent, FidelityNoticeComponent],
  templateUrl: './sheets-editor.component.html',
  styleUrls: ['../univer/univer-editor.component.scss', './sheets-editor.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SheetsEditorComponent extends UniverEditorBase<UniverWorkbook, Workbook> {
  private readonly load = inject(UNIVER_SHEETS_LOADER);
  readonly noun = 'spreadsheet';
  readonly isCsv = computed(() => previewKindFor(this.file()) === 'csv');
  /** What the open file holds beyond Univer's model (charts, pivots, macros, …), written back on save. */
  private preserved: PreservedParts | null = null;
  /** The formula engine of the open instance, off the main thread. */
  private formulaWorker: Worker | null = null;
  private sheetsKit: UniverSheetsKit | null = null;

  readonly panels = PANELS;
  readonly panel = signal<SheetPanel | null>(null);
  readonly panelLabel = computed(() => PANELS.find(p => p.id === this.panel())?.label ?? '');
  /** The open sheet, for the panels; null until a unit exists. */
  readonly grid = signal<SheetGrid | null>(null);
  /** Charts, pivots and slicers belong to .xlsx files, and are edited in edit mode. */
  readonly showObjectsBar = computed(() => !this.readOnly() && !this.isCsv() && this.state() === 'ready');

  /** The charts of the open workbook, in edit mode. */
  readonly charts = signal<ChartSession | null>(null);
  /** The open file's pivot tables and slicers, in edit mode. */
  readonly pivots = signal<PivotWorkspace | null>(null);
  readonly slicers = signal<SlicerWorkspace | null>(null);

  markObjectsChanged(): void {
    this.setDirty(true);
  }
  private chartStart: { specs: ChartSpec[]; baseline: Record<string, UniverDrawingsForSheet>; theme: ExcelThemeColors } | null = null;

  togglePanel(id: SheetPanel): void {
    this.panel.update(current => (current === id ? null : id));
  }

  protected async loadKit(): Promise<UniverKit> {
    const kit = await this.load(this.document);
    this.sheetsKit = kit;
    return kit;
  }

  protected override univerOptions(kit: UniverKit, container: HTMLElement, readOnly: boolean): ReturnType<UniverKit['options']> {
    const sheetsKit = kit as Partial<UniverSheetsKit>;
    this.formulaWorker = sheetsKit.createFormulaWorker?.() ?? null;
    return kit.options(container, { readOnly, formulaWorker: this.formulaWorker });
  }

  protected override onDispose(): void {
    this.grid.set(null);
    this.charts()?.dispose();
    this.charts.set(null);
    this.pivots()?.detach();
    this.slicers()?.detach();
    this.pivots.set(null);
    this.slicers.set(null);
    this.formulaWorker?.terminate();
    this.formulaWorker = null;
  }

  protected async parse(blob: Blob, file: FileAttachment): Promise<{ data: UniverWorkbook; report: FidelityReport }> {
    const id = `wb-${file.id}`;
    this.preserved = null;
    this.chartStart = null;
    if (previewKindFor(file) === 'csv') {
      const result = delimitedTextToWorkbook(await blob.text(), csvDelimiterFor(file), { id, name: file.name });
      return { data: result.workbook, report: result.report };
    }
    const result = await xlsxToWorkbook(await blob.arrayBuffer(), { id, name: file.name });
    this.preserved = result.preserved ?? null;
    if (this.preserved) {
      const theme = parseThemeColors(this.preserved.extras?.themes?.['theme1']);
      this.chartStart = { specs: chartSpecsFromPreserved(this.preserved, result.workbook, theme), baseline: drawingBaseline(result.workbook), theme };
    }
    return { data: result.workbook, report: result.report };
  }

  protected createUnit(univerAPI: FUniver, data: UniverWorkbook, readOnly: boolean): Workbook {
    const api = univerAPI as FUniver & { createWorkbook(data: unknown): Workbook };
    const workbook = api.createWorkbook(data);
    const grid = new FacadeSheetGrid(workbook as never, univerAPI as never);
    const repaint: DrawingRepaint = (drawing, source) => {
      if (this.univer) this.sheetsKit?.repaintDrawing?.(this.univer, drawing, source);
    };
    this.grid.set(grid);
    if (!readOnly && this.chartStart) {
      const { specs, baseline, theme } = this.chartStart;
      const session = new ChartSession(specs, baseline, grid, new FacadeSheetDrawings(workbook as never, repaint), theme, () => this.setDirty(true));
      session.start();
      this.charts.set(session);
    }
    if (!readOnly && this.preserved) {
      const pivots = new PivotWorkspace(this.preserved);
      pivots.attach(grid);
      const slicers = new SlicerWorkspace(this.preserved, pivots);
      slicers.attach(grid, new FacadeSlicerPictures(workbook as never, repaint));
      this.pivots.set(pivots);
      this.slicers.set(slicers);
    }
    workbook.setEditable(!readOnly);
    // The unit's permission points register as it comes up, so the lock is set again once it has.
    if (readOnly) setTimeout(() => workbook.setEditable(false), 1200);
    return workbook;
  }

  protected async serialize(data: UniverWorkbook, file: FileAttachment): Promise<Blob> {
    if (previewKindFor(file) === 'csv') {
      const first = data.sheets[data.sheetOrder[0]];
      const others = data.sheetOrder.length - 1;
      this.exportReport.set({
        dropped: others > 0 ? [{ feature: 'Sheets after the first (a CSV file holds one sheet)', count: others }] : [],
        warnings: [],
      });
      return new Blob([sheetToCsv(first, csvDelimiterFor(file))], { type: file.file_type || 'text/csv' });
    }
    this.pivots()?.flush();
    this.slicers()?.flush();
    const session = this.charts();
    const preserved = this.preserved && session ? applyChartChanges(this.preserved, data, session.saveState()) : this.preserved;
    const { bytes, report } = await workbookToXlsx(data, preserved);
    this.exportReport.set(report);
    // A workbook with macros stays macro-enabled; the file type says so to the host.
    return new Blob([bytes as BlobPart], { type: hasMacros(this.preserved) ? XLSM_MIME : XLSX_MIME });
  }
}
