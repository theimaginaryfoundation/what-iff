import { signal } from '@angular/core';
import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { fakeTextComments } from '../file-workspace/text-comments/fake-text-comments.spec-helper';
import { TextCommentSurface, TextCommentsState } from '../file-workspace/text-comments/text-comments-state';
import { TextCommentThread, TextCommentsService } from '../file-workspace/text-comments/text-comments.service';
import { PageBridgeAction } from './page-bridge';
import { PageComments } from './page-comments';

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
    expect(state.draft()).toMatchObject({ range: TAG, quote: '<h1 id="title">', element: { page: 'page-1', selector: '#title' } });
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
    expect(state.draft()).toMatchObject({ range: null, quote: 'On the page · li “Two”', element: { selector: 'li' } });
    comments.handle({ type: 'picked', selector: 'p', fingerprint: { tag: 'p' }, src: 'other-file:0-3', created: null, label: 'p' });
    expect(state.draft()).toMatchObject({ range: null, element: { selector: 'p' } });
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
});
