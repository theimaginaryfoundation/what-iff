import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { COMMENT_SAVER } from '../file-workspace/editors/file-editor';
import { fakeTextComments } from '../file-workspace/text-comments/fake-text-comments.spec-helper';
import { TextCommentsService } from '../file-workspace/text-comments/text-comments.service';
import { CommentPeopleService } from '../office/comments/comment-people.service';
import { DiagramEditorComponent } from './diagram-editor.component';
import { DIAGRAMS_URL } from './diagram-site.config';

const SITE = 'https://diagrams.example.test/index.html';
const ORIGIN = 'https://diagrams.example.test';
const XML = '<mxfile><diagram id="p1"><mxGraphModel><root><mxCell id="0"/></root></mxGraphModel></diagram></mxfile>';

function file(): FileAttachment {
  return { id: 'f-1', name: 'flow.drawio', file_type: 'application/vnd.jgraph.mxfile' } as FileAttachment;
}

describe('DiagramEditorComponent', () => {
  let fixture: ComponentFixture<DiagramEditorComponent>;
  let posted: { message: Record<string, unknown>; origin: string }[];
  let frameWindow: Window;
  let textComments: ReturnType<typeof fakeTextComments>;

  function setUp(site: string | null) {
    TestBed.configureTestingModule({
      imports: [DiagramEditorComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: DIAGRAMS_URL, useValue: site },
        { provide: CommentPeopleService, useValue: { me: signal({ id: 'user:u-1', name: 'Pat Lee' }) } },
        { provide: TextCommentsService, useValue: (textComments = fakeTextComments()) },
        {
          provide: COMMENT_SAVER,
          useValue: { canComment: signal(true), saveComments: () => Promise.resolve('saved'), showCurrentContent: () => undefined },
        },
      ],
    });
    fixture = TestBed.createComponent(DiagramEditorComponent);
  }

  async function open(readOnly: boolean, text = XML) {
    fixture.componentRef.setInput('file', file());
    fixture.componentRef.setInput('blob', new Blob([text]));
    fixture.componentRef.setInput('readOnly', readOnly);
    fixture.detectChanges();
    await fixture.whenStable();
    const iframe = fixture.nativeElement.querySelector('iframe') as HTMLIFrameElement;
    posted = [];
    frameWindow = {
      postMessage: vi.fn((m: string, origin: string) => posted.push({ message: JSON.parse(m), origin })),
    } as unknown as Window;
    Object.defineProperty(iframe, 'contentWindow', { value: frameWindow });
    return iframe;
  }

  /** A message from the editor frame. */
  async function fromEditor(message: Record<string, unknown>, origin = ORIGIN, source: Window = frameWindow) {
    window.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(message), origin, source }));
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
  }

  beforeEach(() => setUp(SITE));

  it('loads the file into the editor once it is ready, read-only while viewing', async () => {
    const iframe = await open(true);
    expect(iframe.getAttribute('src')).toContain('chrome=0');
    expect(fixture.nativeElement.textContent).toContain('Opening the diagram');
    await fromEditor({ event: 'init' });
    expect(posted).toEqual([{ message: { action: 'load', xml: XML, autosave: 1, title: 'flow.drawio' }, origin: ORIGIN }]);
    await fromEditor({ event: 'load', xml: XML });
    expect(fixture.nativeElement.textContent).not.toContain('Opening the diagram');
    // A viewer takes no changes.
    await fromEditor({ event: 'autosave', xml: '<mxfile>changed</mxfile>' });
    expect(fixture.componentInstance.dirty()).toBe(false);
  });

  it('says so when the editor does not load, instead of waiting forever', async () => {
    vi.useFakeTimers();
    try {
      TestBed.resetTestingModule();
      setUp(SITE);
      fixture.componentRef.setInput('file', file());
      fixture.componentRef.setInput('blob', new Blob([XML]));
      fixture.detectChanges();
      vi.advanceTimersByTime(20_000);
      fixture.detectChanges();
      expect(fixture.nativeElement.textContent).toContain("didn't load from " + ORIGIN);
    } finally {
      vi.useRealTimers();
    }
  });

  it('downloads the diagram as an image the editor makes', async () => {
    const saved: { blob: Blob; name: string }[] = [];
    let blob: Blob | null = null;
    const createUrl = vi.spyOn(URL, 'createObjectURL').mockImplementation(b => ((blob = b as Blob), 'blob:x'));
    const revokeUrl = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved.push({ blob: blob!, name: this.download });
    });
    await open(true);
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    posted.length = 0;
    const done = fixture.componentInstance.download('png');
    expect(fixture.componentInstance.exporting()).toBe('png');
    const request = posted[0].message;
    expect(request).toMatchObject({ action: 'export', format: 'xmlpng', scale: 2 });
    // Another request's answer is not this one's.
    await fromEditor({ event: 'export', format: 'xmlpng', data: 'data:image/png;base64,AAAA', message: { requestId: 'other' } });
    expect(saved).toEqual([]);
    await fromEditor({ event: 'export', format: 'xmlpng', data: 'data:image/png;base64,iVBORw==', message: request });
    await done;
    expect(saved.map(d => [d.name, d.blob.type])).toEqual([['flow.png', 'image/png']]);
    expect(fixture.componentInstance.exporting()).toBeNull();
    expect(fixture.componentInstance.exportFailed()).toBe(false);
    for (const spy of [createUrl, revokeUrl, click]) spy.mockRestore();
  });

  it('says the download failed when the editor gives back no image', async () => {
    await open(true);
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    posted.length = 0;
    const done = fixture.componentInstance.download('svg');
    const request = posted[0].message;
    expect(request).toMatchObject({ action: 'export', format: 'xmlsvg' });
    await fromEditor({ event: 'export', format: 'xmlsvg', data: 'not a data URL', message: request });
    await done;
    expect(fixture.componentInstance.exportFailed()).toBe(true);
    expect(fixture.componentInstance.exporting()).toBeNull();
  });

  it('comments on an element while viewing, written at once, and marks it in the editor', async () => {
    await open(true);
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    await fromEditor({ event: 'whatiff-page', page: 'p1' });
    posted.length = 0;
    // The right-click menu's New comment on the User box.
    await fromEditor({ event: 'whatiff-menu', item: 'new', cell: 'a', page: 'p1', label: 'User' });
    const editor = fixture.componentInstance;
    expect(editor.comments.draft()).toMatchObject({ cell: { id: 'a', page: 'p1' }, quote: 'User' });
    expect(posted.at(-1)?.message).toEqual({ action: 'whatiff-marks', page: 'p1', marks: { a: 'active' } });

    editor.comments.create({ text: 'Who is this?', mentions: [] });
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    expect(textComments.state.puts).toHaveLength(1);
    expect(textComments.state.puts[0].threads[0]).toMatchObject({ anchor: null, cell: { id: 'a', page: 'p1' }, quote: 'User' });
    expect(editor.dirty()).toBe(false);

    // Clicking the element chooses its thread; choosing it in the panel shows the element.
    editor.comments.activeId.set(null);
    await fromEditor({ event: 'whatiff-click', cell: 'a', page: 'p1', label: 'User' });
    const threadId = textComments.state.puts[0].threads[0].id;
    expect(editor.comments.activeId()).toBe(threadId);
    posted.length = 0;
    editor.cellComments.selectThread(threadId);
    expect(posted.map(p => p.message)).toContainEqual({ action: 'whatiff-reveal', cell: 'a', page: 'p1' });
  });

  it('replies to and deletes threads from the menu, and marks only the page shown', async () => {
    await open(true);
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    await fromEditor({ event: 'whatiff-page', page: 'p1' });
    await fromEditor({ event: 'whatiff-menu', item: 'new', cell: 'a', page: 'p1', label: 'User' });
    const editor = fixture.componentInstance;
    editor.comments.create({ text: 'Who is this?', mentions: [] });
    await fixture.whenStable();
    const threadId = textComments.state.puts[0].threads[0].id;
    editor.comments.activeId.set(null);

    // Another page shows none of this page's marks; back on it, the thread's element is marked.
    await fromEditor({ event: 'whatiff-page', page: 'p2' });
    expect(posted.at(-1)?.message).toEqual({ action: 'whatiff-marks', page: 'p2', marks: {} });
    await fromEditor({ event: 'whatiff-page', page: 'p1' });
    expect(posted.at(-1)?.message).toEqual({ action: 'whatiff-marks', page: 'p1', marks: { a: 'open' } });

    editor.closeComments();
    expect(editor.commentsOpen()).toBe(false);
    // Reply on an element with no thread does nothing; on the User box it opens that thread's reply.
    await fromEditor({ event: 'whatiff-menu', item: 'reply', cell: 'z', page: 'p1' });
    expect(editor.cellComments.replyRequest()).toBeNull();
    await fromEditor({ event: 'whatiff-menu', item: 'reply', cell: 'a', page: 'p1' });
    expect(editor.comments.activeId()).toBe(threadId);
    expect(editor.cellComments.replyRequest()).toEqual({ threadId });
    expect(editor.commentsOpen()).toBe(true);
    editor.toggleComments();
    expect(editor.commentsOpen()).toBe(false);

    await fromEditor({ event: 'whatiff-menu', item: 'delete', cell: 'a', page: 'p1' });
    await new Promise(resolve => setTimeout(resolve));
    expect(textComments.state.puts.at(-1)?.threads).toEqual([]);
    expect(editor.comments.activeId()).toBeNull();
  });

  it('comments on the whole diagram when no single element is chosen', async () => {
    await open(true);
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    await fromEditor({
      event: 'whatiff-selection',
      page: 'p1',
      cells: [
        { id: 'a', label: 'User' },
        { id: 'b', label: 'Agent' },
      ],
    });
    const editor = fixture.componentInstance;
    editor.cellComments.startComment();
    expect(editor.comments.draft()).toBeTruthy();
    expect(editor.comments.draft()?.cell).toBeUndefined();
    expect(editor.commentsOpen()).toBe(true);
  });

  it('writes comments made while editing once the save has stored the diagram', async () => {
    await open(false);
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    const editor = fixture.componentInstance;
    await fromEditor({ event: 'whatiff-selection', page: 'p1', cells: [{ id: 'b', label: 'Agent' }] });
    editor.cellComments.startComment();
    editor.comments.create({ text: 'Rename?', mentions: [] });
    expect(editor.dirty()).toBe(true);
    expect(textComments.state.puts).toHaveLength(0);

    const saved = await editor.content();
    editor.markSaved(saved);
    await editor.saved({ ...file(), content_version: 2 } as FileAttachment, saved);
    expect(textComments.state.puts).toHaveLength(1);
    expect(textComments.state.puts[0]).toMatchObject({ contentVersion: 2 });
    expect(textComments.state.puts[0].threads[0]).toMatchObject({ cell: { id: 'b', page: 'p1' }, quote: 'Agent' });
    expect(editor.dirty()).toBe(false);
  });

  it('tracks changes while editing, hands out the latest XML, and is clean once saved', async () => {
    const iframe = await open(false);
    expect(iframe.getAttribute('src')).not.toContain('chrome=0');
    const dirty: boolean[] = [];
    fixture.componentInstance.dirtyChange.subscribe(d => dirty.push(d));
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    await fromEditor({ event: 'autosave', xml: '<mxfile>v2</mxfile>' });
    expect(fixture.componentInstance.dirty()).toBe(true);
    const saved = await fixture.componentInstance.content();
    expect(await saved.text()).toBe('<mxfile>v2</mxfile>');
    expect(saved.type).toBe('application/vnd.jgraph.mxfile');
    fixture.componentInstance.markSaved(saved);
    expect(fixture.componentInstance.dirty()).toBe(false);
    expect(dirty).toEqual([true, false]);

    // The workspace hands back the saved bytes: nothing is loaded again.
    posted.length = 0;
    fixture.componentRef.setInput('blob', saved);
    fixture.detectChanges();
    await fixture.whenStable();
    expect(posted).toEqual([]);
  });

  it('ignores messages from anywhere but the editor frame', async () => {
    await open(false);
    await fromEditor({ event: 'init' }, 'https://evil.example.test');
    await fromEditor({ event: 'init' }, ORIGIN, {} as Window);
    expect(posted).toEqual([]);
  });

  it('asks the workspace to save on the editor’s save shortcut', async () => {
    await open(false);
    const save = vi.fn();
    fixture.componentInstance.saveRequested.subscribe(save);
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    await fromEditor({ event: 'save', xml: '<mxfile>v3</mxfile>' });
    expect(save).toHaveBeenCalledTimes(1);
    expect(fixture.componentInstance.dirty()).toBe(true);
  });

  it('says so when this server has no diagram editor', async () => {
    TestBed.resetTestingModule();
    setUp(null);
    fixture.componentRef.setInput('file', file());
    fixture.componentRef.setInput('blob', new Blob([XML]));
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('iframe')).toBeNull();
    expect(fixture.nativeElement.textContent).toContain('no diagram editor set up');
  });
});
