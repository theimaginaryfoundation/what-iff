import { signal } from '@angular/core';
import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { fakeTextComments } from '../file-workspace/text-comments/fake-text-comments.spec-helper';
import { TextCommentSurface, TextCommentsState } from '../file-workspace/text-comments/text-comments-state';
import { TextCommentThread, TextCommentsService } from '../file-workspace/text-comments/text-comments.service';
import { PageBridgeAction, PageProject } from './page-bridge';
import { PageComments, lineRange } from './page-comments';

const HTML = '<main><h1 id="title">Hello</h1><ul id="data"></ul></main>';
const TAG = { start: 6, end: 21 }; // <h1 id="title">
const page = { id: 'page-1', name: 'index.html' } as FileAttachment;

async function setup(threads: TextCommentThread[] = []) {
  const ranges = new Map<string, { start: number; end: number } | null>();
  const surface: TextCommentSurface = {
    place: (id, range) => (range ? ranges.set(id, range) : ranges.delete(id)),
    ranges: () => new Map(ranges),
    paint: () => undefined,
    reveal: () => undefined,
    text: range => HTML.slice(range.start, range.end),
  };
  const api = fakeTextComments({ contentVersion: 1, revision: 1, threads });
  const state = new TextCommentsState(api as unknown as TextCommentsService, signal({ id: 'user:u-1', name: 'Pat Lee' }), () => undefined);
  state.attach(surface);
  await state.load(page.id);
  const sent: PageBridgeAction[] = [];
  const openPanel = vi.fn();
  const canComment = signal(true);
  const comments = new PageComments(
    state,
    a => sent.push(a),
    openPanel,
    () => canComment(),
    () => page,
  );
  return { state, comments, sent, openPanel, canComment };
}

describe('PageComments', () => {
  it('marks threads by their code anchor in this file and their element on this page', async () => {
    const { comments, state } = await setup([
      { id: 'tag', anchor: { ...TAG }, quote: '<h1 id="title">', comments: [] },
      {
        id: 'made',
        anchor: null,
        quote: 'li',
        element: { page: 'page-1', selector: '#data > li:nth-of-type(2)', fingerprint: { tag: 'li', text: 'Two' } },
        comments: [],
      },
      { id: 'other-page', anchor: null, quote: 'x', element: { page: 'page-2', selector: 'p' }, comments: [] },
      { id: 'done', anchor: { ...TAG }, quote: '<h1 id="title">', resolved: true, comments: [] },
      { id: 'whole', anchor: null, quote: '', comments: [] },
    ]);
    state.activeId.set('made');
    expect(comments.marks()).toEqual([
      { id: 'tag', state: 'open', src: 'page-1:6-21' },
      { id: 'made', state: 'active', selector: '#data > li:nth-of-type(2)', fingerprint: { tag: 'li', text: 'Two' } },
    ]);
  });

  it('starts a thread on a picked element: on its tag too when this file declares it', async () => {
    const { comments, state, sent, openPanel } = await setup();
    comments.togglePick();
    expect(comments.picking()).toBe(true);
    expect(sent).toEqual([{ type: 'pick' }]);

    comments.handle({
      type: 'picked',
      selector: '#title',
      fingerprint: { tag: 'h1', text: 'Hello', attrs: { id: 'title' } },
      src: 'page-1:6-21',
      created: null,
      label: 'h1#title “Hello”',
    });
    expect(comments.picking()).toBe(false);
    expect(openPanel).toHaveBeenCalled();
    await vi.waitFor(() =>
      expect(state.draft()).toMatchObject({ range: TAG, quote: '<h1 id="title">', element: { page: 'page-1', selector: '#title' } }),
    );
    // The draft is marked on the page as the active thread.
    expect(comments.marks()).toEqual([
      {
        id: 'draft',
        state: 'active',
        src: 'page-1:6-21',
        selector: '#title',
        fingerprint: { tag: 'h1', text: 'Hello', attrs: { id: 'title' } },
      },
    ]);

    state.create({ text: 'Friendlier?', mentions: [] });
    expect(state.threads()[0]).toMatchObject({ anchor: TAG, element: { selector: '#title' } });
  });

  it('starts a thread on the element alone when a script made it, or another file declares it', async () => {
    const { comments, state } = await setup();
    comments.handle({
      type: 'picked',
      selector: 'li',
      fingerprint: { tag: 'li' },
      src: null,
      created: { path: 'app.js', line: 3, column: 9 },
      label: 'li “Two”',
    });
    // No project to find the script in (a page on its own): on the element alone, kept with the page.
    await vi.waitFor(() =>
      expect(state.draft()).toMatchObject({ range: null, quote: 'On the page · li “Two”', element: { selector: 'li' } }),
    );
    comments.handle({ type: 'picked', selector: 'p', fingerprint: { tag: 'p' }, src: 'other-file:0-3', created: null, label: 'p' });
    await vi.waitFor(() => expect(state.draft()).toMatchObject({ range: null, element: { selector: 'p' } }));
  });

  it('cannot pick where the user may not comment, and leaves picking on a second click or Esc', async () => {
    const { comments, sent, canComment } = await setup();
    canComment.set(false);
    comments.togglePick();
    expect(comments.picking()).toBe(false);
    canComment.set(true);
    comments.togglePick();
    comments.togglePick();
    expect(sent).toEqual([{ type: 'pick' }, { type: 'stop-pick' }]);
    comments.togglePick();
    comments.handle({ type: 'pick-cancelled' });
    expect(comments.picking()).toBe(false);
  });

  it('shows the marks again to a page that reloaded, and chooses a thread from its mark', async () => {
    const { comments, state, sent, openPanel } = await setup([{ id: 'tag', anchor: { ...TAG }, quote: '<h1 id="title">', comments: [] }]);
    comments.handle({ type: 'ready' });
    expect(sent).toEqual([{ type: 'marks', marks: [{ id: 'tag', state: 'open', src: 'page-1:6-21' }] }]);
    comments.handle({ type: 'select', id: 'tag' });
    expect(state.activeId()).toBe('tag');
    expect(openPanel).toHaveBeenCalled();
    comments.handle({ type: 'unplaced', ids: ['tag', 'draft'] });
    expect([...comments.unplaced()]).toEqual(['tag']);
    comments.reveal('tag');
    expect(sent.at(-1)).toEqual({ type: 'reveal', id: 'tag' });
  });

  it('keeps a thread on an element a script made with the script, on the line that made it', async () => {
    const { state } = await setup();
    const script = { id: 'js-1', name: 'app.js', folder: 'site' } as FileAttachment;
    const SCRIPT = 'const ul = document.querySelector("#data");\n  ul.innerHTML = items.map(li).join("");\n';
    const scriptApi = fakeTextComments({ contentVersion: 1, revision: 0, threads: [] });
    let scriptState: TextCommentsState | null = null;
    const project: PageProject = {
      servedFiles: () => new Map([['js/app.js', script]]),
      fileAt: async path => (path === 'js/app.js' ? script : null),
      text: async () => SCRIPT,
      sourceLine: async (_path, line) => line,
    };
    const sent: PageBridgeAction[] = [];
    const comments = new PageComments(
      state,
      a => sent.push(a),
      () => undefined,
      () => true,
      () => page,
      () => project,
      () =>
        (scriptState = new TextCommentsState(scriptApi as unknown as TextCommentsService, signal({ id: 'user:u-1', name: 'Pat Lee' }), () =>
          scriptState!.autosave(),
        )),
    );
    comments.handle({
      type: 'picked',
      selector: '#data > li:nth-of-type(2)',
      fingerprint: { tag: 'li', text: 'Two' },
      src: null,
      created: { path: 'js/app.js', line: 2, column: 6 },
      label: 'li “Two”',
    });
    await vi.waitFor(() => expect(scriptState?.draft()).toBeTruthy());
    // On the call-site line, without its indentation, and on the element.
    expect(scriptState!.draft()).toMatchObject({
      range: { start: 46, end: 84 },
      element: { page: 'page-1', selector: '#data > li:nth-of-type(2)' },
    });
    expect(comments.draftQuote()).toBe('ul.innerHTML = items.map(li).join("");');
    expect(state.draft()).toBeNull();
    // The draft is marked on the page by its code in the script (which no element carries here) and its element.
    expect(comments.marks()).toEqual([
      { id: 'draft', state: 'active', src: 'js-1:46-84', selector: '#data > li:nth-of-type(2)', fingerprint: { tag: 'li', text: 'Two' } },
    ]);

    comments.create({ text: 'Typo?', mentions: [] });
    await vi.waitFor(() => expect(scriptApi.state.puts).toHaveLength(1));
    const thread = scriptApi.state.puts[0].threads[0];
    expect(thread).toMatchObject({ anchor: { start: 46, end: 84 }, element: { page: 'page-1' } });
    // The panel lists it with its file; a reply goes to the script's comments.
    expect(comments.shown().map(t => t.quote)).toEqual(['js/app.js · ul.innerHTML = items.map(li).join("");']);
    comments.reply({ threadId: thread.id, draft: { text: 'Fixed', mentions: [] } });
    await vi.waitFor(() => expect(scriptApi.state.puts.at(-1)?.threads[0].comments).toHaveLength(2));
    expect(comments.activeId()).toBe(thread.id);
    expect(comments.marks()).toEqual([
      { id: thread.id, state: 'active', src: 'js-1:46-84', selector: '#data > li:nth-of-type(2)', fingerprint: { tag: 'li', text: 'Two' } },
    ]);
  });

  it('keeps a thread on an element JSX made with the component, on its tag, and maps a compiled script’s call site', async () => {
    const { state } = await setup();
    const tsx = { id: 'tsx-1', name: 'App.tsx', folder: 'site' } as FileAttachment;
    const ts = { id: 'ts-1', name: 'list.ts', folder: 'site' } as FileAttachment;
    const TSX = 'export const App = () => (\n  <h1 className="t">Hi</h1>\n);\n';
    const TS = 'const a: number = 1;\nconst li = document.createElement("li");\n';
    const apis = new Map<string, ReturnType<typeof fakeTextComments>>();
    const states = new Map<string, TextCommentsState>();
    const project: PageProject = {
      servedFiles: () =>
        new Map([
          ['App.tsx', tsx],
          ['list.ts', ts],
        ]),
      fileAt: async path => (path === 'App.tsx' ? tsx : path === 'list.ts' ? ts : null),
      text: async file => (file.id === 'tsx-1' ? TSX : TS),
      // The compiled list.ts has the call on its line 5.
      sourceLine: async (path, line) => (path === 'list.ts' && line === 5 ? 2 : null),
    };
    const comments = new PageComments(
      state,
      () => undefined,
      () => undefined,
      () => true,
      () => page,
      () => project,
      file => {
        const api = fakeTextComments({ contentVersion: 1, revision: 0, threads: [] });
        apis.set(file.id, api);
        const s: TextCommentsState = new TextCommentsState(
          api as unknown as TextCommentsService,
          signal({ id: 'user:u-1', name: 'Pat Lee' }),
          () => s.autosave(),
        );
        states.set(file.id, s);
        return s;
      },
    );
    // The h1's data-wi-src is its JSX tag in App.tsx.
    comments.handle({
      type: 'picked',
      selector: 'h1.t',
      fingerprint: { tag: 'h1', text: 'Hi' },
      src: 'tsx-1:29-47',
      created: null,
      label: 'h1 “Hi”',
    });
    await vi.waitFor(() => expect(states.get('tsx-1')?.draft()).toBeTruthy());
    expect(comments.draftQuote()).toBe('<h1 className="t">');
    expect(comments.marks()).toEqual([
      { id: 'draft', state: 'active', src: 'tsx-1:29-47', selector: 'h1.t', fingerprint: { tag: 'h1', text: 'Hi' } },
    ]);
    comments.cancelDraft();

    // A li list.ts made, called from line 5 of its compiled module: line 2 of list.ts.
    comments.handle({
      type: 'picked',
      selector: 'li',
      fingerprint: { tag: 'li' },
      src: null,
      created: { path: 'list.ts', line: 5, column: 12 },
      label: 'li',
    });
    await vi.waitFor(() => expect(states.get('ts-1')?.draft()).toBeTruthy());
    expect(comments.draftQuote()).toBe('const li = document.createElement("li");');
    comments.cancelDraft();

    // A call site the map does not place: the thread is on the element alone, in the page's file.
    comments.handle({
      type: 'picked',
      selector: 'li',
      fingerprint: { tag: 'li' },
      src: null,
      created: { path: 'list.ts', line: 9, column: 1 },
      label: 'li',
    });
    await vi.waitFor(() => expect(state.draft()).toBeTruthy());
    expect(state.draft()).toMatchObject({ element: { page: 'page-1', selector: 'li' } });
  });

  it('adopts a draft started on the page’s own text', async () => {
    const { comments, state } = await setup();
    state.start({ ...TAG });
    expect(comments.draftQuote()).toBe('<h1 id="title">');
    comments.create({ text: 'From the source', mentions: [] });
    expect(state.threads()[0]).toMatchObject({ anchor: TAG });
  });
});

describe('lineRange', () => {
  it('is a line without its indentation, or null for a blank or missing one', () => {
    expect(lineRange('a\n  bc  \r\nd', 2)).toEqual({ start: 4, end: 6 });
    expect(lineRange('a\n   \nb', 2)).toBeNull();
    expect(lineRange('a', 3)).toBeNull();
    expect(lineRange('a', 0)).toBeNull();
  });
});
