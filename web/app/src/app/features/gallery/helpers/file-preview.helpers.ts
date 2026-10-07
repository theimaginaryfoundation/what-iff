import { FileAttachment, isImageAttachment } from '../../../core/models/file-attachment.model';

/**
 * How the in-tab file viewer shows a file. Everything is read-only.
 * - markdown / csv: rendered, with a toggle back to the raw text.
 * - text: plain text or source code, never interpreted (HTML and SVG source included).
 * - pdf: the browser's own PDF viewer, where it has one.
 * - image: the bytes as an image (only reached by a direct ?file= link; the grid opens images in the image popup).
 * - none: no preview (office documents, archives, unknown binaries); download only.
 */
export type FilePreviewKind = 'markdown' | 'csv' | 'text' | 'pdf' | 'image' | 'none';

/** Text bigger than this is cut off in the viewer (download for the rest). */
export const MAX_TEXT_PREVIEW_BYTES = 1024 * 1024;
/** Markdown bigger than this is shown raw: rendering it would stall the tab. */
export const MAX_MARKDOWN_RENDER_CHARS = 300_000;
/** A CSV table stops after this many rows (the raw view still shows the whole preview). */
export const MAX_CSV_TABLE_ROWS = 2_000;

const MARKDOWN_EXTENSIONS = new Set(['.md', '.markdown', '.mdx']);
const CSV_EXTENSIONS = new Set(['.csv', '.tsv']);
/** Extensions read as text whatever MIME type the upload claimed (browsers often send none for code). */
const TEXT_EXTENSIONS = new Set([
  '.txt', '.text', '.log', '.json', '.jsonl', '.ndjson', '.xml', '.yaml', '.yml', '.toml', '.ini', '.cfg', '.conf', '.env',
  '.html', '.htm', '.css', '.scss', '.svg', '.js', '.mjs', '.cjs', '.ts', '.tsx', '.jsx', '.go', '.py', '.rb', '.php',
  '.java', '.kt', '.swift', '.c', '.h', '.cpp', '.hpp', '.cc', '.cs', '.rs', '.sh', '.bash', '.zsh', '.ps1', '.sql',
  '.tex', '.r', '.lua', '.pl', '.dart', '.scala', '.vue', '.graphql', '.proto', '.diff', '.patch', '.rst', '.adoc', '.srt',
  '.vtt',
]);
/** Non-text application/* types that are still plain text. */
const TEXT_MIME_TYPES = new Set([
  'application/json', 'application/ld+json', 'application/xml', 'application/javascript', 'application/x-javascript',
  'application/typescript', 'application/x-sh', 'application/x-python', 'application/x-yaml', 'application/yaml',
  'application/toml', 'application/sql', 'application/x-tex', 'application/x-httpd-php', 'application/xhtml+xml',
  'application/x-ndjson', 'application/graphql',
]);

/** The lower-case extension of a file name, with its dot ("" if it has none). */
export function fileExtension(name: string): string {
  const base = (name ?? '').split(/[/\\]/).pop() ?? '';
  const dot = base.lastIndexOf('.');
  return dot <= 0 || dot === base.length - 1 ? '' : base.slice(dot).toLowerCase();
}

function mimeOf(file: Pick<FileAttachment, 'file_type'>): string {
  return (file.file_type ?? '').split(';')[0].trim().toLowerCase();
}

export function previewKindFor(file: Pick<FileAttachment, 'name' | 'file_type'>): FilePreviewKind {
  const mime = mimeOf(file);
  const ext = fileExtension(file.name);
  if (MARKDOWN_EXTENSIONS.has(ext) || mime === 'text/markdown' || mime === 'text/x-markdown') return 'markdown';
  if (CSV_EXTENSIONS.has(ext) || mime === 'text/csv' || mime === 'text/tab-separated-values') return 'csv';
  if (mime === 'application/pdf' || ext === '.pdf') return 'pdf';
  // SVG is an image type but is markup that can carry script: show its source, never render it.
  if (mime === 'image/svg+xml' || ext === '.svg') return 'text';
  if (isImageAttachment(file)) return 'image';
  if (TEXT_EXTENSIONS.has(ext) || mime.startsWith('text/') || TEXT_MIME_TYPES.has(mime)) return 'text';
  return 'none';
}

/** The delimiter a CSV-like file uses: tab for .tsv / text/tab-separated-values, else comma. */
export function csvDelimiterFor(file: Pick<FileAttachment, 'name' | 'file_type'>): ',' | '\t' {
  return fileExtension(file.name) === '.tsv' || mimeOf(file) === 'text/tab-separated-values' ? '\t' : ',';
}

/** A short badge for a file tile: the extension ("PDF", "MD"), else a word from the MIME type. */
export function fileBadge(file: Pick<FileAttachment, 'name' | 'file_type'>): string {
  const ext = fileExtension(file.name);
  if (ext) return ext.slice(1, 6).toUpperCase();
  const subtype = mimeOf(file).split('/')[1] ?? '';
  const word = subtype.replace(/^(x-|vnd\.)/, '').split(/[.+-]/)[0];
  return word ? word.slice(0, 5).toUpperCase() : 'FILE';
}

/** A plain-language name for a file's type, for the viewer's details line. */
export function fileTypeLabel(file: Pick<FileAttachment, 'name' | 'file_type'>): string {
  switch (previewKindFor(file)) {
    case 'markdown':
      return 'Markdown';
    case 'csv':
      return csvDelimiterFor(file) === '\t' ? 'TSV table' : 'CSV table';
    case 'pdf':
      return 'PDF';
    case 'image':
      return 'Image';
    case 'text':
      return fileExtension(file.name) ? `${fileBadge(file)} file` : 'Text';
    default:
      // "DOCX file" reads better than application/vnd.openxmlformats-officedocument...
      return fileExtension(file.name) ? `${fileBadge(file)} file` : mimeOf(file) || 'File';
  }
}

export interface DecodedText {
  text: string;
  /** The file was longer than MAX_TEXT_PREVIEW_BYTES and only its start is in text. */
  truncated: boolean;
  /** The bytes do not look like text (NUL bytes or many invalid UTF-8 sequences). */
  binary: boolean;
}

/**
 * Decodes the start of a file as UTF-8 for the viewer. The cap is in bytes and is applied to the
 * bytes before decoding; a multi-byte character split by the cut decodes as one replacement
 * character at the end. A byte-order mark is dropped. Bytes that are clearly not text are flagged
 * rather than shown as a wall of replacement characters.
 */
export function decodeTextPreview(bytes: Uint8Array, maxBytes = MAX_TEXT_PREVIEW_BYTES): DecodedText {
  const truncated = bytes.length > maxBytes;
  const head = truncated ? bytes.subarray(0, maxBytes) : bytes;
  const text = new TextDecoder('utf-8', { fatal: false, ignoreBOM: false }).decode(head);
  const sample = text.slice(0, 8192);
  let replacements = 0;
  for (const ch of sample) {
    // By design a NUL byte means binary: the text the viewer shows (UTF-8 text, code, CSV) never has one.
    if (ch === '\u0000') return { text: '', truncated, binary: true };
    if (ch === '�') replacements += 1;
  }
  // A cut can split one multi-byte character, so allow a little noise before calling it binary.
  const binary = sample.length > 0 && replacements > Math.max(4, sample.length / 50);
  return { text: binary ? '' : text, truncated, binary };
}

export interface CsvTable {
  header: string[];
  rows: string[][];
  /** There were more rows than MAX_CSV_TABLE_ROWS; parsing stopped at the cap. */
  truncated: boolean;
}

/**
 * Parses CSV/TSV text (RFC 4180: quoted fields, doubled quotes, line breaks inside quotes, CRLF or
 * LF). The first row is the header. Ragged rows are padded to the widest row so the table lines up.
 * Parsing stops at the first row past the cap: the table never shows more, and the raw view has the rest.
 */
export function parseCsv(text: string, delimiter: ',' | '\t' = ',', maxRows = MAX_CSV_TABLE_ROWS): CsvTable {
  const records: string[][] = [];
  let record: string[] = [];
  let field = '';
  let quoted = false;
  let truncated = false;
  const pushRecord = () => {
    record.push(field);
    field = '';
    // A blank line is not a row.
    if (!(record.length === 1 && record[0] === '')) {
      if (records.length <= maxRows) records.push(record);
      else truncated = true;
    }
    record = [];
  };
  for (let i = 0; i < text.length && !truncated; i += 1) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i += 1;
        } else {
          quoted = false;
        }
      } else {
        field += ch;
      }
    } else if (ch === '"' && field === '') {
      quoted = true;
    } else if (ch === delimiter) {
      record.push(field);
      field = '';
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i += 1;
      pushRecord();
    } else {
      field += ch;
    }
  }
  if (!truncated && (field !== '' || record.length > 0)) pushRecord();

  const [header = [], ...rows] = records;
  const width = records.reduce((max, row) => Math.max(max, row.length), 0);
  const pad = (row: string[]) => (row.length < width ? [...row, ...Array<string>(width - row.length).fill('')] : row);
  return { header: pad(header), rows: rows.map(pad), truncated };
}
