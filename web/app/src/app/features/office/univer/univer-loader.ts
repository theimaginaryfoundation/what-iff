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
  options(container: HTMLElement, opts?: { readOnly?: boolean; formulaWorker?: Worker | null }): Parameters<UniverPresets['createUniver']>[0];
  /** A formula worker for one instance (null where workers are unavailable); the caller terminates it. */
  createFormulaWorker(): Worker | null;
}

export interface UniverDocsKit {
  createUniver: UniverPresets['createUniver'];
  options(container: HTMLElement, opts?: { readOnly?: boolean }): Parameters<UniverPresets['createUniver']>[0];
  UniverInstanceType: UniverPresets['UniverInstanceType'];
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
    const [presets, core, link, drawing, enUS, linkUS, drawingUS] = await Promise.all([
      import('@univerjs/presets'),
      import('@univerjs/preset-docs-core'),
      import('@univerjs/preset-docs-hyper-link'),
      import('@univerjs/preset-docs-drawing'),
      import('@univerjs/preset-docs-core/locales/en-US'),
      import('@univerjs/preset-docs-hyper-link/locales/en-US'),
      import('@univerjs/preset-docs-drawing/locales/en-US'),
      linkStylesheet(document, UNIVER_DOCS_STYLESHEET, 'univerDocs'),
    ]);
    const strings = presets.merge({}, enUS.default, linkUS.default, drawingUS.default);
    return {
      createUniver: presets.createUniver,
      UniverInstanceType: presets.UniverInstanceType,
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
