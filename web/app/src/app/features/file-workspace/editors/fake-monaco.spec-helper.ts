/** A Monaco double for specs: models hold text, editors forward commands, colorize wraps spans. */
export interface FakeMonacoModel {
  uri: { toString(): string };
  value: string;
  listeners: (() => void)[];
  getValue(): string;
  setValue(v: string): void;
  onDidChangeContent(fn: () => void): { dispose(): void };
  dispose(): void;
  /** Test hook: simulate typing. */
  type(v: string): void;
  /** Decorations by id, as offsets: the fake keeps the whole text on one line, so a column is an offset + 1. */
  decorations: Map<string, { start: number; end: number; options: { inlineClassName?: string } }>;
  [key: string]: unknown;
}

/** A position or range in the fake's one-line coordinates. */
function fakeRange(start: number, end: number) {
  return {
    startLineNumber: 1,
    startColumn: start + 1,
    endLineNumber: 1,
    endColumn: end + 1,
    isEmpty: () => end <= start,
    getStartPosition: () => ({ lineNumber: 1, column: start + 1 }),
    getEndPosition: () => ({ lineNumber: 1, column: end + 1 }),
    getPosition: () => ({ lineNumber: 1, column: end + 1 }),
  };
}

export function fakeMonaco() {
  const models = new Map<string, FakeMonacoModel>();
  let decorationIds = 0;
  const state = {
    created: 0,
    commands: [] as (() => void)[],
    options: [] as object[],
    colorized: [] as string[],
    themes: [] as string[],
    selectionListeners: [] as ((e: { selection: ReturnType<typeof fakeRange>; source: string }) => void)[],
    actions: [] as { id: string; run: () => void }[],
    editors: [] as { selection: unknown }[],
  };
  const monaco = {
    languages: {
      getLanguages: () => [
        { id: 'python', extensions: ['.py'] },
        { id: 'markdown', extensions: ['.md'] },
        { id: 'go', extensions: ['.go'] },
      ],
    },
    Uri: { parse: (value: string) => ({ toString: () => value }) },
    KeyMod: { CtrlCmd: 2048, Shift: 1024, Alt: 512 },
    KeyCode: { KeyS: 49, Enter: 3, Escape: 9, KeyM: 43 },
    Range: class {
      constructor(_l1: number, c1: number, _l2: number, c2: number) {
        return fakeRange(c1 - 1, c2 - 1);
      }
    },
    editor: {
      TrackedRangeStickiness: { NeverGrowsWhenTypingAtEdges: 1 },
      OverviewRulerLane: { Right: 4 },
      defineTheme: () => undefined,
      setTheme: (name: string) => {
        state.themes.push(name);
      },
      getModel: (uri: { toString(): string }) => models.get(uri.toString()) ?? null,
      createModel: (value: string, _language: string, uri: { toString(): string }) => {
        const model: FakeMonacoModel = {
          uri,
          value,
          listeners: [],
          getValue: () => model.value,
          setValue: (v: string) => {
            model.value = v;
          },
          onDidChangeContent: fn => {
            model.listeners.push(fn);
            return { dispose: () => undefined };
          },
          dispose: () => {
            models.delete(uri.toString());
          },
          type: (v: string) => {
            model.value = v;
            model.listeners.forEach(fn => fn());
          },
          decorations: new Map(),
          getPositionAt: (offset: number) => ({ lineNumber: 1, column: offset + 1 }),
          getOffsetAt: (position: { column: number }) => position.column - 1,
          getValueInRange: (range: { startColumn: number; endColumn: number }) =>
            model.value.slice(range.startColumn - 1, range.endColumn - 1),
          getWordAtPosition: (position: { column: number }) => {
            const at = position.column - 1;
            const before = /\w*$/.exec(model.value.slice(0, at))![0];
            const after = /^\w*/.exec(model.value.slice(at))![0];
            return before || after ? { word: before + after, startColumn: at - before.length + 1, endColumn: at + after.length + 1 } : null;
          },
          deltaDecorations: (old: string[], next: { range: { startColumn: number; endColumn: number }; options: object }[]) => {
            old.forEach(id => model.decorations.delete(id));
            return next.map(d => {
              const id = `d${++decorationIds}`;
              model.decorations.set(id, { start: d.range.startColumn - 1, end: d.range.endColumn - 1, options: d.options });
              return id;
            });
          },
          getDecorationRange: (id: string) => {
            const d = model.decorations.get(id);
            return d ? fakeRange(d.start, d.end) : null;
          },
        };
        models.set(uri.toString(), model);
        return model;
      },
      create: (_el: HTMLElement, options: { model: FakeMonacoModel }) => {
        state.created += 1;
        let current = options.model;
        const editor = {
          addCommand: (_key: number, fn: () => void) => {
            state.commands.push(fn);
          },
          updateOptions: (o: object) => {
            state.options.push(o);
          },
          setModel: (m: FakeMonacoModel) => {
            current = m;
          },
          getModel: () => current,
          selection: fakeRange(0, 0),
          createContextKey: () => ({ set: () => undefined }),
          onDidChangeCursorSelection: (fn: (e: { selection: ReturnType<typeof fakeRange>; source: string }) => void) => {
            state.selectionListeners.push(fn);
            return { dispose: () => undefined };
          },
          addAction: (action: { id: string; run: () => void }) => {
            state.actions.push(action);
            return { dispose: () => undefined };
          },
          getSelection() {
            return this.selection;
          },
          getPosition() {
            return this.selection.getPosition();
          },
          revealRangeInCenterIfOutsideViewport: () => undefined,
          getContentHeight: () => 40,
          onDidContentSizeChange: () => ({ dispose: () => undefined }),
          layout: () => undefined,
          focus: () => undefined,
          dispose: () => undefined,
        };
        state.editors.push(editor);
        return editor;
      },
      colorize: (text: string, language: string) => {
        state.colorized.push(language);
        return Promise.resolve(`<span class="mtk1">${text.replace(/</g, '&lt;')}</span>`);
      },
    },
  };
  return { monaco, models, state };
}

/** Selects [start, end) in a fake editor, as the user would. */
export function selectInFake(editor: { selection: unknown }, start: number, end: number): void {
  editor.selection = fakeRange(start, end);
}
