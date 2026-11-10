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
}

export function fakeMonaco() {
  const models = new Map<string, FakeMonacoModel>();
  const state = { created: 0, commands: [] as (() => void)[], options: [] as object[], colorized: [] as string[], themes: [] as string[] };
  const monaco = {
    languages: { getLanguages: () => [{ id: 'python', extensions: ['.py'] }, { id: 'markdown', extensions: ['.md'] }, { id: 'go', extensions: ['.go'] }] },
    Uri: { parse: (value: string) => ({ toString: () => value }) },
    KeyMod: { CtrlCmd: 2048, Shift: 1024 },
    KeyCode: { KeyS: 49, Enter: 3, Escape: 9 },
    editor: {
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
        };
        models.set(uri.toString(), model);
        return model;
      },
      create: (_el: HTMLElement, options: { model: FakeMonacoModel }) => {
        state.created += 1;
        let current = options.model;
        return {
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
          getContentHeight: () => 40,
          onDidContentSizeChange: () => ({ dispose: () => undefined }),
          layout: () => undefined,
          focus: () => undefined,
          dispose: () => undefined,
        };
      },
      colorize: (text: string, language: string) => {
        state.colorized.push(language);
        return Promise.resolve(`<span class="mtk1">${text.replace(/</g, '&lt;')}</span>`);
      },
    },
  };
  return { monaco, models, state };
}
