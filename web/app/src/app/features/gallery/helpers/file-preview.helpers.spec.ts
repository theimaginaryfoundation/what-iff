import {
  csvDelimiterFor,
  decodeTextPreview,
  fileBadge,
  fileExtension,
  fileTypeLabel,
  parseCsv,
  previewKindFor,
} from './file-preview.helpers';

const file = (name: string, file_type = '') => ({ name, file_type });

describe('file-preview.helpers', () => {
  describe('previewKindFor', () => {
    it.each([
      ['notes.md', 'text/plain', 'markdown'],
      ['notes', 'text/markdown', 'markdown'],
      ['data.csv', 'application/octet-stream', 'csv'],
      ['data.tsv', '', 'csv'],
      ['report.pdf', 'application/pdf', 'pdf'],
      ['report', 'application/pdf', 'pdf'],
      ['photo.png', 'image/png', 'image'],
      ['main.go', '', 'text'],
      ['main.go', 'application/octet-stream', 'text'],
      ['page.html', 'text/html', 'text'],
      ['config.json', 'application/json', 'text'],
      ['readme', 'text/plain; charset=utf-8', 'text'],
      ['letter.docx', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', 'none'],
      ['bundle.zip', 'application/zip', 'none'],
      ['blob.bin', 'application/octet-stream', 'none'],
    ])('%s (%s) previews as %s', (name, type, kind) => {
      expect(previewKindFor(file(name, type))).toBe(kind);
    });

    it('shows SVG as source rather than rendering markup that can carry script', () => {
      expect(previewKindFor(file('logo.svg', 'image/svg+xml'))).toBe('text');
    });
  });

  it('reads extensions case-insensitively and ignores dotfiles and trailing dots', () => {
    expect(fileExtension('Report.PDF')).toBe('.pdf');
    expect(fileExtension('dir/archive.tar.gz')).toBe('.gz');
    expect(fileExtension('.env')).toBe('');
    expect(fileExtension('notes.')).toBe('');
  });

  it('labels tiles by extension, else by MIME subtype', () => {
    expect(fileBadge(file('a.json'))).toBe('JSON');
    expect(fileBadge(file('a.markdown'))).toBe('MARKD');
    expect(fileBadge(file('upload', 'application/pdf'))).toBe('PDF');
    expect(fileBadge(file('upload', 'application/vnd.ms-excel'))).toBe('MS');
    expect(fileBadge(file('upload', ''))).toBe('FILE');
  });

  it('describes the type in plain words', () => {
    expect(fileTypeLabel(file('a.md'))).toBe('Markdown');
    expect(fileTypeLabel(file('a.tsv'))).toBe('TSV table');
    expect(fileTypeLabel(file('a.go'))).toBe('GO file');
    expect(fileTypeLabel(file('a.zip', 'application/zip'))).toBe('ZIP file');
    expect(fileTypeLabel(file('upload', 'application/zip'))).toBe('application/zip');
  });

  it('picks the delimiter from the extension or MIME type', () => {
    expect(csvDelimiterFor(file('a.tsv'))).toBe('\t');
    expect(csvDelimiterFor(file('a', 'text/tab-separated-values'))).toBe('\t');
    expect(csvDelimiterFor(file('a.csv'))).toBe(',');
  });

  describe('decodeTextPreview', () => {
    const utf8 = (s: string) => new TextEncoder().encode(s);

    it('decodes UTF-8 and drops a byte-order mark', () => {
      const bytes = new Uint8Array([0xef, 0xbb, 0xbf, ...utf8('héllo — ok')]);
      expect(decodeTextPreview(bytes)).toEqual({ text: 'héllo — ok', truncated: false, binary: false });
    });

    it('cuts long files and says so', () => {
      const result = decodeTextPreview(utf8('abcdefghij'), 4);
      expect(result.text).toBe('abcd');
      expect(result.truncated).toBe(true);
    });

    it('flags bytes that are not text', () => {
      expect(decodeTextPreview(new Uint8Array([0x50, 0x4b, 0x03, 0x04, 0x00, 0x00])).binary).toBe(true);
      const noise = new Uint8Array(400).map((_, i) => (i % 2 ? 0xff : 0xfe));
      expect(decodeTextPreview(noise).binary).toBe(true);
    });

    it('treats an empty file as empty text', () => {
      expect(decodeTextPreview(new Uint8Array())).toEqual({ text: '', truncated: false, binary: false });
    });
  });

  describe('parseCsv', () => {
    it('splits the header from the rows', () => {
      expect(parseCsv('name,age\nAda,36\nAlan,41\n')).toEqual({
        header: ['name', 'age'],
        rows: [['Ada', '36'], ['Alan', '41']],
        truncatedRows: 0,
      });
    });

    it('handles quotes, doubled quotes, delimiters and line breaks inside quotes, and CRLF', () => {
      const table = parseCsv('a,b\r\n"x, y","say ""hi"""\r\n"multi\nline",z');
      expect(table.rows).toEqual([['x, y', 'say "hi"'], ['multi\nline', 'z']]);
    });

    it('pads ragged rows and skips blank lines', () => {
      const table = parseCsv('a,b,c\n1\n\n2,3\n');
      expect(table.header).toEqual(['a', 'b', 'c']);
      expect(table.rows).toEqual([['1', '', ''], ['2', '3', '']]);
    });

    it('splits on tabs for TSV', () => {
      expect(parseCsv('a\tb\n1,5\t2', '\t').rows).toEqual([['1,5', '2']]);
    });

    it('stops after the row cap and counts what it dropped', () => {
      const table = parseCsv('h\n1\n2\n3\n4', ',', 2);
      expect(table.rows).toEqual([['1'], ['2']]);
      expect(table.truncatedRows).toBe(2);
    });
  });
});
