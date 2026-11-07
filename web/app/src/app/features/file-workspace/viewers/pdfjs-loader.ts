import { InjectionToken } from '@angular/core';

export type PdfJs = typeof import('pdfjs-dist');

/** The stylesheet PDF.js's text layer needs, built as a lazy global style (angular.json). */
export const PDFJS_STYLESHEET = 'pdfjs.css';

let pdfjsPromise: Promise<PdfJs> | undefined;

/**
 * Loads PDF.js once, wires its worker and links its stylesheet. Mirrors loadKatex(): the library
 * stays out of the initial bundle and only arrives the first time a PDF is opened.
 */
export function loadPdfJs(document: Document): Promise<PdfJs> {
  pdfjsPromise ??= (async () => {
    if (!document.querySelector(`link[data-pdfjs]`)) {
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = PDFJS_STYLESHEET;
      link.dataset['pdfjs'] = '1';
      document.head.appendChild(link);
    }
    const pdfjs = await import('pdfjs-dist');
    pdfjs.GlobalWorkerOptions.workerPort = new Worker(new URL('./pdf.worker', import.meta.url), { type: 'module' });
    return pdfjs;
  })();
  return pdfjsPromise;
}

/** The loader, as a token so tests can hand the viewer a fake PDF.js. */
export const PDFJS_LOADER = new InjectionToken<(document: Document) => Promise<PdfJs>>('PDFJS_LOADER', {
  providedIn: 'root',
  factory: () => loadPdfJs,
});
