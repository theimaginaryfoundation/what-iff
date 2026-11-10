import { monacoLanguageFor, vfsUriFor } from './monaco-loader';

describe('monaco-loader helpers', () => {
  const monaco = {
    languages: {
      getLanguages: () => [
        { id: 'python', extensions: ['.py', '.pyw'] },
        { id: 'markdown', extensions: ['.md'] },
        { id: 'json', extensions: ['.json'] },
      ],
    },
    Uri: { parse: (value: string) => ({ toString: () => value }) },
  };

  it('picks the language from the file extension, case-insensitively, else plain text', () => {
    expect(monacoLanguageFor(monaco as never, 'notes.PY')).toBe('python');
    expect(monacoLanguageFor(monaco as never, 'README.md')).toBe('markdown');
    expect(monacoLanguageFor(monaco as never, 'data.csv')).toBe('plaintext');
    expect(monacoLanguageFor(monaco as never, 'Makefile')).toBe('plaintext');
  });

  it('builds a stable VFS URI from the folder, name and id', () => {
    expect(vfsUriFor(monaco as never, { id: 'f 1', folder: '/docs/', name: 'a.md' }).toString()).toBe('vfs:///docs/a.md?id=f%201');
    expect(vfsUriFor(monaco as never, { id: 'x', name: 'top.txt' }).toString()).toBe('vfs:///top.txt?id=x');
  });
});
