import { CellType, Notebook, NotebookCell, copyCell, newCell } from './nbformat';

/** The structural edits, as pure functions over the notebook; undo keeps whole versions, which share their cells. */

function withCells(notebook: Notebook, cells: readonly NotebookCell[]): Notebook {
  return { ...notebook, cells };
}

export function insertCell(notebook: Notebook, at: number, type: CellType): { notebook: Notebook; cell: NotebookCell } {
  const cell = newCell(notebook, type);
  const cells = notebook.cells.slice();
  cells.splice(Math.max(0, Math.min(at, cells.length)), 0, cell);
  return { notebook: withCells(notebook, cells), cell };
}

/** A notebook always keeps one cell, so the last one is emptied rather than removed. */
export function deleteCell(notebook: Notebook, uid: string): Notebook {
  const cells = notebook.cells.filter(c => c.uid !== uid);
  return withCells(notebook, cells.length ? cells : [newCell(notebook, 'code')]);
}

export function moveCell(notebook: Notebook, uid: string, delta: number): Notebook {
  const from = notebook.cells.findIndex(c => c.uid === uid);
  const to = from + delta;
  if (from === -1 || to < 0 || to >= notebook.cells.length) return notebook;
  const cells = notebook.cells.slice();
  const [cell] = cells.splice(from, 1);
  cells.splice(to, 0, cell);
  return withCells(notebook, cells);
}

export function duplicateCell(notebook: Notebook, uid: string): { notebook: Notebook; cell: NotebookCell | null } {
  const at = notebook.cells.findIndex(c => c.uid === uid);
  if (at === -1) return { notebook, cell: null };
  const cell = copyCell(notebook, notebook.cells[at]);
  const cells = notebook.cells.slice();
  cells.splice(at + 1, 0, cell);
  return { notebook: withCells(notebook, cells), cell };
}

function mapCell(notebook: Notebook, uid: string, patch: (cell: NotebookCell) => NotebookCell): Notebook {
  return withCells(
    notebook,
    notebook.cells.map(c => (c.uid === uid ? patch(c) : c)),
  );
}

export function setSource(notebook: Notebook, uid: string, source: string): Notebook {
  return mapCell(notebook, uid, c => (c.source === source ? c : { ...c, source }));
}

/** Leaving code drops outputs and the execution count, as Jupyter does. */
export function setCellType(notebook: Notebook, uid: string, type: CellType): Notebook {
  return mapCell(notebook, uid, c => (c.type === type ? c : { ...c, type, outputs: [], executionCount: null }));
}

export function clearOutputs(notebook: Notebook, uid?: string): Notebook {
  return withCells(
    notebook,
    notebook.cells.map(c =>
      (uid === undefined || c.uid === uid) && (c.outputs.length || c.executionCount !== null) ? { ...c, outputs: [], executionCount: null } : c,
    ),
  );
}
