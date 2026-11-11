import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, inject, signal } from '@angular/core';
import { BehaviorSubject, map } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { csvDelimiterFor, previewKindFor } from '../../gallery/helpers/file-preview.helpers';
import { COMMENT_SAVER } from '../../file-workspace/editors/file-editor';
import { CommentAutosave } from '../comments/comment-autosave';
import { CommentPeopleService } from '../comments/comment-people.service';
import { CommentsPanelComponent } from '../comments/comments-panel.component';
import { CommentsPanelState } from '../comments/comments-panel-state.service';
import { putSheetComments, takeSheetComments } from '../xlsx/sheet-comments';
import { SheetCommentsState } from './sheet-comments-state';
import { FidelityReport } from '../shared/fidelity';
import { FidelityNoticeComponent } from '../shared/fidelity-notice.component';
import { MUTATION, UniverEditorBase, UniverKit, UniverUnit } from '../univer/univer-editor-base';
import { FUniver, SheetCommentCells, UNIVER_SHEETS_LOADER, UniverSheetsKit } from '../univer/univer-loader';
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

const SELECTION_OPERATION = 'sheet.operation.set-selections';
const WORKSHEET_ACTIVE_OPERATION = 'sheet.operation.set-worksheet-active';

interface SheetCommandEvent {
  id?: string;
  type?: number;
  params?: {
    unitId?: string;
    subUnitId?: string;
    selections?: { range?: { startRow: number; startColumn: number }; primary?: { actualRow: number; actualColumn: number } | null }[];
  };
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
  imports: [
    CommentsPanelComponent,
    SheetChartsPanelComponent,
    SheetPivotsPanelComponent,
    SheetSlicersPanelComponent,
    FidelityNoticeComponent,
  ],
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

  // --- comments (ADR 0x045) ------------------------------------------------------------------------
  readonly me = inject(CommentPeopleService).me;
  readonly comments = new SheetCommentsState(this.me, () => this.commentsChanged());
  private readonly autosave = new CommentAutosave(inject(COMMENT_SAVER, { optional: true }), this);
  /** Comments belong to .xlsx files: while editing, or while viewing a file the user may change. */
  readonly commentable = computed(() => !this.isCsv() && (!this.readOnly() || this.autosave.available()));
  readonly saveStatus = this.autosave.status;
  private readonly panelState = inject(CommentsPanelState);
  /** The panel opens by itself on a workbook with open threads, until the user opens or closes it. */
  readonly commentsOpen = computed(() => !this.isCsv() && (this.panelState.open() ?? this.comments.openCount() > 0));
  /** Opens a thread's reply box in the panel (the right-click menu's "Reply to comment"). */
  readonly replyRequest = signal<{ threadId: string } | null>(null);
  private commentCells: SheetCommentCells | null = null;
  private contextMenu: { dispose(): void } | null = null;
  /** Whether the selected cell has a thread, which decides the right-click menu's items. */
  private readonly cellOnComment = new BehaviorSubject(false);
  /** Set by a click or key press in the grid, so the selection that follows can pick a thread. */
  private selectionByUser = false;
  private refreshTimer: ReturnType<typeof setTimeout> | null = null;

  toggleComments(): void {
    this.panelState.open.set(!this.commentsOpen());
  }

  closeComments(): void {
    this.panelState.open.set(false);
  }

  /** Starts a thread on the selected cell (or opens the one it has). */
  startComment(): void {
    if (!this.commentable()) return;
    this.comments.start();
    this.panelState.open.set(true);
  }

  replyAtSelection(): void {
    const threadId = this.comments.atSelection();
    if (!threadId) return;
    this.comments.select(threadId);
    this.panelState.open.set(true);
    this.replyRequest.set({ threadId });
  }

  async deleteAtSelection(): Promise<void> {
    const threadId = this.comments.atSelection();
    if (threadId) await this.comments.remove({ threadId, commentId: threadId });
    this.cellOnComment.next(!!this.comments.atSelection());
  }

  /** A comment change: part of the edit while editing; saved straight away while viewing. */
  private commentsChanged(): void {
    this.setDirty(true);
    if (this.readOnly()) this.autosave.request();
  }

  /** Where threads are is read from the grid, once its changes settle. */
  private refreshCommentsSoon(): void {
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    this.refreshTimer = setTimeout(() => {
      this.refreshTimer = null;
      this.comments.refresh();
    }, 200);
  }

  markObjectsChanged(): void {
    this.setDirty(true);
  }
  private chartStart: { specs: ChartSpec[]; baseline: Record<string, UniverDrawingsForSheet>; theme: ExcelThemeColors } | null = null;

  constructor() {
    super();
    // The grid marks open threads' cells, the active one more strongly.
    effect(() => {
      const activeId = this.comments.activeId();
      const hidden = new Set(
        this.comments
          .threads()
          .filter(t => t.resolved && t.id !== activeId)
          .map(t => t.id),
      );
      this.comments.cells();
      this.commentCells?.paint({ activeId, hidden });
    });
    const host = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
    for (const type of ['pointerdown', 'keydown'] as const) {
      host.addEventListener(
        type,
        event => (this.selectionByUser = !!(event.target as Element | null)?.closest?.('.sheets-editor__stage')),
        { capture: true },
      );
    }
    host.addEventListener(
      'keydown',
      event => {
        if ((event.ctrlKey || event.metaKey) && event.altKey && event.key.toLowerCase() === 'm') {
          event.preventDefault();
          event.stopPropagation();
          this.startComment();
        }
      },
      { capture: true },
    );
  }

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
    this.contextMenu?.dispose();
    this.contextMenu = null;
    this.commentCells?.dispose();
    this.commentCells = null;
    this.comments.attach(null);
    if (this.refreshTimer) clearTimeout(this.refreshTimer);
    this.refreshTimer = null;
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
    this.comments.load(takeSheetComments(result.workbook, this.preserved?.rules?.threads.persons ?? []));
    if (this.preserved) {
      const theme = parseThemeColors(this.preserved.extras?.themes?.['theme1']);
      this.chartStart = {
        specs: chartSpecsFromPreserved(this.preserved, result.workbook, theme),
        baseline: drawingBaseline(result.workbook),
        theme,
      };
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
      const session = new ChartSession(specs, baseline, grid, new FacadeSheetDrawings(workbook as never, repaint), theme, () =>
        this.setDirty(true),
      );
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
    this.attachComments(univerAPI, data.id, readOnly);
    workbook.setEditable(!readOnly);
    // The unit's permission points register as it comes up, so the lock is set again once it has.
    if (readOnly) setTimeout(() => workbook.setEditable(false), 1200);
    return workbook;
  }

  private attachComments(univerAPI: FUniver, unitId: string, readOnly: boolean): void {
    if (this.isCsv() || !this.univer || !this.sheetsKit?.commentCells) return;
    this.commentCells = this.sheetsKit.commentCells(this.univer, univerAPI, unitId);
    this.comments.attach(this.commentCells);
    univerAPI.addEvent(univerAPI.Event.CommandExecuted, (event: SheetCommandEvent) => {
      if (event.params?.unitId !== unitId) return;
      if (event.id === SELECTION_OPERATION) {
        const selection = event.params.selections?.[0];
        const row = selection?.primary?.actualRow ?? selection?.range?.startRow;
        const col = selection?.primary?.actualColumn ?? selection?.range?.startColumn;
        if (row === undefined || col === undefined || !event.params.subUnitId) return;
        this.cellOnComment.next(!!this.comments.atSelection());
        // Univer also reports the selection when a sheet is shown or scrolled; only a click or key
        // press in the grid is the user choosing a cell.
        if (this.selectionByUser) {
          this.selectionByUser = false;
          this.comments.cellChosen({ sheetId: event.params.subUnitId, row, col });
        }
      } else if (event.id === WORKSHEET_ACTIVE_OPERATION || event.type === MUTATION) this.refreshCommentsSoon();
    });
    if (!readOnly && this.sheetsKit.addContextMenuItems) {
      const onComment$ = this.cellOnComment.asObservable();
      this.contextMenu = this.sheetsKit.addContextMenuItems(this.univer, [
        {
          id: 'whatiff.sheet.comment.new',
          title: 'New comment',
          icon: 'new',
          order: 0,
          hidden$: onComment$,
          run: () => this.startComment(),
        },
        {
          id: 'whatiff.sheet.comment.reply',
          title: 'Reply to comment',
          icon: 'reply',
          order: 0,
          hidden$: onComment$.pipe(map(on => !on)),
          run: () => this.replyAtSelection(),
        },
        {
          id: 'whatiff.sheet.comment.delete',
          title: 'Delete comment',
          icon: 'delete',
          order: 0.1,
          hidden$: onComment$.pipe(map(on => !on)),
          run: () => void this.deleteAtSelection(),
        },
      ]);
    }
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
    putSheetComments(data, this.comments.threads());
    const { bytes, report } = await workbookToXlsx(data, preserved);
    this.exportReport.set(report);
    // A workbook with macros stays macro-enabled; the file type says so to the host.
    return new Blob([bytes as BlobPart], { type: hasMacros(this.preserved) ? XLSM_MIME : XLSX_MIME });
  }
}
