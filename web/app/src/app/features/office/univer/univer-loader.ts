import { InjectionToken } from '@angular/core';

/**
 * Loads Univer on demand (ADR 0x043, office suite): the presets for sheets or docs, their
 * English strings, and their stylesheets, which are built as lazy global styles (angular.json
 * `styles` entries with `inject: false`) and linked once, as Monaco's is. The result is what a
 * component needs to create an instance; tests provide a fake through the tokens.
 */
export type UniverPresets = typeof import('@univerjs/presets');
export type FUniver = import('@univerjs/presets').FUniver;
export type Univer = import('@univerjs/presets').Univer;

export interface UniverSheetsKit {
  createUniver: UniverPresets['createUniver'];
  /**
   * createUniver options ready for a container element: locale, strings, theme and presets. With
   * a `formulaWorker`, formulas are calculated there instead of on the main thread.
   */
  options(
    container: HTMLElement,
    opts?: { readOnly?: boolean; formulaWorker?: Worker | null },
  ): Parameters<UniverPresets['createUniver']>[0];
  /** A formula worker for one instance (null where workers are unavailable); the caller terminates it. */
  createFormulaWorker(): Worker | null;
  /**
   * Paints a picture's new source now. Univer stores a changed source but its image shape only reads
   * it when the sheet is next rendered from scratch, so a redrawn chart would look stale until then.
   */
  repaintDrawing(univer: Univer, drawing: { unitId: string; subUnitId: string; drawingId: string }, source: string): void;
  /** Adds items to the grid's right-click menu; dispose removes their commands. */
  addContextMenuItems(univer: Univer, items: readonly DocMenuItem[]): { dispose(): void };
  /** The comment cells of a workbook (ADR 0x045). */
  commentCells(univer: Univer, univerAPI: FUniver, unitId: string): SheetCommentCells;
}

/** Cells tagged with comment threads (xlsx/sheet-comments.ts), driven by the editor's threads. */
export interface SheetCommentCells {
  /** Marks the active thread's cell more strongly and leaves hidden (resolved) threads' cells unmarked. */
  paint(state: { activeId: string | null; hidden: ReadonlySet<string> }): void;
  /** Tags a cell with a thread (or clears it with null). */
  tag(sheetId: string, row: number, col: number, threadId: string | null): Promise<boolean>;
  /** Where each thread's cell is now, by thread id: the first cell tagged with it. */
  locate(): Map<string, { sheetId: string; row: number; col: number }>;
  /** The thread a cell is tagged with. */
  at(sheetId: string, row: number, col: number): string | null;
  /** Shows a cell: its sheet, selected and scrolled to. */
  reveal(sheetId: string, row: number, col: number): void;
  /** The selected cell (the top-left of the selection). */
  activeCell(): { sheetId: string; row: number; col: number } | null;
  activeSheetId(): string | null;
  dispose(): void;
}

/** The parts of Univer's sheets facade the comment cells use. */
interface SheetFacadeWorkbook {
  getSheetBySheetId(id: string): {
    getSheetId(): string;
    activate(): unknown;
    getRange(row: number, col: number): { activate(): unknown };
  } | null;
  getActiveSheet(): { getSheetId(): string };
  getActiveRange(): { getRow(): number; getColumn(): number } | null;
}

export interface UniverDocsKit {
  createUniver: UniverPresets['createUniver'];
  options(container: HTMLElement, opts?: { readOnly?: boolean }): Parameters<UniverPresets['createUniver']>[0];
  UniverInstanceType: UniverPresets['UniverInstanceType'];
  /**
   * Shows or hides the editing guides: margin corners and lines for table sides that have no border.
   * Paint only, so the document and its layout do not change. False when the page is not rendered yet.
   */
  setDocGuides(univer: Univer, unitId: string, show: boolean): boolean;
  /** The comment marks of a document (ADR 0x045); null until its page is rendered. */
  commentMarks(univer: Univer, unitId: string): DocCommentMarks | null;
  /** Adds items to the page's right-click menu; dispose removes their commands. */
  addContextMenuItems(univer: Univer, items: readonly DocMenuItem[]): { dispose(): void };
}

/** An item of the document's right-click menu. */
export interface DocMenuItem {
  id: string;
  title: string;
  icon: 'new' | 'reply' | 'delete';
  order: number;
  /** Whether the item is left out, as it changes. */
  hidden$: import('rxjs').Observable<boolean>;
  run(): void;
}

/** Univer's comment decorations over a document's text, driven by the editor's own comment threads. */
export interface DocCommentMarks {
  /** Paints the active thread's text with a background and leaves hidden (resolved) threads unmarked. */
  paint(state: { activeId: string | null; hidden: ReadonlySet<string> }): void;
  /** Marks [start, end) of a story ('' is the body) as thread `id`'s text. */
  add(id: string, segmentId: string, start: number, end: number): Promise<boolean>;
  remove(id: string, segmentId: string): Promise<boolean>;
  /** Scrolls the thread's text into view. */
  reveal(id: string): void;
  /** Where each thread still marked in the body starts; deleting a thread's text removes its mark. */
  positions(): Map<string, number>;
  /** The thread marked at a body offset, if any. */
  at(offset: number): string | null;
  /** A story's text over [start, end), for quoting. */
  text(segmentId: string, start: number, end: number): string;
  /** The word at an offset, as [start, end); null where there is none (a space, a paragraph end). */
  wordAt(segmentId: string, offset: number): { start: number; end: number } | null;
  dispose(): void;
}

export const UNIVER_SHEETS_STYLESHEET = 'univer-sheets.css';
export const UNIVER_DOCS_STYLESHEET = 'univer-docs.css';

function linkStylesheet(document: Document, href: string, marker: string): Promise<void> {
  if (document.querySelector(`link[data-${marker}]`)) return Promise.resolve();
  return new Promise<void>(resolve => {
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = href;
    link.dataset[marker] = '1';
    link.onload = () => resolve();
    link.onerror = () => resolve(); // the editor still works, unstyled, and the console says why
    document.head.appendChild(link);
  });
}

/** The formula engine runs in a worker (univer-formula.worker.ts), one per sheets instance. */
export function createFormulaWorker(): Worker | null {
  if (typeof Worker === 'undefined') return null;
  try {
    return new Worker(new URL('./univer-formula.worker', import.meta.url), { type: 'module' });
  } catch (error) {
    console.warn('Univer formula worker unavailable; formulas run on the main thread', error);
    return null;
  }
}

const DOC_MAIN_COMPONENT = '__Document_Render_Main__';
/** What adding menu items needs of a preset's exports (docs and sheets core presets both have it). */
type MenuModule = Pick<
  typeof import('@univerjs/preset-docs-core'),
  'ComponentManager' | 'ContextMenuGroup' | 'ContextMenuPosition' | 'IMenuManagerService' | 'IconManager' | 'MenuItemType'
>;

/** Adds comment items to the main area's right-click menu, first in the list under the quick actions, as Word puts them. */
function addMenuItems(
  univer: Univer,
  presets: UniverPresets,
  core: MenuModule,
  icons: typeof import('@univerjs/icons'),
  items: readonly DocMenuItem[],
): { dispose(): void } {
  const injector = univer.__getInjector();
  const commands = injector.get(presets.ICommandService);
  const iconManager = injector.get(core.IconManager);
  const disposers = [
    ...Object.entries(COMMENT_MENU_ICONS).map(([name, key]) => iconManager.register(name, icons[key])),
    ...items.map(item => commands.registerCommand({ id: item.id, type: presets.CommandType.OPERATION, handler: () => (item.run(), true) })),
  ];
  const iconName = (icon: DocMenuItem['icon']) => Object.keys(COMMENT_MENU_ICONS)[['new', 'reply', 'delete'].indexOf(icon)];
  injector.get(core.IMenuManagerService).mergeMenu({
    [core.ContextMenuPosition.MAIN_AREA]: {
      [core.ContextMenuGroup.FORMAT]: Object.fromEntries(
        items.map(item => [
          item.id,
          {
            order: item.order - 10,
            menuItemFactory: () => ({
              id: item.id,
              type: core.MenuItemType.BUTTON,
              title: item.title,
              icon: iconName(item.icon),
              hidden$: item.hidden$,
            }),
          },
        ]),
      ),
    },
  });
  return { dispose: () => disposers.forEach(d => d.dispose()) };
}

/** The comment menu items' icons, registered under names of our own. */
const COMMENT_MENU_ICONS = {
  WhatIffCommentNewIcon: 'InsertCommentDoubleIcon',
  WhatIffCommentReplyIcon: 'ReplyToCommentIcon',
  WhatIffCommentDeleteIcon: 'DeleteIcon',
} as const;
/** The custom-data key a commented cell holds its thread's id under (xlsx/sheet-comments.ts COMMENT_CELL_KEY). */
const SHEET_COMMENT_KEY = 'whatiffCommentThread';
/** SetRangeValuesMutation. */
const SHEET_SET_RANGE_VALUES_MUTATION = 'sheet.mutation.set-range-values';
/** RichTextEditingMutation: the mutation every document change goes through. */
const DOC_RICH_TEXT_MUTATION = 'doc.mutation.rich-text-editing';
const DOC_BACKGROUND_COMPONENT = '__Document_Render_Background__';
/** A table side with no border, while guides are showing: dashed and light, as Word's gridlines are. */
const DOC_GUIDE_LINE = { color: { rgb: '#B8C0CC' }, width: { v: 0.75 }, dashStyle: 3 };

interface DocRenderComponent {
  makeDirty(dirty: boolean): void;
  _marginStrokeColor?: string;
  _resolveTableCellBorder?(primary: unknown, secondary: unknown): unknown;
  /** Univer's own method, kept while it is wrapped. */
  __resolveTableCellBorder?(primary: unknown, secondary: unknown): unknown;
}

let sheetsKit: Promise<UniverSheetsKit> | null = null;
let docsKit: Promise<UniverDocsKit> | null = null;

export function loadUniverSheets(document: Document): Promise<UniverSheetsKit> {
  sheetsKit ??= (async () => {
    const [
      presets,
      core,
      dv,
      cf,
      filter,
      link,
      drawing,
      sort,
      find,
      note,
      icons,
      enUS,
      dvUS,
      cfUS,
      filterUS,
      linkUS,
      drawingUS,
      sortUS,
      findUS,
      noteUS,
    ] = await Promise.all([
      import('@univerjs/presets'),
      import('@univerjs/preset-sheets-core'),
      import('@univerjs/preset-sheets-data-validation'),
      import('@univerjs/preset-sheets-conditional-formatting'),
      import('@univerjs/preset-sheets-filter'),
      import('@univerjs/preset-sheets-hyper-link'),
      import('@univerjs/preset-sheets-drawing'),
      import('@univerjs/preset-sheets-sort'),
      import('@univerjs/preset-sheets-find-replace'),
      import('@univerjs/preset-sheets-note'),
      import('@univerjs/icons'),
      import('@univerjs/preset-sheets-core/locales/en-US'),
      import('@univerjs/preset-sheets-data-validation/locales/en-US'),
      import('@univerjs/preset-sheets-conditional-formatting/locales/en-US'),
      import('@univerjs/preset-sheets-filter/locales/en-US'),
      import('@univerjs/preset-sheets-hyper-link/locales/en-US'),
      import('@univerjs/preset-sheets-drawing/locales/en-US'),
      import('@univerjs/preset-sheets-sort/locales/en-US'),
      import('@univerjs/preset-sheets-find-replace/locales/en-US'),
      import('@univerjs/preset-sheets-note/locales/en-US'),
      linkStylesheet(document, UNIVER_SHEETS_STYLESHEET, 'univerSheets'),
    ]);
    const strings = presets.merge(
      {},
      enUS.default,
      dvUS.default,
      cfUS.default,
      filterUS.default,
      linkUS.default,
      drawingUS.default,
      sortUS.default,
      findUS.default,
      noteUS.default,
    );
    return {
      createUniver: presets.createUniver,
      createFormulaWorker,
      addContextMenuItems: (univer, items) => addMenuItems(univer, presets, core as unknown as MenuModule, icons, items),
      commentCells: (univer, univerAPI, unitId) => {
        const injector = univer.__getInjector();
        const render = injector.get(core.IRenderManagerService).getRenderUnitById(unitId);
        const workbook = () => injector.get(presets.IUniverInstanceService).getUnit<import('@univerjs/presets').Workbook>(unitId);
        let state: { activeId: string | null; hidden: ReadonlySet<string> } = { activeId: null, hidden: new Set() };
        const idOf = (cell: unknown) => {
          const id = (cell as { custom?: Record<string, unknown> | null } | null | undefined)?.custom?.[SHEET_COMMENT_KEY];
          return typeof id === 'string' ? id : null;
        };
        // A commented cell gets the corner marker Excel draws, stronger while its thread is active.
        const interceptor = injector.get(core.SheetInterceptorService).intercept(core.INTERCEPTOR_POINT.CELL_CONTENT, {
          effect: presets.InterceptorEffectEnum.Style,
          priority: 100,
          handler: (cell, pos, next) => {
            const id = pos.unitId === unitId ? idOf(pos.rawData) : null;
            if (!id || state.hidden.has(id)) return next(cell);
            const marked: { markers?: Record<string, unknown> } & Record<string, unknown> = {
              ...(cell && cell !== pos.rawData ? cell : pos.rawData),
            };
            const active = id === state.activeId;
            marked.markers = { ...marked.markers, tr: { color: active ? '#E8A400' : '#FFBD37', size: active ? 9 : 6 } };
            return next(marked);
          },
        });
        const repaint = () => injector.get(core.IRenderManagerService).getRenderUnitById(unitId)?.mainComponent?.makeForceDirty?.(true);
        const fWorkbook = () => (univerAPI as unknown as { getWorkbook(id: string): SheetFacadeWorkbook | null }).getWorkbook(unitId);
        void render;
        return {
          paint: next => {
            state = next;
            repaint();
          },
          tag: async (sheetId, row, col, threadId) => {
            const sheet = workbook()?.getSheetBySheetId(sheetId);
            if (!sheet) return false;
            const custom = { ...(sheet.getCellRaw(row, col)?.custom ?? {}), [SHEET_COMMENT_KEY]: threadId };
            // A mutation, not a command: comments are written while viewing, when the sheet is locked.
            const done = await injector.get(presets.ICommandService).executeCommand(SHEET_SET_RANGE_VALUES_MUTATION, {
              unitId,
              subUnitId: sheetId,
              cellValue: { [row]: { [col]: { custom } } },
            });
            repaint();
            return done;
          },
          locate: () => {
            const out = new Map<string, { sheetId: string; row: number; col: number }>();
            for (const sheet of workbook()?.getSheets() ?? []) {
              sheet.getCellMatrix().forValue((row, col, cell) => {
                const id = idOf(cell);
                if (id && !out.has(id)) out.set(id, { sheetId: sheet.getSheetId(), row, col });
              });
            }
            return out;
          },
          at: (sheetId, row, col) => idOf(workbook()?.getSheetBySheetId(sheetId)?.getCellRaw(row, col)),
          reveal: (sheetId, row, col) => {
            const book = fWorkbook();
            const sheet = book?.getSheetBySheetId(sheetId);
            if (!sheet) return;
            if (book!.getActiveSheet().getSheetId() !== sheetId) sheet.activate();
            sheet.getRange(row, col).activate();
          },
          activeCell: () => {
            const book = fWorkbook();
            const sheet = book?.getActiveSheet();
            const range = book?.getActiveRange();
            return sheet && range ? { sheetId: sheet.getSheetId(), row: range.getRow(), col: range.getColumn() } : null;
          },
          activeSheetId: () => fWorkbook()?.getActiveSheet().getSheetId() ?? null,
          dispose: () => interceptor.dispose(),
        };
      },
      repaintDrawing: (univer, { unitId, subUnitId, drawingId }, source) => {
        const scene = univer.__getInjector().get(core.IRenderManagerService).getRenderUnitById(unitId)?.scene;
        const shape = scene?.getObjectIncludeInGroup(`${unitId}#-#${subUnitId}#-#${drawingId}`) as
          { changeSource?(url: string): void } | undefined;
        shape?.changeSource?.(source);
      },
      options: (container, opts) => ({
        locale: presets.LocaleType.EN_US,
        locales: { [presets.LocaleType.EN_US]: strings },
        theme: presets.defaultTheme,
        presets: [
          core.UniverSheetsCorePreset({
            container,
            header: true,
            // Read-only keeps the sheet tabs and zoom so a reader can move around, and drops the rest.
            footer: opts?.readOnly
              ? { sheetBar: true, statisticBar: false, menus: false, zoomSlider: true, addSheetButtonConfig: { show: false } }
              : undefined,
            formulaBar: !opts?.readOnly,
            toolbar: !opts?.readOnly,
            contextMenu: !opts?.readOnly,
            ...(opts?.formulaWorker ? { workerURL: opts.formulaWorker } : {}),
          }),
          dv.UniverSheetsDataValidationPreset(),
          cf.UniverSheetsConditionalFormattingPreset(),
          filter.UniverSheetsFilterPreset(),
          link.UniverSheetsHyperLinkPreset(),
          drawing.UniverSheetsDrawingPreset(),
          sort.UniverSheetsSortPreset(),
          find.UniverSheetsFindReplacePreset(),
          note.UniverSheetsNotePreset(),
        ],
      }),
    };
  })();
  return sheetsKit;
}

export function loadUniverDocs(document: Document): Promise<UniverDocsKit> {
  docsKit ??= (async () => {
    const [presets, core, link, drawing, enUS, linkUS, drawingUS, icons] = await Promise.all([
      import('@univerjs/presets'),
      import('@univerjs/preset-docs-core'),
      import('@univerjs/preset-docs-hyper-link'),
      import('@univerjs/preset-docs-drawing'),
      import('@univerjs/preset-docs-core/locales/en-US'),
      import('@univerjs/preset-docs-hyper-link/locales/en-US'),
      import('@univerjs/preset-docs-drawing/locales/en-US'),
      import('@univerjs/icons'),
      linkStylesheet(document, UNIVER_DOCS_STYLESHEET, 'univerDocs'),
    ]);
    const strings = presets.merge({}, enUS.default, linkUS.default, drawingUS.default);
    return {
      createUniver: presets.createUniver,
      UniverInstanceType: presets.UniverInstanceType,
      setDocGuides: (univer, unitId, show) => {
        const injector = univer.__getInjector();
        const components = injector.get(core.IRenderManagerService).getRenderUnitById(unitId)?.components as
          Map<string, DocRenderComponent> | undefined;
        const main = components?.get(DOC_MAIN_COMPONENT);
        const background = components?.get(DOC_BACKGROUND_COMPONENT);
        if (!main || !background) return false;
        // Univer has no option for either, so this reaches two fields of the pinned version.
        const resolve = main.__resolveTableCellBorder ?? main._resolveTableCellBorder!;
        main.__resolveTableCellBorder = resolve;
        main._resolveTableCellBorder = show ? (primary, secondary) => resolve.call(main, primary, secondary) ?? DOC_GUIDE_LINE : resolve;
        background._marginStrokeColor = show ? undefined : 'transparent';
        main.makeDirty(true);
        background.makeDirty(true);
        return true;
      },
      commentMarks: (univer, unitId) => {
        const injector = univer.__getInjector();
        const unit = injector.get(core.IRenderManagerService).getRenderUnitById(unitId);
        if (!unit?.mainComponent) return null;
        const commands = injector.get(presets.ICommandService);
        const document = () => injector.get(presets.IUniverInstanceService).getUnit<import('@univerjs/presets').DocumentDataModel>(unitId);
        const COMMENT = presets.CustomDecorationType.COMMENT;
        let state: { activeId: string | null; hidden: ReadonlySet<string> } = { activeId: null, hidden: new Set() };
        const interceptor = unit.with(core.DocInterceptorService).intercept(core.DOC_INTERCEPTOR_POINT.CUSTOM_DECORATION, {
          handler: (data, _pos, next) =>
            data && data.type === COMMENT
              ? next({ ...data, active: data.id === state.activeId, show: !state.hidden.has(data.id) })
              : next(data),
        });
        const edit = async (segmentId: string, textX: InstanceType<UniverPresets['TextX']>) => {
          // A viewer's document is disabled, which makes every edit a no-op; a comment mark is the one
          // change a viewer makes, so the document is enabled for just that.
          const model = document();
          const disabled = !!model?.getDisabled();
          if (disabled) model!.setDisabled(false);
          try {
            return await commands.executeCommand(DOC_RICH_TEXT_MUTATION, {
              unitId,
              segmentId,
              actions: presets.JSONX.getInstance().editOp(textX.serialize()),
              textRanges: null,
              noNeedSetTextRange: true,
            });
          } finally {
            if (disabled) model!.setDisabled(true);
          }
        };
        const decorations = () => document()?.getBody()?.customDecorations ?? [];
        return {
          paint: next => {
            state = next;
            // The comment style is applied as the page is laid out, so a change needs a new layout.
            unit.with(core.DocSkeletonManagerService).recalculate();
            unit.mainComponent?.makeDirty(true);
          },
          add: (id, segmentId, start, end) =>
            edit(
              segmentId,
              presets.BuildTextUtils.customDecoration.add({
                ranges: [{ startOffset: start, endOffset: end, collapsed: false }],
                id,
                type: COMMENT,
              }),
            ),
          remove: async (id, segmentId) => {
            const documentDataModel = document();
            const textX = documentDataModel && presets.BuildTextUtils.customDecoration.delete({ id, segmentId, documentDataModel });
            return textX ? edit(segmentId, textX) : false;
          },
          reveal: id => {
            const mark = decorations().find(d => d.id === id);
            if (mark)
              unit
                .with(core.DocBackScrollRenderController)
                .scrollToRange({ startOffset: mark.startIndex, endOffset: mark.endIndex + 1, collapsed: false });
          },
          positions: () =>
            new Map(
              decorations()
                .filter(d => d.type === COMMENT)
                .map(d => [d.id, d.startIndex]),
            ),
          at: offset => {
            // The innermost mark wins where comments overlap, as Univer's own comment UI picks.
            const hits = decorations().filter(d => d.type === COMMENT && d.startIndex <= offset && offset <= d.endIndex);
            hits.sort((a, b) => a.endIndex - a.startIndex - (b.endIndex - b.startIndex));
            return hits[0]?.id ?? null;
          },
          wordAt: (segmentId, offset) => {
            const model = document();
            const stream = (segmentId ? model?.getSelfOrHeaderFooterModel(segmentId)?.getBody() : model?.getBody())?.dataStream ?? '';
            const word = /[\p{L}\p{N}_'’-]/u;
            // The caret sits between characters: the word is the one it is in or just after.
            let at = word.test(stream[offset] ?? '') ? offset : word.test(stream[offset - 1] ?? '') ? offset - 1 : -1;
            if (at < 0) return null;
            let start = at;
            while (start > 0 && word.test(stream[start - 1])) start--;
            while (at < stream.length && word.test(stream[at])) at++;
            return { start, end: at };
          },
          text: (segmentId, start, end) => {
            const model = document();
            const body = segmentId ? model?.getSelfOrHeaderFooterModel(segmentId)?.getBody() : model?.getBody();
            return body?.dataStream.slice(start, end) ?? '';
          },
          dispose: () => interceptor.dispose(),
        };
      },
      addContextMenuItems: (univer, items) => addMenuItems(univer, presets, core, icons, items),
      options: (container, opts) => ({
        locale: presets.LocaleType.EN_US,
        locales: { [presets.LocaleType.EN_US]: strings },
        theme: presets.defaultTheme,
        presets: [
          core.UniverDocsCorePreset({ container, header: true, toolbar: !opts?.readOnly, contextMenu: !opts?.readOnly }),
          link.UniverDocsHyperLinkPreset(),
          drawing.UniverDocsDrawingPreset(),
        ],
      }),
    };
  })();
  return docsKit;
}

/** For tests: forget loaded kits. */
export function resetUniverLoaders(): void {
  sheetsKit = null;
  docsKit = null;
}

export const UNIVER_SHEETS_LOADER = new InjectionToken<typeof loadUniverSheets>('UNIVER_SHEETS_LOADER', {
  providedIn: 'root',
  factory: () => loadUniverSheets,
});
export const UNIVER_DOCS_LOADER = new InjectionToken<typeof loadUniverDocs>('UNIVER_DOCS_LOADER', {
  providedIn: 'root',
  factory: () => loadUniverDocs,
});
