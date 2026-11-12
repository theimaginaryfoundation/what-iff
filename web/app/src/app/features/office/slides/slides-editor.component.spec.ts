import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import PptxGenJS from 'pptxgenjs';
import { parse } from 'pptxtojson';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { CommentPeopleService } from '../comments/comment-people.service';
import { COMMENT_SAVER, CommentSaver } from '../../file-workspace/editors/file-editor';
import { CommentsPanelState } from '../comments/comments-panel-state.service';
import { normalizePackage } from '../pptx/pptx-export';
import { OoxmlPackage } from '../shared/ooxml';
import { pptxToDeck } from '../pptx/pptx-import';
import { PPTX_PARSER, PPTX_WRITER } from '../pptx/pptx-loader';
import { PPTX_MIME, SlidesEditorComponent } from './slides-editor.component';

async function samplePptx(): Promise<Blob> {
  const pptx = new PptxGenJS();
  pptx.layout = 'LAYOUT_16x9';
  const s1 = pptx.addSlide();
  s1.addText('Title one', { x: 0.5, y: 0.5, w: 9, h: 1, fontSize: 32, bold: true });
  s1.addNotes('Notes for one');
  const s2 = pptx.addSlide();
  s2.addText('Title two', { x: 0.5, y: 0.5, w: 9, h: 1, fontSize: 32 });
  const bytes = await normalizePackage(new Uint8Array((await pptx.write({ outputType: 'arraybuffer' })) as ArrayBuffer));
  return new Blob([bytes as BlobPart], { type: PPTX_MIME });
}

function attachment(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u-1', name: 'deck.pptx', file_type: PPTX_MIME, created_at: '2026-05-01T00:00:00Z', ...overrides };
}

/** The comment saver the next editor gets, if any. */
let testSaver: CommentSaver | null = null;

describe('SlidesEditorComponent', () => {
  let fixture: ComponentFixture<SlidesEditorComponent>;

  beforeEach(async () => {
    testSaver = null;
    await TestBed.configureTestingModule({
      imports: [SlidesEditorComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: PPTX_PARSER, useValue: () => Promise.resolve(parse) },
        { provide: PPTX_WRITER, useValue: async () => new PptxGenJS() },
        { provide: CommentPeopleService, useValue: { me: signal({ id: 'user:u-1', name: 'Pat Lee' }) } },
        // The workspace's comment saver, when a test gives one (comments made while viewing).
        { provide: COMMENT_SAVER, useFactory: () => testSaver },
      ],
    }).compileComponents();
  });

  async function open(blob: Blob, readOnly?: boolean): Promise<HTMLElement> {
    fixture = TestBed.createComponent(SlidesEditorComponent);
    fixture.componentRef.setInput('file', attachment());
    fixture.componentRef.setInput('blob', blob);
    if (readOnly !== undefined) fixture.componentRef.setInput('readOnly', readOnly);
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).not.toBe('loading'));
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  const key = (el: HTMLElement, key: string, init: KeyboardEventInit = {}) =>
    el.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init }));

  it('shows the deck read-only with a rail, a pager and keyboard navigation', async () => {
    const el = await open(await samplePptx());
    expect(fixture.componentInstance.state()).toBe('ready');
    expect(el.querySelectorAll('.slides__thumb')).toHaveLength(2);
    expect(el.querySelector('.slides__toolbar')).toBeNull();
    expect(el.querySelector('.slides__status-text')?.textContent).toContain('Slide 1 of 2');
    expect(el.querySelector('.slides__canvas .slide__text')?.textContent).toBe('Title one');
    expect(el.querySelector('.slides__notes-text')?.textContent).toBe('Notes for one');
    key(el, 'ArrowRight');
    fixture.detectChanges();
    expect(fixture.componentInstance.slideIndex()).toBe(1);
    expect(el.querySelector('.slides__canvas .slide__text')?.textContent).toBe('Title two');
    expect(el.querySelector('.slides__notes')).toBeNull();
    key(el, 'Home');
    expect(fixture.componentInstance.slideIndex()).toBe(0);
    (el.querySelectorAll('.slides__thumb')[1] as HTMLButtonElement).click();
    expect(fixture.componentInstance.slideIndex()).toBe(1);
  });

  it('edits: inserts, nudges, formats, undoes, and saves a deck that reads back with the changes', async () => {
    const dirty = vi.fn();
    const sample = await samplePptx();
    const el = await open(sample, false);
    fixture.componentInstance.dirtyChange.subscribe(dirty);
    expect(el.querySelector('.slides__toolbar')).not.toBeNull();
    const c = fixture.componentInstance;

    c.insertTextBox();
    expect(c.dirty()).toBe(true);
    expect(dirty).toHaveBeenCalledWith(true);
    const inserted = c.selected()!;
    expect(inserted.kind).toBe('shape');
    expect(c.slide().elements).toHaveLength(2);

    key(el, 'ArrowRight', { shiftKey: true });
    expect(c.selected()!.left).toBe(inserted.left + 10);
    c.toggleRunStyle('bold');
    c.setFontSize(40);
    c.setAlign('center');
    c.setFill('#00ff00');
    expect(c.selectedBold()).toBe(true);
    expect(c.selectedFontSize()).toBe(40);
    expect(c.selectedAlign()).toBe('center');
    expect(c.selectedFill()).toBe('#00FF00');

    c.undo();
    expect(c.selectedFill()).toBe('#FFFFFF');
    c.redo();
    expect(c.selectedFill()).toBe('#00FF00');

    c.onTextCommit({ id: inserted.id, paragraphs: [{ align: 'center', runs: [{ text: 'Typed', bold: true, fontSize: 40 }] }] });
    expect(c.editingId()).toBeNull();

    c.insertShape('ellipse');
    c.insertLine();
    expect(c.slide().elements).toHaveLength(4);
    c.deleteSelected();
    expect(c.slide().elements).toHaveLength(3);

    c.addSlide();
    expect(c.deck().slides).toHaveLength(3);
    expect(c.slideIndex()).toBe(1);
    c.setBackground('#112233');
    c.setNotes('New notes');
    c.deleteSlide();
    expect(c.deck().slides).toHaveLength(2);

    const blob = await c.content();
    expect(blob.type).toBe(PPTX_MIME);
    // The save patched the file it was read from: its master, layout and theme are untouched.
    const original = await OoxmlPackage.open(await sample.arrayBuffer());
    const saved = await OoxmlPackage.open(await blob.arrayBuffer());
    for (const path of ['ppt/slideMasters/slideMaster1.xml', 'ppt/slideLayouts/slideLayout1.xml', 'ppt/theme/theme1.xml'])
      expect(await saved.text(path), path).toBe(await original.text(path));
    const back = await pptxToDeck(await blob.arrayBuffer(), { id: 'x', name: 'x.pptx' }, parse);
    expect(back.deck.slides).toHaveLength(2);
    const texts = back.deck.slides[0].elements
      .filter(e => e.kind === 'shape')
      .map(e => (e.kind === 'shape' ? e.text?.paragraphs[0] : null));
    expect(texts[1]).toMatchObject({ align: 'center', runs: [{ text: 'Typed', bold: true, fontSize: 40 }] });
    expect(back.deck.slides[0].elements.find(e => e.kind === 'shape' && e.shapeType === 'ellipse')).toMatchObject({
      fill: { type: 'color', color: '#4472C4' },
    });
    c.markSaved();
    expect(c.dirty()).toBe(false);

    // A second save in the same session: a shape added before the first save is moved, another is
    // deleted, and neither duplicates nor lingers.
    c.goTo(0);
    c.selectedId.set(inserted.id);
    key(el, 'ArrowRight', { shiftKey: true });
    const ellipse = c.slide().elements.find(e => e.kind === 'shape' && e.shapeType === 'ellipse')!;
    c.selectedId.set(ellipse.id);
    c.deleteSelected();
    c.goTo(1);
    c.setNotes('Second save notes');
    const again = await pptxToDeck(await (await c.content()).arrayBuffer(), { id: 'x', name: 'x.pptx' }, parse);
    expect(again.deck.slides[1].notes).toBe('Second save notes');
    expect(again.deck.slides[0].elements.find(e => e.kind === 'shape' && e.shapeType === 'ellipse')).toBeUndefined();
    const typed = again.deck.slides[0].elements.filter(e => e.kind === 'shape' && e.text?.paragraphs[0]?.runs[0]?.text === 'Typed');
    expect(typed).toHaveLength(1);
    expect(again.deck.slides[0].elements).toHaveLength(back.deck.slides[0].elements.length - 1);
  });

  it('asks to save on Ctrl+S only while editable, and keeps shortcuts out of text being typed', async () => {
    const save = vi.fn();
    const el = await open(await samplePptx(), false);
    fixture.componentInstance.saveRequested.subscribe(save);
    key(el, 's', { ctrlKey: true });
    expect(save).toHaveBeenCalledTimes(1);
    const c = fixture.componentInstance;
    c.insertTextBox();
    const id = c.selectedId()!;
    c.onRequestEdit(id);
    fixture.detectChanges();
    const editable = el.querySelector<HTMLElement>('[contenteditable="true"]')!;
    expect(editable).not.toBeNull();
    editable.dispatchEvent(new KeyboardEvent('keydown', { key: 'Delete', bubbles: true }));
    expect(c.slide().elements.find(e => e.id === id)).toBeDefined();

    const viewer = await open(await samplePptx());
    const saveView = vi.fn();
    fixture.componentInstance.saveRequested.subscribe(saveView);
    key(viewer, 's', { ctrlKey: true });
    expect(saveView).not.toHaveBeenCalled();
  });

  describe('comments', () => {
    beforeEach(() => TestBed.inject(CommentsPanelState).open.set(null));
    const reread = async () =>
      pptxToDeck(new Uint8Array(await (await fixture.componentInstance.content()).arrayBuffer()), { id: 'x', name: 'x.pptx' }, parse);

    it('comments on the selected shape: a pin at its corner, the shape outlined, and the thread saved into the file', async () => {
      const el = await open(await samplePptx(), false);
      const c = fixture.componentInstance;
      const title = c.slide().elements[0];
      c.onSelect(title.id);
      c.startComment();
      fixture.detectChanges();
      expect(el.querySelector('.thread--draft')).not.toBeNull();
      expect(el.querySelector(`.slides__stage [data-element-id="${title.id}"]`)?.classList).toContain('slide__element--commented');

      c.comments.create({ text: 'Shorter title?', mentions: [] });
      fixture.detectChanges();
      expect(c.dirty()).toBe(true);
      const pins = el.querySelectorAll('.slides__stage .slide__pin');
      expect(pins).toHaveLength(1);
      expect(pins[0].getAttribute('aria-label')).toBe('Comment by Pat Lee');
      expect(el.querySelector('.slides__count')?.textContent?.trim()).toBe('1');
      expect(el.querySelector('.slides__thumb-comments')?.textContent?.trim()).toBe('1');

      const blob = await c.content();
      c.markSaved(blob);
      expect(c.dirty()).toBe(false);
      const back = await pptxToDeck(new Uint8Array(await blob.arrayBuffer()), { id: 'x', name: 'x.pptx' }, parse);
      expect(back.comments.threads).toEqual([
        expect.objectContaining({
          anchor: {
            slideId: back.deck.slides[0].id,
            x: title.left + title.width,
            y: title.top,
            elementId: back.deck.slides[0].elements[0].id,
          },
          comments: [expect.objectContaining({ authorId: 'user:u-1', authorName: 'Pat Lee', text: 'Shorter title?' })],
        }),
      ]);
      // A reply after the save is a change of its own.
      c.comments.reply({ threadId: c.comments.threads()[0].id, draft: { text: 'Yes', mentions: [] } });
      expect(c.dirty()).toBe(true);
    });

    it("a shape thread's pin moves with its shape, and undo moves it back", async () => {
      const el = await open(await samplePptx(), false);
      const c = fixture.componentInstance;
      const title = c.slide().elements[0];
      c.onSelect(title.id);
      c.startComment();
      c.comments.create({ text: 'Follow me', mentions: [] });
      fixture.detectChanges();
      const at = () => c.comments.threads()[0].anchor;
      expect(at()).toEqual(expect.objectContaining({ x: title.left + title.width, y: title.top }));
      c.onGestureStart();
      c.onGeometryChange({
        id: title.id,
        geometry: { left: title.left + 50, top: title.top + 20, width: title.width, height: title.height },
      });
      fixture.detectChanges();
      expect(at()).toEqual(expect.objectContaining({ x: title.left + title.width + 50, y: title.top + 20 }));
      c.undo();
      fixture.detectChanges();
      expect(at()).toEqual(expect.objectContaining({ x: title.left + title.width, y: title.top }));
      void el;
    });

    it('comments on the slide when nothing is selected, and shows each slide its own threads', async () => {
      const el = await open(await samplePptx(), false);
      const c = fixture.componentInstance;
      c.startComment();
      c.comments.create({ text: 'Whole slide note', mentions: [] });
      fixture.detectChanges();
      expect(c.comments.threads()[0].anchor).toEqual({ slideId: c.slide().id, x: 12, y: 24 });
      expect(c.comments.threads()[0].quote).toBe('Slide 1');
      expect(el.querySelectorAll('app-comments-panel .thread')).toHaveLength(1);
      c.goTo(1);
      fixture.detectChanges();
      expect(el.querySelectorAll('app-comments-panel .thread')).toHaveLength(0);
      expect(el.querySelectorAll('.slide__pin')).toHaveLength(0);
      expect((await reread()).comments.threads.map(t => t.comments[0].text)).toEqual(['Whole slide note']);
    });

    it("right-click: a new comment where the slide was clicked, and reply to or delete a pin's thread", async () => {
      const el = await open(await samplePptx(), false);
      const c = fixture.componentInstance;
      const slideEl = el.querySelector('.slides__stage .slide') as HTMLElement;
      const rect = slideEl.getBoundingClientRect();
      slideEl.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: rect.left, clientY: rect.top }));
      fixture.detectChanges();
      const items = () => [...el.querySelectorAll('.slides__context [role="menuitem"]')].map(b => b.textContent?.trim());
      expect(items()).toEqual(['New comment']);
      (el.querySelector('.slides__context [role="menuitem"]') as HTMLButtonElement).click();
      c.comments.create({ text: 'Top-left note', mentions: [] });
      fixture.detectChanges();
      expect(c.comments.threads()[0].anchor).toEqual({ slideId: c.slide().id, x: 0, y: 0 });

      const pin = el.querySelector('.slides__stage .slide__pin') as HTMLElement;
      pin.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
      fixture.detectChanges();
      expect(items()).toEqual(['Reply to comment', 'Delete comment']);
      (el.querySelectorAll('.slides__context [role="menuitem"]')[0] as HTMLButtonElement).click();
      fixture.detectChanges();
      expect(el.querySelector('app-comments-panel textarea[aria-label="Reply"]')).not.toBeNull();

      pin.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }));
      fixture.detectChanges();
      (el.querySelectorAll('.slides__context [role="menuitem"]')[1] as HTMLButtonElement).click();
      expect(c.comments.threads()).toEqual([]);
    });

    it('while viewing, with a saver: a comment on the slide is saved as it is made', async () => {
      const saveComments = vi.fn().mockResolvedValue('saved');
      testSaver = { canComment: signal(true), saveComments, showCurrentContent: vi.fn() };
      const el = await open(await samplePptx());
      const c = fixture.componentInstance;
      expect(c.commentable()).toBe(true);
      const button = [...el.querySelectorAll<HTMLButtonElement>('.slides__status button')].find(b => b.textContent?.trim() === 'Comment')!;
      button.click();
      c.comments.create({ text: 'Seen in the viewer', mentions: [] });
      await vi.waitFor(() => expect(saveComments).toHaveBeenCalledTimes(1));
      expect((await reread()).comments.threads.map(t => t.comments[0].text)).toEqual(['Seen in the viewer']);
    });

    it('read-only: pins show the threads, and choosing one opens it in the panel', async () => {
      const editor = await open(await samplePptx(), false);
      fixture.componentInstance.startComment();
      fixture.componentInstance.comments.create({ text: 'Look here', mentions: [] });
      const blob = await fixture.componentInstance.content();
      void editor;
      TestBed.inject(CommentsPanelState).open.set(false);

      const el = await open(blob);
      const c = fixture.componentInstance;
      expect(el.querySelector('app-comments-panel')).toBeNull();
      (el.querySelector('.slide__pin') as HTMLButtonElement).click();
      fixture.detectChanges();
      expect(c.comments.activeId()).toBe(c.comments.threads()[0].id);
      expect(el.querySelector('app-comments-panel .thread--active')?.textContent).toContain('Look here');
      expect(el.querySelector('app-comments-panel .thread__reply')).toBeNull();
    });
  });

  it('reports bytes it cannot read', async () => {
    const el = await open(new Blob(['nope']));
    expect(fixture.componentInstance.state()).toBe('error');
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('presentation could not be opened');
  });
});
