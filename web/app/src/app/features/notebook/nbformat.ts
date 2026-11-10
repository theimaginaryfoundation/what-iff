/**
 * nbformat 4 read and written by hand (ADR 0x043 §8): it is JSON. The model exposes what the editor
 * changes (type, source, outputs) and keeps everything else in `raw`, so metadata, attachments and
 * fields we have never heard of survive, and a notebook nobody edited serializes to the bytes Jupyter
 * wrote: one-space indent, keys in their original order, a trailing newline.
 */

export type CellType = 'code' | 'markdown' | 'raw';

/**
 * One cell output as stored. Notebooks come from anywhere, so every field is checked before use;
 * the known nbformat 4 shapes below are what the guards narrow to.
 */
export interface NotebookOutput {
  output_type: string;
  [key: string]: unknown;
}

export interface StreamOutput extends NotebookOutput {
  output_type: 'stream';
  name?: unknown;
  text?: unknown;
}

export interface ErrorOutput extends NotebookOutput {
  output_type: 'error';
  ename?: unknown;
  evalue?: unknown;
  traceback?: unknown;
}

export interface DataOutput extends NotebookOutput {
  output_type: 'display_data' | 'execute_result';
  data?: unknown;
  metadata?: unknown;
}

export function isStreamOutput(output: NotebookOutput): output is StreamOutput {
  return output.output_type === 'stream';
}

export function isErrorOutput(output: NotebookOutput): output is ErrorOutput {
  return output.output_type === 'error';
}

export function isDataOutput(output: NotebookOutput): output is DataOutput {
  return output.output_type === 'display_data' || output.output_type === 'execute_result';
}

/** An object field of an output, or an empty one when the file holds something else. */
export function recordField(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

export interface NotebookCell {
  /** This editor's own key; the nbformat `id` stays in `raw`. */
  readonly uid: string;
  readonly type: CellType;
  readonly source: string;
  readonly outputs: readonly NotebookOutput[];
  readonly executionCount: number | null;
  /** The cell as read, including `id`, `metadata` and `attachments`; the fields above override it on write. */
  readonly raw: Readonly<Record<string, unknown>>;
}

export interface Notebook {
  /** The top level as read, without `cells`. */
  readonly raw: Readonly<Record<string, unknown>>;
  readonly cells: readonly NotebookCell[];
  /** Whether the writer keeps `cells` before the other keys, as it found them. */
  readonly cellsFirst: boolean;
}

export class NotebookFormatError extends Error {}

let nextUid = 0;
const newUid = () => `cell-${++nextUid}`;

/** Parses a notebook; throws NotebookFormatError with a message fit to show. */
export function parseNotebook(text: string): Notebook {
  let json: unknown;
  try {
    json = JSON.parse(text);
  } catch {
    throw new NotebookFormatError('This file is not valid JSON, so it cannot be read as a notebook.');
  }
  if (!isRecord(json) || typeof json['nbformat'] !== 'number') {
    throw new NotebookFormatError('This file does not look like a Jupyter notebook.');
  }
  if (json['nbformat'] < 4) {
    throw new NotebookFormatError(`This notebook uses the older format ${json['nbformat']}. Open and save it in Jupyter to upgrade it.`);
  }
  if (!Array.isArray(json['cells'])) throw new NotebookFormatError('This notebook has no list of cells.');
  const { cells, ...rest } = json;
  return {
    raw: rest,
    cells: (cells as unknown[]).filter(isRecord).map(parseCell),
    cellsFirst: Object.keys(json)[0] === 'cells',
  };
}

function parseCell(raw: Record<string, unknown>): NotebookCell {
  const type: CellType = raw['cell_type'] === 'code' ? 'code' : raw['cell_type'] === 'raw' ? 'raw' : 'markdown';
  return {
    uid: newUid(),
    type,
    source: joinSource(raw['source']),
    outputs: Array.isArray(raw['outputs']) ? (raw['outputs'] as NotebookOutput[]) : [],
    executionCount: typeof raw['execution_count'] === 'number' ? raw['execution_count'] : null,
    raw,
  };
}

/** A multi-line field: nbformat allows a string or a list of lines with their newlines. */
export function joinSource(value: unknown): string {
  if (typeof value === 'string') return value;
  return Array.isArray(value) ? value.join('') : '';
}

/** Lines keeping their newlines, as Jupyter writes them; "" is no lines. */
export function splitSource(text: string): string[] {
  return text.match(/[^\n]*\n|[^\n]+$/g) ?? [];
}

export function serializeNotebook(notebook: Notebook): string {
  const cells = notebook.cells.map(writeCell);
  const out = notebook.cellsFirst ? { cells, ...notebook.raw } : { ...notebook.raw, cells };
  return `${JSON.stringify(out, null, 1)}\n`;
}

function writeCell(cell: NotebookCell): Record<string, unknown> {
  const out: Record<string, unknown> = { ...cell.raw, cell_type: cell.type };
  // A source written as a single string stays one, unless it would now differ.
  out['source'] = typeof cell.raw['source'] === 'string' ? cell.source : splitSource(cell.source);
  if (cell.type === 'code') {
    out['outputs'] = cell.outputs;
    out['execution_count'] = cell.executionCount;
  } else {
    delete out['outputs'];
    delete out['execution_count'];
  }
  return out;
}

/** nbformat 4.5 and later require a cell id, unique in the notebook. */
function usesCellIds(notebook: Pick<Notebook, 'raw'>): boolean {
  const minor = notebook.raw['nbformat_minor'];
  return (notebook.raw['nbformat'] as number) > 4 || (typeof minor === 'number' && minor >= 5);
}

function newCellId(): string {
  return Array.from({ length: 8 }, () => Math.floor(Math.random() * 16).toString(16)).join('');
}

export function newCell(notebook: Pick<Notebook, 'raw'>, type: CellType, source = ''): NotebookCell {
  const raw: Record<string, unknown> = { cell_type: type, metadata: {}, source: [] };
  if (usesCellIds(notebook)) raw['id'] = newCellId();
  return { uid: newUid(), type, source, outputs: [], executionCount: null, raw };
}

/** A copy for the same notebook: its own key and its own nbformat id. */
export function copyCell(notebook: Pick<Notebook, 'raw'>, cell: NotebookCell): NotebookCell {
  const raw = { ...cell.raw };
  if (usesCellIds(notebook)) raw['id'] = newCellId();
  return { ...cell, uid: newUid(), raw };
}

/** A Markdown cell's source with its `attachment:` images as data URLs, for showing. */
export function resolveAttachments(cell: NotebookCell): string {
  const attachments = cell.raw['attachments'];
  if (!isRecord(attachments)) return cell.source;
  return cell.source.replace(/attachment:([^\s)"']+)/g, (whole, name: string) => {
    const bundle = attachments[safeDecode(name)];
    if (!isRecord(bundle)) return whole;
    const mime = Object.keys(bundle).find(m => m.startsWith('image/') && m !== 'image/svg+xml');
    return mime ? `data:${mime};base64,${joinSource(bundle[mime]).replace(/\s+/g, '')}` : whole;
  });
}

/** The language code cells are written in, from the notebook's metadata. */
export function notebookLanguage(notebook: Pick<Notebook, 'raw'>): string {
  const metadata = notebook.raw['metadata'];
  if (isRecord(metadata)) {
    const info = metadata['language_info'];
    if (isRecord(info) && typeof info['name'] === 'string') return info['name'];
    const spec = metadata['kernelspec'];
    if (isRecord(spec) && typeof spec['language'] === 'string') return spec['language'];
  }
  return 'python';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** A name with a stray `%` is not percent-encoded: it is the name as written. */
function safeDecode(name: string): string {
  try {
    return decodeURIComponent(name);
  } catch {
    return name;
  }
}
