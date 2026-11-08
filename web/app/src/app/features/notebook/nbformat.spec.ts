import { deleteCell, duplicateCell, insertCell, moveCell, setCellType, setSource, clearOutputs } from './notebook-commands';
import { NotebookCell, NotebookFormatError, newCell, resolveAttachments, notebookLanguage, parseNotebook, serializeNotebook, splitSource } from './nbformat';

/** As Jupyter writes it: one-space indent, source as lines, a trailing newline, keys sorted. */
const JUPYTER = `{
 "cells": [
  {
   "cell_type": "markdown",
   "id": "aaaa1111",
   "metadata": {
    "tags": [
     "intro"
    ]
   },
   "attachments": {
    "a.png": {
     "image/png": "AAAA"
    }
   },
   "source": [
    "# Title\\n",
    "\\n",
    "Text — with ünicode"
   ]
  },
  {
   "cell_type": "code",
   "execution_count": 3,
   "id": "bbbb2222",
   "metadata": {},
   "outputs": [
    {
     "name": "stdout",
     "output_type": "stream",
     "text": [
      "hi\\n"
     ]
    }
   ],
   "source": [
    "print('hi')"
   ],
   "x_future_field": {
    "kept": true
   }
  }
 ],
 "metadata": {
  "kernelspec": {
   "display_name": "Python 3",
   "language": "python",
   "name": "python3"
  },
  "language_info": {
   "name": "python",
   "version": "3.11"
  }
 },
 "nbformat": 4,
 "nbformat_minor": 5
}
`;

describe('nbformat', () => {
  it('writes an untouched notebook back byte for byte', () => {
    expect(serializeNotebook(parseNotebook(JUPYTER))).toBe(JUPYTER);
  });

  it('reads the model and keeps what it does not model through an edit', () => {
    const nb = parseNotebook(JUPYTER);
    expect(nb.cells.map(c => c.type)).toEqual(['markdown', 'code']);
    expect(nb.cells[0].source).toBe('# Title\n\nText — with ünicode');
    expect(nb.cells[1].executionCount).toBe(3);
    expect(notebookLanguage(nb)).toBe('python');

    const edited = JSON.parse(serializeNotebook(setSource(nb, nb.cells[1].uid, "print('a')\nprint('b')\n")));
    expect(edited.cells[1].source).toEqual(["print('a')\n", "print('b')\n"]);
    expect(edited.cells[1].x_future_field).toEqual({ kept: true });
    expect(edited.cells[1].outputs).toHaveLength(1);
    expect(edited.cells[0].attachments['a.png']['image/png']).toBe('AAAA');
    expect(edited.metadata.language_info.version).toBe('3.11');
  });

  it('accepts a source written as one string, and keeps it one', () => {
    const nb = parseNotebook(JSON.stringify({ nbformat: 4, nbformat_minor: 2, metadata: {}, cells: [{ cell_type: 'code', source: 'a\nb', outputs: [], execution_count: null, metadata: {} }] }));
    expect(nb.cells[0].source).toBe('a\nb');
    expect(JSON.parse(serializeNotebook(nb)).cells[0].source).toBe('a\nb');
  });

  it('refuses what is not a notebook 4, with a reason', () => {
    expect(() => parseNotebook('not json')).toThrow(NotebookFormatError);
    expect(() => parseNotebook('{"a":1}')).toThrow(/does not look like/);
    expect(() => parseNotebook('{"nbformat":3,"worksheets":[]}')).toThrow(/older format 3/);
    expect(() => parseNotebook('{"nbformat":4}')).toThrow(/no list of cells/);
  });

  it('splits lines keeping their newlines', () => {
    expect(splitSource('')).toEqual([]);
    expect(splitSource('a')).toEqual(['a']);
    expect(splitSource('a\n')).toEqual(['a\n']);
    expect(splitSource('a\n\nb')).toEqual(['a\n', '\n', 'b']);
  });

  it('gives new cells an id only where the format has them, and writes each type validly', () => {
    const modern = parseNotebook(JUPYTER);
    expect(newCell(modern, 'code').raw['id']).toMatch(/^[0-9a-f]{8}$/);
    const old = parseNotebook(JSON.stringify({ nbformat: 4, nbformat_minor: 2, metadata: {}, cells: [] }));
    expect(newCell(old, 'code').raw['id']).toBeUndefined();

    const { notebook } = insertCell(old, 0, 'code');
    const { notebook: two } = insertCell(notebook, 1, 'markdown');
    const written = JSON.parse(serializeNotebook(two)).cells;
    expect(written[0]).toEqual({ cell_type: 'code', metadata: {}, source: [], outputs: [], execution_count: null });
    expect(written[1]).toEqual({ cell_type: 'markdown', metadata: {}, source: [] });
  });

  it('edits structure without touching the version it came from', () => {
    const nb = parseNotebook(JUPYTER);
    const [a, b] = nb.cells;
    expect(moveCell(nb, b.uid, -1).cells.map(c => c.uid)).toEqual([b.uid, a.uid]);
    expect(moveCell(nb, a.uid, -1)).toBe(nb);
    const dup = duplicateCell(nb, b.uid);
    expect(dup.notebook.cells).toHaveLength(3);
    expect(dup.cell?.raw['id']).not.toBe(b.raw['id']);
    expect(deleteCell(nb, a.uid).cells.map(c => c.uid)).toEqual([b.uid]);
    expect(deleteCell(deleteCell(nb, a.uid), b.uid).cells).toHaveLength(1);
    expect(nb.cells).toHaveLength(2);
  });

  it('drops outputs when a cell leaves code, and clears them on request', () => {
    const nb = parseNotebook(JUPYTER);
    const uid = nb.cells[1].uid;
    const md = JSON.parse(serializeNotebook(setCellType(nb, uid, 'markdown'))).cells[1];
    expect(md.cell_type).toBe('markdown');
    expect(md.outputs).toBeUndefined();
    expect(md.execution_count).toBeUndefined();
    const cleared = JSON.parse(serializeNotebook(clearOutputs(nb))).cells[1];
    expect(cleared.outputs).toEqual([]);
    expect(cleared.execution_count).toBeNull();
  });

  it('leaves an attachment name that is not valid percent-encoding as written', () => {
    const cell = { source: '![x](attachment:100%.png)', raw: { attachments: { '100%.png': { 'image/png': 'aGk=' } } } };
    expect(resolveAttachments(cell as unknown as NotebookCell)).toBe('![x](data:image/png;base64,aGk=)');
  });
});
