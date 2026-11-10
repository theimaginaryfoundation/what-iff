import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { PdfViewerComponent } from './pdf-viewer.component';
import { PDFJS_LOADER, type PdfJs } from './pdfjs-loader';

const file: FileAttachment = { id: 'f-1', user_id: 'u-1', name: 'report.pdf', file_type: 'application/pdf', created_at: '2026-05-01T00:00:00Z' };

interface Fake {
  pdfjs: PdfJs;
  rendered: number[];
  destroyed: number;
}

function fakePdfJs(pages = 3): Fake {
  const fake: Fake = { rendered: [], destroyed: 0, pdfjs: null as unknown as PdfJs };
  const page = (n: number) => ({
    getViewport: ({ scale }: { scale: number }) => ({ width: 600 * scale, height: 800 * scale, scale }),
    render: () => {
      fake.rendered.push(n);
      return { promise: Promise.resolve() };
    },
    streamTextContent: () => ({}),
  });
  fake.pdfjs = {
    getDocument: () => ({
      promise: Promise.resolve({ numPages: pages, getPage: (n: number) => Promise.resolve(page(n)) }),
      destroy: () => {
        fake.destroyed += 1;
        return Promise.resolve();
      },
    }),
    setLayerDimensions: () => undefined,
    TextLayer: class {
      render() {
        return Promise.resolve();
      }
    },
  } as unknown as PdfJs;
  return fake;
}

describe('PdfViewerComponent', () => {
  let fixture: ComponentFixture<PdfViewerComponent>;
  let fake: Fake;

  beforeEach(async () => {
    // jsdom has no IntersectionObserver; this one reports every page as visible at once.
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        constructor(private readonly cb: (entries: { isIntersecting: boolean; target: Element }[]) => void) {}
        observe(el: Element) {
          this.cb([{ isIntersecting: true, target: el }]);
        }
        disconnect() {}
      },
    );
    fake = fakePdfJs();
    await TestBed.configureTestingModule({
      imports: [PdfViewerComponent],
      providers: [provideZonelessChangeDetection(), { provide: PDFJS_LOADER, useValue: () => Promise.resolve(fake.pdfjs) }],
    }).compileComponents();
    fixture = TestBed.createComponent(PdfViewerComponent);
    fixture.componentRef.setInput('file', file);
    fixture.componentRef.setInput('blob', new Blob(['%PDF-1.7'], { type: 'application/pdf' }));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).toBe('ready'));
    fixture.detectChanges();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('lays out a shell per page, renders the visible ones, and reports the count', async () => {
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelectorAll('.pdf-viewer__page')).toHaveLength(3);
    expect(el.textContent).toContain('Page 1 of 3');
    await vi.waitFor(() => expect(fake.rendered).toEqual([1, 2, 3]));
    expect(el.querySelectorAll('.pdf-viewer__page canvas')).toHaveLength(3);
    expect(el.querySelectorAll('.pdf-viewer__page .textLayer')).toHaveLength(3);
  });

  it('re-lays out at a new zoom and clamps to the steps', () => {
    const el = fixture.nativeElement as HTMLElement;
    const before = (el.querySelector('.pdf-viewer__page') as HTMLElement).style.width;
    fixture.componentInstance.zoomIn();
    fixture.detectChanges();
    expect(fixture.componentInstance.zoom()).toBe(1.25);
    expect((el.querySelector('.pdf-viewer__page') as HTMLElement).style.width).not.toBe(before);
    for (let i = 0; i < 10; i++) fixture.componentInstance.zoomIn();
    expect(fixture.componentInstance.zoom()).toBe(3);
    fixture.componentInstance.fitWidth();
    expect(fixture.componentInstance.zoom()).toBe(1);
  });

  it('closes the document when the bytes change or it is destroyed', async () => {
    fixture.componentRef.setInput('blob', new Blob(['%PDF-1.7 v2'], { type: 'application/pdf' }));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fake.destroyed).toBe(1));
    await vi.waitFor(() => expect(fixture.componentInstance.state()).toBe('ready'));
    fixture.destroy();
    expect(fake.destroyed).toBe(2);
  });
});
