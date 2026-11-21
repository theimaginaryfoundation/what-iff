/// <reference lib="webworker" />

// The compile worker (ADR 0x050 §4; compiler.ts): esbuild and sass off the app's thread, so a
// file that takes forever stops the worker, not the app. Loaded from the app's own origin, as the
// Monaco workers are.

import { CompileError, compileScript, compileStyle, warmUpEsbuild } from './compile';
import type { FromCompileWorker, ToCompileWorker } from './compiler';

const reads = new Map<number, (text: string | null) => void>();
let nextRead = 1;

const post = (message: FromCompileWorker) => postMessage(message);

addEventListener('message', (event: MessageEvent<ToCompileWorker>) => {
  const data = event.data;
  switch (data.type) {
    case 'warm':
      void warmUpEsbuild(data.wasmBase);
      return;
    case 'read-result':
      reads.get(data.readId)?.(data.text);
      reads.delete(data.readId);
      return;
    case 'compile':
      void compile(data);
      return;
  }
});

async function compile(data: Extract<ToCompileWorker, { type: 'compile' }>): Promise<void> {
  try {
    const text =
      data.op === 'script'
        ? await compileScript(data.source, data.path, data.loader, data.options)
        : await compileStyle(data.source, data.path, data.syntax, path => readFromApp(data.id, path));
    post({ type: 'done', id: data.id, text });
  } catch (error) {
    post({
      type: 'failed',
      id: data.id,
      message: error instanceof Error ? error.message : String(error),
      compileError: error instanceof CompileError,
    });
  }
}

/** A project file's text, from the app, which holds the project. */
function readFromApp(id: number, path: string): Promise<string | null> {
  const readId = nextRead++;
  return new Promise(resolve => {
    reads.set(readId, resolve);
    post({ type: 'read', id, readId, path });
  });
}
