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
/** The comment menu items' icons, registered under names of our own. */
const COMMENT_MENU_ICONS = {
  WhatIffCommentNewIcon: 'InsertCommentDoubleIcon',
  WhatIffCommentReplyIcon: 'ReplyToCommentIcon',
  WhatIffCommentDeleteIcon: 'DeleteIcon',
} as const;
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
      threadComment,
      enUS,
      dvUS,
      cfUS,
      filterUS,
      linkUS,
      drawingUS,
      sortUS,
      findUS,
      noteUS,
      threadCommentUS,
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
      import('@univerjs/preset-sheets-thread-comment'),
      import('@univerjs/preset-sheets-core/locales/en-US'),
      import('@univerjs/preset-sheets-data-validation/locales/en-US'),
      import('@univerjs/preset-sheets-conditional-formatting/locales/en-US'),
      import('@univerjs/preset-sheets-filter/locales/en-US'),
      import('@univerjs/preset-sheets-hyper-link/locales/en-US'),
      import('@univerjs/preset-sheets-drawing/locales/en-US'),
      import('@univerjs/preset-sheets-sort/locales/en-US'),
      import('@univerjs/preset-sheets-find-replace/locales/en-US'),
      import('@univerjs/preset-sheets-note/locales/en-US'),
      import('@univerjs/preset-sheets-thread-comment/locales/en-US'),
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
      threadCommentUS.default,
    );
    return {
      createUniver: presets.createUniver,
      createFormulaWorker,
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
          threadComment.UniverSheetsThreadCommentPreset(),
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
        const edit = (segmentId: string, textX: InstanceType<UniverPresets['TextX']>) =>
          commands.executeCommand(DOC_RICH_TEXT_MUTATION, {
            unitId,
            segmentId,
            actions: presets.JSONX.getInstance().editOp(textX.serialize()),
            textRanges: null,
            noNeedSetTextRange: true,
          });
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
      addContextMenuItems: (univer, items) => {
        const injector = univer.__getInjector();
        const commands = injector.get(presets.ICommandService);
        const iconManager = injector.get(core.IconManager);
        const disposers = [
          ...Object.entries(COMMENT_MENU_ICONS).map(([name, key]) => iconManager.register(name, icons[key])),
          ...items.map(item =>
            commands.registerCommand({ id: item.id, type: presets.CommandType.OPERATION, handler: () => (item.run(), true) }),
          ),
        ];
        const iconName = (icon: DocMenuItem['icon']) => Object.keys(COMMENT_MENU_ICONS)[['new', 'reply', 'delete'].indexOf(icon)];
        // First in the list under the quick actions, as Word puts its comment items.
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
      },
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
