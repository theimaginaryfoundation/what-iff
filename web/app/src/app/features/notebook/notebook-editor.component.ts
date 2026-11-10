import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, computed, effect, inject, input, output, signal, untracked, viewChild } from '@angular/core';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileEditor } from '../file-workspace/editors/file-editor';
import { CellType, Notebook, NotebookCell, NotebookFormatError, notebookLanguage, parseNotebook, serializeNotebook } from './nbformat';
import { notebookLoaders } from './notebook-outputs';
import { clearOutputs, deleteCell, duplicateCell, insertCell, moveCell, setCellType, setSource } from './notebook-commands';
import { NotebookCellComponent } from './notebook-cell.component';
import { NotebookRunComponent } from './notebook-run.component';
import { JUPYTERLITE_URL } from './notebook-run.config';

export const IPYNB_MIME = 'application/x-ipynb+json';
const DIRTY_CHECK_MS = 300;
const HISTORY_LIMIT = 100;

/**
 * The notebook viewer and editor (ADR 0x043 §11): our own cells over nbformat. The same component is
 * both: `readOnly` defaults to true and the host sets false for the editor role. Edits are to cells
 * and their order; nothing executes here.
 */
@Component({
  selector: 'app-notebook-editor',
  standalone: true,
  imports: [NotebookCellComponent, NotebookRunComponent],
  templateUrl: './notebook-editor.component.html',
  styleUrl: './notebook-editor.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class NotebookEditorComponent implements FileEditor {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly runner = viewChild(NotebookRunComponent);
  /** Null when this deployment has no runner: Run is not offered. */
  readonly runUrl = inject(JUPYTERLITE_URL);

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();
  readonly readOnly = input(true);

  readonly dirtyChange = output<boolean>();

  /** Run is open: its edits, outputs and kernel state live in the runner until it saves. */

  readonly busyChange = output<boolean>();
  readonly saveRequested = output<void>();

  readonly dirty = signal(false);
  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly error = signal<string | null>(null);
  readonly notebook = signal<Notebook | null>(null);
  readonly activeUid = signal<string | null>(null);
  readonly editingUid = signal<string | null>(null);
  readonly canUndo = signal(false);
  readonly canRedo = signal(false);
  /** Run mode: JupyterLite takes the cells' place, holding the notebook as it was when entered. */
  readonly running = signal(false);
  /** The reader chose to run this notebook's script-driven output; it lasts until another file opens. */
  readonly interactiveAllowed = signal(false);
  readonly runNotebook = signal<unknown>(null);
  readonly cellTypes: readonly { value: CellType; label: string }[] = [
    { value: 'code', label: 'Code' },
    { value: 'markdown', label: 'Markdown' },
    { value: 'raw', label: 'Raw' },
  ];

  readonly cells = computed<readonly NotebookCell[]>(() => this.notebook()?.cells ?? []);
  readonly language = computed(() => {
    const notebook = this.notebook();
    return notebook ? notebookLanguage(notebook) : 'python';
  });
  /** Only worked out once allowed: the frames are the only thing that use it. */
  readonly loaders = computed(() => (this.interactiveAllowed() ? notebookLoaders(this.cells()) : ''));
  readonly activeCell = computed(() => this.cells().find(c => c.uid === this.activeUid()) ?? null);
  readonly activeIndex = computed(() => this.cells().findIndex(c => c.uid === this.activeUid()));
  readonly hasOutputs = computed(() => this.cells().some(c => c.outputs.length > 0 || c.executionCount !== null));

  private baseline = '';
  private lastContent: Blob | null = null;
  /** The serialized text behind each blob handed to the host. */
  private readonly handedOut = new WeakMap<Blob, string>();
  private generation = 0;
  private undoStack: Notebook[] = [];
  private redoStack: Notebook[] = [];
  /** Typing in one cell is one undo step: the version from before the first keystroke. */
  private typingIn: string | null = null;
  private dirtyTimer: ReturnType<typeof setTimeout> | null = null;
  private pendingDelete = false;

  constructor() {
    effect(() => {
      const running = this.running();
      untracked(() => this.busyChange.emit(running));
    });
    effect(() => {
      const blob = this.blob();
      this.file();
      // After a save the host hands back the bytes this editor produced; it already shows them.
      if (blob === this.lastContent) return;
      untracked(() => void this.open(blob));
    });
    // Leaving edit mode leaves no cell being edited.
    effect(() => {
      if (this.readOnly()) {
        untracked(() => {
          this.editingUid.set(null);
          this.running.set(false);
        });
      }
    });
    // Focusable, so the notebook's keys work once a cell's editor has closed.
    this.host.nativeElement.tabIndex = -1;
    this.host.nativeElement.addEventListener('keydown', event => this.onKeydown(event));
    inject(DestroyRef).onDestroy(() => {
      this.generation += 1;
      if (this.dirtyTimer) clearTimeout(this.dirtyTimer);
    });
  }

  // --- FileEditor --------------------------------------------------------------------------------

  async content(): Promise<Blob> {
    const notebook = this.notebook();
    if (!notebook) throw new Error('The notebook is not loaded');
    const text = serializeNotebook(notebook);
    const blob = new Blob([text], { type: IPYNB_MIME });
    this.handedOut.set(blob, text);
    this.lastContent = blob;
    return blob;
  }

  markSaved(saved?: Blob): void {
    const notebook = this.notebook();
    if (!notebook) return this.setDirty(false);
    const current = serializeNotebook(notebook);
    // What was saved, which is not the current notebook if the user kept typing during the save.
    this.baseline = (saved && this.handedOut.get(saved)) ?? current;
    this.setDirty(current !== this.baseline);
  }

  // --- selection ---------------------------------------------------------------------------------

  select(cell: NotebookCell): void {
    if (this.readOnly()) return;
    if (this.editingUid() && this.editingUid() !== cell.uid) this.stopEditing();
    this.activeUid.set(cell.uid);
    // Code is edited as soon as it is clicked; Markdown is read until asked for.
    if (cell.type === 'code') this.startEditing(cell.uid);
  }

  startEditing(uid: string): void {
    if (this.readOnly()) return;
    this.activeUid.set(uid);
    this.editingUid.set(uid);
    this.typingIn = null;
  }

  stopEditing(): void {
    this.editingUid.set(null);
    this.typingIn = null;
    // Keys go back to the notebook once the editor is gone.
    this.host.nativeElement.focus({ preventScroll: true });
  }

  /** Shift+Enter: on to the next cell, or a new one at the end, as in Jupyter. */
  next(): void {
    const index = this.activeIndex();
    const following = this.cells()[index + 1];
    if (following) this.select(following);
    else this.addCell('code', 'below');
  }

  // --- edits -------------------------------------------------------------------------------------

  onSourceChange(uid: string, source: string): void {
    const notebook = this.notebook();
    if (!notebook) return;
    const next = setSource(notebook, uid, source);
    if (next.cells === notebook.cells || next.cells.every((c, i) => c === notebook.cells[i])) return;
    if (this.typingIn !== uid) {
      this.remember();
      this.typingIn = uid;
    }
    this.apply(next);
  }

  addCell(type: CellType, where: 'above' | 'below'): void {
    const notebook = this.notebook();
    if (!notebook || this.readOnly()) return;
    const index = this.activeIndex();
    const at = index === -1 ? notebook.cells.length : where === 'above' ? index : index + 1;
    const { notebook: next, cell } = insertCell(notebook, at, type);
    this.edit(next);
    this.startEditing(cell.uid);
  }

  deleteActive(): void {
    const uid = this.activeUid();
    const notebook = this.notebook();
    if (!uid || !notebook) return;
    const index = this.activeIndex();
    const next = deleteCell(notebook, uid);
    this.edit(next);
    this.editingUid.set(null);
    this.activeUid.set(next.cells[Math.min(index, next.cells.length - 1)]?.uid ?? null);
  }

  moveActive(delta: number): void {
    const uid = this.activeUid();
    const notebook = this.notebook();
    if (uid && notebook) this.edit(moveCell(notebook, uid, delta));
  }

  duplicateActive(): void {
    const uid = this.activeUid();
    const notebook = this.notebook();
    if (!uid || !notebook) return;
    const { notebook: next, cell } = duplicateCell(notebook, uid);
    this.edit(next);
    if (cell) this.activeUid.set(cell.uid);
    this.editingUid.set(null);
  }

  setActiveType(type: CellType): void {
    const uid = this.activeUid();
    const notebook = this.notebook();
    if (!uid || !notebook || this.activeCell()?.type === type) return;
    this.edit(setCellType(notebook, uid, type));
    // The editor comes back with the new language.
    this.editingUid.set(null);
  }

  clearActiveOutputs(): void {
    const uid = this.activeUid();
    const notebook = this.notebook();
    if (uid && notebook) this.edit(clearOutputs(notebook, uid));
  }

  clearAllOutputs(): void {
    const notebook = this.notebook();
    if (notebook) this.edit(clearOutputs(notebook));
  }

  undo(): void {
    this.travel(this.undoStack, this.redoStack);
  }

  redo(): void {
    this.travel(this.redoStack, this.undoStack);
  }

  // --- run ---------------------------------------------------------------------------------------

  startRun(): void {
    const notebook = this.notebook();
    if (!notebook || !this.runUrl || this.readOnly()) return;
    this.editingUid.set(null);
    this.runNotebook.set(JSON.parse(serializeNotebook(notebook)));
    this.running.set(true);
  }

  /** Back to the cells; the runner saves what is unsaved first. */
  stopRun(): void {
    const runner = this.runner();
    if (runner) runner.requestClose();
    else this.running.set(false);
  }

  /** The runner saved: its notebook, with whatever outputs it produced, is now the file. */
  onRunSaved(content: unknown): void {
    let next: Notebook;
    try {
      next = parseNotebook(JSON.stringify(content));
    } catch (error) {
      console.error('The runner saved something that is not a notebook', error);
      return;
    }
    this.typingIn = null;
    this.remember();
    this.apply(next);
    this.activeUid.set(null);
    this.saveRequested.emit();
  }

  // --- internals ---------------------------------------------------------------------------------

  private travel(from: Notebook[], to: Notebook[]): void {
    const current = this.notebook();
    const target = from.pop();
    if (!current || !target) return;
    to.push(current);
    this.typingIn = null;
    this.apply(target);
    if (!target.cells.some(c => c.uid === this.activeUid())) {
      this.activeUid.set(target.cells[0]?.uid ?? null);
      this.editingUid.set(null);
    }
    this.updateHistoryFlags();
  }

  private edit(next: Notebook): void {
    if (next === this.notebook()) return;
    this.typingIn = null;
    this.remember();
    this.apply(next);
  }

  private remember(): void {
    const current = this.notebook();
    if (!current) return;
    this.undoStack.push(current);
    if (this.undoStack.length > HISTORY_LIMIT) this.undoStack.shift();
    this.redoStack = [];
    this.updateHistoryFlags();
  }

  private apply(next: Notebook): void {
    this.notebook.set(next);
    this.setDirty(true);
    // Back to what was loaded is not dirty; comparing on a timer keeps typing cheap on big notebooks.
    if (this.dirtyTimer) clearTimeout(this.dirtyTimer);
    this.dirtyTimer = setTimeout(() => {
      const notebook = this.notebook();
      if (notebook) this.setDirty(serializeNotebook(notebook) !== this.baseline);
    }, DIRTY_CHECK_MS);
  }

  private updateHistoryFlags(): void {
    this.canUndo.set(this.undoStack.length > 0);
    this.canRedo.set(this.redoStack.length > 0);
  }

  private setDirty(value: boolean): void {
    if (this.dirty() === value) return;
    this.dirty.set(value);
    this.dirtyChange.emit(value);
  }

  private async open(blob: Blob): Promise<void> {
    const generation = ++this.generation;
    this.state.set('loading');
    this.error.set(null);
    this.setDirty(false);
    this.undoStack = [];
    this.redoStack = [];
    this.updateHistoryFlags();
    this.activeUid.set(null);
    this.editingUid.set(null);
    this.running.set(false);
    this.interactiveAllowed.set(false);
    try {
      const text = await blob.text();
      if (generation !== this.generation) return;
      const notebook = parseNotebook(text);
      this.notebook.set(notebook);
      this.baseline = serializeNotebook(notebook);
      this.state.set('ready');
    } catch (error) {
      if (generation !== this.generation) return;
      this.error.set(error instanceof NotebookFormatError ? error.message : 'This notebook could not be opened here.');
      this.state.set('error');
    }
  }

  private onKeydown(event: KeyboardEvent): void {
    const mod = event.ctrlKey || event.metaKey;
    if (mod && !event.shiftKey && event.key.toLowerCase() === 's') {
      event.preventDefault();
      if (!this.readOnly()) this.saveRequested.emit();
      return;
    }
    // Keys in the cell editor are the editor's, apart from those it sends up itself.
    if (this.readOnly() || this.editingUid() || this.running()) return;
    const target = event.target as HTMLElement;
    if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'BUTTON') return;
    const key = event.key;
    if (mod && key.toLowerCase() === 'z') {
      event.preventDefault();
      if (event.shiftKey) this.redo();
      else this.undo();
      return;
    }
    if (mod || event.altKey) return;
    const deleting = this.pendingDelete;
    this.pendingDelete = false;
    const index = this.activeIndex();
    let handled = true;
    if (key === 'ArrowDown' || key === 'j') this.selectAt(index + 1);
    else if (key === 'ArrowUp' || key === 'k') this.selectAt(index - 1);
    else if (key === 'Enter' && this.activeUid()) this.startEditing(this.activeUid()!);
    else if (key === 'a') this.addCell('code', 'above');
    else if (key === 'b') this.addCell('code', 'below');
    else if (key === 'm') this.setActiveType('markdown');
    else if (key === 'y') this.setActiveType('code');
    else if (key === 'd') {
      if (deleting) this.deleteActive();
      else this.pendingDelete = true;
    } else handled = false;
    if (handled) event.preventDefault();
  }

  /** Moves the selection without opening an editor. */
  private selectAt(index: number): void {
    const cell = this.cells()[Math.max(0, Math.min(index, this.cells().length - 1))];
    if (!cell) return;
    this.activeUid.set(cell.uid);
    this.host.nativeElement.querySelector(`[data-cell="${CSS.escape(cell.uid)}"]`)?.scrollIntoView?.({ block: 'nearest' });
  }
}
