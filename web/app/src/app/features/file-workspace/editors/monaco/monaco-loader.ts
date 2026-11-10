import { InjectionToken } from '@angular/core';

import { defineMonacoThemes } from './monaco-themes';

export type Monaco = typeof import('monaco-editor');

/** Monaco's stylesheet, built as a lazy global style (angular.json `styles`, `inject: false`). */
export const MONACO_STYLESHEET = 'monaco.css';

let monacoPromise: Promise<Monaco> | undefined;

/**
 * Loads Monaco once (ADR 0x043 step 0 recipe): links its stylesheet, points each language service
 * at its own worker bundle, then imports the editor. Language tokenizers register lazily inside
 * Monaco, so this costs nothing per language. Mirrors loadKatex().
 */
export function loadMonaco(document: Document): Promise<Monaco> {
  monacoPromise ??= (async () => {
    if (!document.querySelector('link[data-monaco]')) {
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = MONACO_STYLESHEET;
      link.dataset['monaco'] = '1';
      await new Promise<void>(resolve => {
        link.onload = () => resolve();
        link.onerror = () => resolve(); // the editor still works, unstyled, and the console says why
        document.head.appendChild(link);
      });
    }
    (globalThis as { MonacoEnvironment?: unknown }).MonacoEnvironment = {
      getWorker(_workerId: string, label: string): Worker {
        switch (label) {
          case 'json':
            return new Worker(new URL('./workers/json.worker', import.meta.url), { type: 'module' });
          case 'css':
          case 'scss':
          case 'less':
            return new Worker(new URL('./workers/css.worker', import.meta.url), { type: 'module' });
          case 'html':
          case 'handlebars':
          case 'razor':
            return new Worker(new URL('./workers/html.worker', import.meta.url), { type: 'module' });
          case 'typescript':
          case 'javascript':
            return new Worker(new URL('./workers/ts.worker', import.meta.url), { type: 'module' });
          default:
            return new Worker(new URL('./workers/editor.worker', import.meta.url), { type: 'module' });
        }
      },
    };
    const monaco = await import('monaco-editor');
    defineMonacoThemes(monaco);
    return monaco;
  })();
  // A failed load is not kept, so the next editor retries instead of failing until a reload.
  const attempt = monacoPromise;
  attempt.catch(() => {
    if (monacoPromise === attempt) monacoPromise = undefined;
  });
  return attempt;
}

/** The loader as a token, so tests can hand components a fake Monaco. */
export const MONACO_LOADER = new InjectionToken<(document: Document) => Promise<Monaco>>('MONACO_LOADER', {
  providedIn: 'root',
  factory: () => loadMonaco,
});

/** The Monaco language whose registered extensions include the file's, else plain text. */
export function monacoLanguageFor(monaco: Pick<Monaco, 'languages'>, fileName: string): string {
  const dot = fileName.lastIndexOf('.');
  const ext = dot >= 0 ? fileName.slice(dot).toLowerCase() : '';
  if (!ext) return 'plaintext';
  const hit = monaco.languages.getLanguages().find(l => l.extensions?.some(e => e.toLowerCase() === ext));
  return hit?.id ?? 'plaintext';
}

/** A stable model URI for a file in the virtual file system, so language services key on its path. */
export function vfsUriFor(monaco: Pick<Monaco, 'Uri'>, file: { id: string; folder?: string; name: string }) {
  const folder = file.folder ? file.folder.replace(/^\/+|\/+$/g, '') + '/' : '';
  return monaco.Uri.parse(`vfs:///${folder}${file.name}?id=${encodeURIComponent(file.id)}`);
}
