import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { CompileError } from './compile';
import { FromCompileWorker, ToCompileWorker, WorkerCompiler } from './compiler';

/** A worker that records what it is sent; `reply` answers as the compile worker would. */
class FakeWorker {
  sent: ToCompileWorker[] = [];
  terminated = false;
  onmessage: ((event: MessageEvent<FromCompileWorker>) => void) | null = null;
  onerror: (() => void) | null = null;
  postMessage(message: ToCompileWorker): void {
    this.sent.push(message);
  }
  terminate(): void {
    this.terminated = true;
  }
  reply(message: FromCompileWorker): void {
    this.onmessage?.({ data: message } as MessageEvent<FromCompileWorker>);
  }
}

describe('WorkerCompiler', () => {
  afterEach(() => vi.useRealTimers());

  const setup = (timeoutMs = 1000) => {
    const workers: FakeWorker[] = [];
    const compiler = new WorkerCompiler(
      'https://app.test/assets/esbuild/',
      () => {
        const w = new FakeWorker();
        workers.push(w);
        return w as unknown as Worker;
      },
      timeoutMs,
    );
    return { compiler, workers };
  };

  it('compiles in its worker, answering the stylesheet’s reads of project files', async () => {
    const { compiler, workers } = setup();
    const read = vi.fn(async (path: string) => (path === '_vars.scss' ? '$c: red;' : null));
    const css = compiler.style("@use 'vars';", 'app.scss', 'scss', read);
    const worker = workers[0]!;
    const job = worker.sent[0] as Extract<ToCompileWorker, { type: 'compile' }>;
    expect(job).toMatchObject({ type: 'compile', op: 'style', path: 'app.scss' });
    worker.reply({ type: 'read', id: job.id, readId: 7, path: '_vars.scss' });
    await vi.waitFor(() => expect(worker.sent.at(-1)).toEqual({ type: 'read-result', readId: 7, text: '$c: red;' }));
    worker.reply({ type: 'done', id: job.id, text: 'h1{color:red}' });
    await expect(css).resolves.toBe('h1{color:red}');

    const js = compiler.script('let a: number', 'a.ts', 'ts', { jsxImportSource: 'react' });
    const script = worker.sent.at(-1) as Extract<ToCompileWorker, { type: 'compile'; op: 'script' }>;
    expect(script.options.wasmBase).toBe('https://app.test/assets/esbuild/');
    worker.reply({ type: 'failed', id: script.id, message: 'a.ts:1:5: Expected ";"', compileError: true });
    await expect(js).rejects.toBeInstanceOf(CompileError);
  });

  it('stops a worker that takes too long, failing what it was compiling, and starts a new one next time', async () => {
    vi.useFakeTimers();
    const { compiler, workers } = setup(1000);
    const endless = compiler.style('@while true {}', 'loop.scss', 'scss', async () => null);
    const caught = endless.then(
      () => new Error('compiled'),
      (e: Error) => e,
    );
    vi.advanceTimersByTime(1000);
    const error = await caught;
    expect(error).toBeInstanceOf(CompileError);
    expect(error.message).toContain('loop.scss: compiling took longer than 1 s');
    expect(workers[0]!.terminated).toBe(true);

    void compiler.script('', 'b.ts', 'ts', { jsxImportSource: 'react' });
    expect(workers).toHaveLength(2);
  });
});

describe('the esbuild WebAssembly asset', () => {
  it('is served from the folder of the installed version, which esbuild asks for', () => {
    const app = resolve(__dirname, '../../../..');
    const version = JSON.parse(readFileSync(resolve(app, 'node_modules/esbuild-wasm/package.json'), 'utf8')).version as string;
    const outputs = [...readFileSync(resolve(app, 'angular.json'), 'utf8').matchAll(/"output": "assets\/esbuild\/([^"]*)"/g)].map(
      m => m[1],
    );
    expect(outputs.length).toBeGreaterThan(0);
    for (const output of outputs) expect(output).toBe(version);
  });
});
