import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { PDFJS_LOADER, type PdfJs } from '../viewers/pdfjs-loader';
import { PDFJS_VIEWER_LOADER, PdfEditorComponent } from './pdf-editor.component';

const file: FileAttachment = { id: 'f-1', user_id: 'u', name: 'report.pdf', file_type: 'application/pdf', created_at: '2026-11-01T00:00:00Z' };

function fakes() {
  const state = {
    storage: { hash: '', onSetModified: null as null | (() => void), onResetModified: null as null | (() => void), resets: 0 },
    saved: 0,
    destroyed: 0,
    modes: [] as number[],
    params: [] as { type: number; value: string }[],
    scale: '',
    setDocuments: [] as unknown[],
  };
  const doc = {
    numPages: 2,
    annotationStorage: {
      get serializable() {
        return { hash: state.storage.hash };
      },
      set onSetModified(fn: (() => void) | null) {
        state.storage.onSetModified = fn;
      },
      set onResetModified(fn: (() => void) | null) {
        state.storage.onResetModified = fn;
      },
      resetModified: () => {
        state.storage.resets += 1;
        state.storage.onResetModified?.();
      },
    },
    saveDocument: () => {
      state.saved += 1;
      return Promise.resolve(new Uint8Array([37, 80, 68, 70]));
    },
  };
  const pdfjs = {
    getDocument: () => ({
      promise: Promise.resolve(doc),
      destroy: () => {
        state.destroyed += 1;
        return Promise.resolve();
      },
    }),
    AnnotationEditorType: { NONE: 0, FREETEXT: 3, HIGHLIGHT: 9, INK: 15 },
    AnnotationEditorParamsType: { FREETEXT_COLOR: 2, INK_COLOR: 11, HIGHLIGHT_COLOR: 32 },
  } as unknown as PdfJs;
  const listeners: Record<string, (() => void)[]> = {};
  class EventBus {
    on(name: string, fn: () => void) {
      (listeners[name] ??= []).push(fn);
    }
    dispatch(name: string, payload: { type: number; value: string }) {
      if (name === 'switchannotationeditorparams') state.params.push({ type: payload.type, value: payload.value });
    }
  }
  class SimpleLinkService {
    setViewer() {}
    setDocument() {}
  }
  class PDFViewer {
    constructor(readonly options: { annotationEditorMode: number }) {}
    set annotationEditorMode(value: { mode: number }) {
      state.modes.push(value.mode);
    }
    set currentScaleValue(value: string) {
      state.scale = value;
    }
    setDocument(d: unknown) {
      state.setDocuments.push(d);
      if (d) listeners['pagesinit']?.forEach(fn => fn());
    }
  }
  const viewerModule = { PDFViewer, EventBus, SimpleLinkService } as never;
  return { pdfjs, viewerModule, state };
}

describe('PdfEditorComponent', () => {
  let fixture: ComponentFixture<PdfEditorComponent>;
  let fake: ReturnType<typeof fakes>;

  beforeEach(async () => {
    fake = fakes();
    await TestBed.configureTestingModule({
      imports: [PdfEditorComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: PDFJS_LOADER, useValue: () => Promise.resolve(fake.pdfjs) },
        { provide: PDFJS_VIEWER_LOADER, useValue: () => Promise.resolve(fake.viewerModule) },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(PdfEditorComponent);
    fixture.componentRef.setInput('file', file);
    fixture.componentRef.setInput('blob', new Blob(['%PDF'], { type: 'application/pdf' }));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).toBe('ready'));
    fixture.detectChanges();
  });

  it('opens the document in the viewer at page width with editing off, and shows the page count', () => {
    expect(fake.state.setDocuments).toHaveLength(1);
    expect(fake.state.scale).toBe('page-width');
    expect(fake.state.modes).toEqual([0]);
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('2 pages');
    expect(fake.state.params.map(p => p.value)).toEqual(['#e02020', '#e02020', '#e02020']);
  });

  it('switches tools and colours the way the viewer toolbar does', () => {
    fixture.componentInstance.setTool('draw');
    fixture.componentInstance.setTool('highlight');
    fixture.componentInstance.setTool('text');
    expect(fake.state.modes).toEqual([0, 15, 9, 3]);
    fixture.componentInstance.setColor('#1f6feb');
    expect(fake.state.params.slice(-3)).toEqual([
      { type: 2, value: '#1f6feb' },
      { type: 11, value: '#1f6feb' },
      { type: 32, value: '#1f6feb' },
    ]);
  });

  it('follows the annotation storage for dirtiness, saves the document, and marks it clean', async () => {
    const dirty: boolean[] = [];
    fixture.componentInstance.dirtyChange.subscribe(d => dirty.push(d));
    fake.state.storage.onSetModified!();
    expect(fixture.componentInstance.dirty()).toBe(true);
    const blob = await fixture.componentInstance.content();
    expect(fake.state.saved).toBe(1);
    expect(blob.type).toBe('application/pdf');
    expect(await blob.text()).toBe('%PDF');
    fixture.componentInstance.markSaved();
    expect(fake.state.storage.resets).toBe(1);
    expect(fixture.componentInstance.dirty()).toBe(false);
    expect(dirty).toEqual([true, false]);
  });

  it('stays dirty for an annotation made while a save was running', async () => {
    fake.state.storage.onSetModified!();
    fake.state.storage.hash = 'a';
    const saved = await fixture.componentInstance.content();
    fake.state.storage.hash = 'b';
    fixture.componentInstance.markSaved(saved);
    expect(fixture.componentInstance.dirty()).toBe(true);
    expect(fake.state.storage.resets).toBe(0);
  });

  it('closes the document on destroy', () => {
    fixture.destroy();
    expect(fake.state.destroyed).toBe(1);
    expect(fake.state.setDocuments.at(-1)).toBeNull();
  });
});
