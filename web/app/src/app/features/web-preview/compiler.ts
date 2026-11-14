import { CompileError, compileScript, compileStyle, warmUpEsbuild } from './compile';

/** How long one file may take to compile before its compiler is stopped. */
export const COMPILE_TIMEOUT_MS = 30_000;

/** What a project's TypeScript, JSX and SCSS are compiled with (ADR 0x050 §4). */
export interface Compiler {
  script(
    source: string,
    path: string,
    loader: 'ts' | 'tsx' | 'jsx',
    options: { jsxImportSource: string; sourceRanges?: { fileId: string; runtime: string } },
  ): Promise<string>;
  /** `read` is a project file's text, or null when there is none (a stylesheet's imports). */
  style(source: string, path: string, syntax: 'scss' | 'indented', read: (path: string) => Promise<string | null>): Promise<string>;
  /** Starts loading esbuild, for a page about to ask for TypeScript or JSX. */
  warmUp(): void;
}

/** Compiles on the calling thread: for tests, and a browser without workers. */
export function inlineCompiler(wasmBase: string): Compiler {
  return {
    script: (source, path, loader, options) => compileScript(source, path, loader, { ...options, wasmBase }),
    style: (source, path, syntax, read) => compileStyle(source, path, syntax, read),
    warmUp: () => void warmUpEsbuild(wasmBase),
  };
}

/** What the app tells the compile worker. */
export type ToCompileWorker =
  | {
      type: 'compile';
      id: number;
      op: 'script';
      source: string;
      path: string;
      loader: 'ts' | 'tsx' | 'jsx';
      options: { wasmBase: string; jsxImportSource: string; sourceRanges?: { fileId: string; runtime: string } };
    }
  | { type: 'compile'; id: number; op: 'style'; source: string; path: string; syntax: 'scss' | 'indented' }
  | { type: 'warm'; wasmBase: string }
  | { type: 'read-result'; readId: number; text: string | null };

/** What the compile worker tells the app. */
export type FromCompileWorker =
  | { type: 'done'; id: number; text: string }
  | { type: 'failed'; id: number; message: string; compileError: boolean }
  | { type: 'read'; id: number; readId: number; path: string };

interface Pending {
  path: string;
  resolve: (text: string) => void;
  reject: (error: Error) => void;
  read?: (path: string) => Promise<string | null>;
  timer: ReturnType<typeof setTimeout>;
}

/**
 * Compiles in a worker of the app's own (`compile.worker.ts`), off the thread the app runs on: a
 * stylesheet with an endless `@while`, or a huge file, would otherwise freeze the app. A file that
 * takes longer than COMPILE_TIMEOUT_MS stops the worker (and with it every file it was compiling),
 * and the next file starts a new one.
 */
export class WorkerCompiler implements Compiler {
  private worker: Worker | null = null;
  private readonly pending = new Map<number, Pending>();
  private nextId = 1;

  constructor(
    private readonly wasmBase: string,
    private readonly createWorker: () => Worker = () => new Worker(new URL('./compile.worker', import.meta.url), { type: 'module' }),
    private readonly timeoutMs = COMPILE_TIMEOUT_MS,
  ) {}

  script(
    source: string,
    path: string,
    loader: 'ts' | 'tsx' | 'jsx',
    options: { jsxImportSource: string; sourceRanges?: { fileId: string; runtime: string } },
  ): Promise<string> {
    return this.send(path, id => ({
      type: 'compile',
      id,
      op: 'script',
      source,
      path,
      loader,
      options: { ...options, wasmBase: this.wasmBase },
    }));
  }

  style(source: string, path: string, syntax: 'scss' | 'indented', read: (path: string) => Promise<string | null>): Promise<string> {
    return this.send(path, id => ({ type: 'compile', id, op: 'style', source, path, syntax }), read);
  }

  warmUp(): void {
    this.ensureWorker().postMessage({ type: 'warm', wasmBase: this.wasmBase } satisfies ToCompileWorker);
  }

  private send(path: string, message: (id: number) => ToCompileWorker, read?: (path: string) => Promise<string | null>): Promise<string> {
    const id = this.nextId++;
    const worker = this.ensureWorker();
    return new Promise<string>((resolve, reject) => {
      const timer = setTimeout(
        () => this.stop(`${path}: compiling took longer than ${this.timeoutMs / 1000} s and was stopped (an endless loop?)`),
        this.timeoutMs,
      );
      this.pending.set(id, { path, resolve, reject, read, timer });
      worker.postMessage(message(id));
    });
  }

  private ensureWorker(): Worker {
    if (this.worker) return this.worker;
    const worker = this.createWorker();
    worker.onmessage = (event: MessageEvent<FromCompileWorker>) => this.onMessage(worker, event.data);
    worker.onerror = () => this.stop('The compiler stopped unexpectedly.');
    this.worker = worker;
    return worker;
  }

  private onMessage(worker: Worker, data: FromCompileWorker): void {
    const pending = this.pending.get(data.id);
    if (!pending || worker !== this.worker) return;
    if (data.type === 'read') {
      const answer = (text: string | null) =>
        worker.postMessage({ type: 'read-result', readId: data.readId, text } satisfies ToCompileWorker);
      (pending.read ?? (async () => null))(data.path).then(answer, () => answer(null));
      return;
    }
    this.pending.delete(data.id);
    clearTimeout(pending.timer);
    if (data.type === 'done') pending.resolve(data.text);
    else pending.reject(data.compileError ? new CompileError(data.message) : new Error(data.message));
  }

  /** Stops the worker and fails what it was compiling; the next file starts a new one. */
  private stop(message: string): void {
    this.worker?.terminate();
    this.worker = null;
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new CompileError(message.startsWith(pending.path) ? message : `${pending.path}: ${message}`));
    }
    this.pending.clear();
  }
}

const shared = new Map<string, Compiler>();

/** The compiler a project uses: one worker for the app, or the calling thread where there are no workers. */
export function compilerFor(wasmBase: string): Compiler {
  let compiler = shared.get(wasmBase);
  if (!compiler) {
    compiler = typeof Worker === 'undefined' ? inlineCompiler(wasmBase) : new WorkerCompiler(wasmBase);
    shared.set(wasmBase, compiler);
  }
  return compiler;
}
