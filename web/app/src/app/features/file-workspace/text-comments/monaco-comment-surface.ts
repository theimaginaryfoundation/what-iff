import type * as MonacoApi from 'monaco-editor';

import { TextCommentSurface } from './text-comments-state';

type Range = { start: number; end: number };

/**
 * Comment threads on text in the Monaco editor (ADR 0x045). Each is a decoration that Monaco moves
 * as the text is typed, with Word's semantics for free: typing inside it keeps it, deleting its
 * first or last characters shrinks it, typing at its edges does not grow it, and deleting all of
 * it leaves it empty, which is a thread on the whole file. Offsets are the model's, in UTF-16
 * code units.
 */
export class MonacoCommentSurface implements TextCommentSurface {
  private readonly decorations = new Map<string, string>();
  private state: { activeId: string | null; hidden: ReadonlySet<string> } = { activeId: null, hidden: new Set() };

  constructor(
    private readonly monaco: typeof MonacoApi,
    private readonly editor: MonacoApi.editor.IStandaloneCodeEditor,
    private readonly model: MonacoApi.editor.ITextModel,
  ) {}

  private options(threadId: string): MonacoApi.editor.IModelDecorationOptions {
    const hidden = this.state.hidden.has(threadId);
    const active = this.state.activeId === threadId;
    return {
      stickiness: this.monaco.editor.TrackedRangeStickiness.NeverGrowsWhenTypingAtEdges,
      ...(hidden
        ? {}
        : {
            inlineClassName: active ? 'whatiff-text-comment whatiff-text-comment--active' : 'whatiff-text-comment',
            overviewRuler: { color: '#F2C94C', position: this.monaco.editor.OverviewRulerLane.Right },
          }),
    };
  }

  private toRange(range: Range): MonacoApi.Range {
    const start = this.model.getPositionAt(range.start);
    const end = this.model.getPositionAt(range.end);
    return new this.monaco.Range(start.lineNumber, start.column, end.lineNumber, end.column);
  }

  place(threadId: string, range: Range | null): void {
    const old = this.decorations.get(threadId);
    const next = range && range.end > range.start ? [{ range: this.toRange(range), options: this.options(threadId) }] : [];
    const [id] = this.model.deltaDecorations(old ? [old] : [], next);
    if (id) this.decorations.set(threadId, id);
    else this.decorations.delete(threadId);
  }

  ranges(): Map<string, Range | null> {
    const out = new Map<string, Range | null>();
    for (const [threadId, id] of this.decorations) {
      const at = this.model.getDecorationRange(id);
      if (!at || at.isEmpty()) {
        out.set(threadId, null);
        continue;
      }
      out.set(threadId, { start: this.model.getOffsetAt(at.getStartPosition()), end: this.model.getOffsetAt(at.getEndPosition()) });
    }
    return out;
  }

  paint(state: { activeId: string | null; hidden: ReadonlySet<string> }): void {
    this.state = state;
    // Each decoration is swapped for one with the new look over the range it has now.
    for (const [threadId, id] of [...this.decorations]) {
      const at = this.model.getDecorationRange(id);
      if (!at) continue;
      const [next] = this.model.deltaDecorations([id], [{ range: at, options: this.options(threadId) }]);
      this.decorations.set(threadId, next);
    }
  }

  reveal(range: Range): void {
    this.editor.revealRangeInCenterIfOutsideViewport(this.toRange(range));
  }

  text(range: Range): string {
    return this.model.getValueInRange(this.toRange(range));
  }

  /** The selection as offsets, or the word at the caret when nothing is selected. */
  selectionOrWord(): Range | null {
    const selection = this.editor.getSelection();
    if (!selection) return null;
    if (!selection.isEmpty()) {
      return { start: this.model.getOffsetAt(selection.getStartPosition()), end: this.model.getOffsetAt(selection.getEndPosition()) };
    }
    const position = selection.getPosition();
    const word = this.model.getWordAtPosition(position);
    if (!word) return null;
    return {
      start: this.model.getOffsetAt({ lineNumber: position.lineNumber, column: word.startColumn }),
      end: this.model.getOffsetAt({ lineNumber: position.lineNumber, column: word.endColumn }),
    };
  }

  dispose(): void {
    this.model.deltaDecorations([...this.decorations.values()], []);
    this.decorations.clear();
  }
}
