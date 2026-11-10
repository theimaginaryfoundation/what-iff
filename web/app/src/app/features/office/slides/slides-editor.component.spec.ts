import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import PptxGenJS from 'pptxgenjs';
import { parse } from 'pptxtojson';

import { FileAttachment } from '../../../core/models/file-attachment.model';
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

describe('SlidesEditorComponent', () => {
  let fixture: ComponentFixture<SlidesEditorComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [SlidesEditorComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: PPTX_PARSER, useValue: () => Promise.resolve(parse) },
        { provide: PPTX_WRITER, useValue: async () => new PptxGenJS() },
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
    const typed = again.deck.slides[0].elements.filter(
      e => e.kind === 'shape' && e.text?.paragraphs[0]?.runs[0]?.text === 'Typed',
    );
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

  it('reports bytes it cannot read', async () => {
    const el = await open(new Blob(['nope']));
    expect(fixture.componentInstance.state()).toBe('error');
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('presentation could not be opened');
  });
});
