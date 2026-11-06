import { AfterViewInit, ChangeDetectionStrategy, Component, ElementRef, OnDestroy, signal, viewChild } from '@angular/core';

type Monaco = typeof import('monaco-editor');

let monacoPromise: Promise<Monaco> | undefined;

/** Loads Monaco once and wires its workers. Mirrors loadKatex() in latex-math.helpers.ts. */
function loadMonaco(): Promise<Monaco> {
  monacoPromise ??= (async () => {
    (self as unknown as { MonacoEnvironment: unknown }).MonacoEnvironment = {
      getWorker(_workerId: string, label: string): Worker {
        switch (label) {
          case 'json':
            return new Worker(new URL('./workers/json.worker', import.meta.url), { type: 'module' });
          case 'css': case 'scss': case 'less':
            return new Worker(new URL('./workers/css.worker', import.meta.url), { type: 'module' });
          case 'html': case 'handlebars': case 'razor':
            return new Worker(new URL('./workers/html.worker', import.meta.url), { type: 'module' });
          case 'typescript': case 'javascript':
            return new Worker(new URL('./workers/ts.worker', import.meta.url), { type: 'module' });
          default:
            return new Worker(new URL('./workers/editor.worker', import.meta.url), { type: 'module' });
        }
      },
    };
    // Monaco's stylesheet is a lazy global style (angular.json styles entry with inject:false),
    // because the esbuild builder does not wire up CSS imported from TypeScript.
    if (!document.querySelector('link[data-monaco]')) {
      const link = document.createElement('link');
      link.rel = 'stylesheet'; link.href = 'monaco.css'; link.dataset['monaco'] = '1';
      await new Promise<void>((resolve, reject) => { link.onload = () => resolve(); link.onerror = () => reject(new Error('monaco.css failed')); document.head.appendChild(link); });
    }
    return import('monaco-editor');
  })();
  return monacoPromise;
}

/**
 * Throwaway spike for ADR 0x043 step 0: does Monaco load lazily under the esbuild application
 * builder, with language workers, per-language tokenizers and static colorize output?
 */
@Component({
  selector: 'app-monaco-spike',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div style="padding: 12px; font: 13px system-ui">
      <p data-testid="status">{{ status() }}</p>
      <div #json data-testid="json" style="height: 160px; border: 1px solid #888"></div>
      <div #ts data-testid="ts" style="height: 160px; border: 1px solid #888; margin-top: 8px"></div>
      <div #py data-testid="py" style="height: 120px; border: 1px solid #888; margin-top: 8px"></div>
      <pre data-testid="colorized" style="border: 1px solid #888; margin-top: 8px; padding: 4px" [innerHTML]="colorized()"></pre>
      <p data-testid="markers">{{ markers() }}</p>
      <p data-testid="suggest">{{ suggestions() }}</p>
    </div>
  `,
})
export class MonacoSpikeComponent implements AfterViewInit, OnDestroy {
  readonly status = signal('loading');
  readonly colorized = signal('');
  readonly markers = signal('');
  readonly suggestions = signal('');
  private readonly jsonHost = viewChild.required<ElementRef<HTMLElement>>('json');
  private readonly tsHost = viewChild.required<ElementRef<HTMLElement>>('ts');
  private readonly pyHost = viewChild.required<ElementRef<HTMLElement>>('py');
  private disposables: { dispose(): void }[] = [];

  async ngAfterViewInit(): Promise<void> {
    const t0 = performance.now();
    const monaco = await loadMonaco();
    const loadedMs = Math.round(performance.now() - t0);

    const jsonModel = monaco.editor.createModel('{\n  "name": "x",\n  "name": 1\n}', 'json', monaco.Uri.parse('inmemory://spike/a.json'));
    const json = monaco.editor.create(this.jsonHost().nativeElement, { model: jsonModel, automaticLayout: true });
    const tsModel = monaco.editor.createModel('const a: number = "s";\nconst b = [1, 2, 3];\nb.', 'typescript', monaco.Uri.parse('inmemory://spike/b.ts'));
    const ts = monaco.editor.create(this.tsHost().nativeElement, { model: tsModel, automaticLayout: true });
    const py = monaco.editor.create(this.pyHost().nativeElement, { value: 'def f(x):\n    return x * 2  # comment\n', language: 'python', readOnly: true, automaticLayout: true });
    this.disposables.push(json, ts, py, jsonModel, tsModel);

    // Static highlighting for the phone (read-only) view: no editor instance.
    this.colorized.set(await monaco.editor.colorize('package main\n\nfunc main() {\n\tprintln("hi")\n}\n', 'go', {}));

    // Worker-backed diagnostics for JSON (duplicate key) and TypeScript (type error).
    await new Promise(r => setTimeout(r, 2500));
    const m = monaco.editor.getModelMarkers({});
    this.markers.set(m.map(x => `${x.owner}:${x.message}`).join(' | '));

    // Worker-backed completions for TypeScript (array methods after "b.").
    const provider = (await monaco.typescript.getTypeScriptWorker())(tsModel.uri);
    const worker = await provider;
    const completions = await worker.getCompletionsAtPosition(tsModel.uri.toString(), tsModel.getValue().length);
    const names = (completions?.entries ?? []).map((e: { name: string }) => e.name);
    this.suggestions.set(names.filter((n: string) => ['map', 'filter', 'length'].includes(n)).join(','));

    this.status.set(`ready in ${loadedMs}ms`);
  }

  ngOnDestroy(): void {
    this.disposables.forEach(d => d.dispose());
  }
}
