import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MARKED_EXTENSIONS, provideMarkdown } from 'ngx-markdown';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { latexMathExtension } from '../../../core/utils/latex-math.helpers';
import { fakeMonaco } from '../editors/fake-monaco.spec-helper';
import { MONACO_LOADER } from '../editors/monaco/monaco-loader';
import { TextViewerComponent } from './text-viewer.component';

function file(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u-1', name: 'notes.txt', file_type: 'text/plain', created_at: '2026-05-01T00:00:00Z', ...overrides };
}

describe('TextViewerComponent', () => {
  let fixture: ComponentFixture<TextViewerComponent>;
  let fake: ReturnType<typeof fakeMonaco>;

  beforeEach(async () => {
    fake = fakeMonaco();
    await TestBed.configureTestingModule({
      imports: [TextViewerComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideMarkdown({ markedExtensions: [{ provide: MARKED_EXTENSIONS, useFactory: latexMathExtension, multi: true }] }),
        { provide: MONACO_LOADER, useValue: () => Promise.resolve(fake.monaco) },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(TextViewerComponent);
  });

  async function show(value: FileAttachment, text: string | Uint8Array): Promise<HTMLElement> {
    fixture.componentRef.setInput('file', value);
    fixture.componentRef.setInput('blob', new Blob([text as BlobPart]));
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  const button = (el: HTMLElement, label: string) =>
    [...el.querySelectorAll('button')].find(b => b.textContent?.trim() === label) as HTMLButtonElement | undefined;

  it('shows a text file as text, never as markup', async () => {
    const el = await show(file({ name: 'page.html', file_type: 'text/html' }), '<b>bold?</b>\nline two');
    expect(el.querySelector('pre.text-viewer__text')?.textContent).toBe('<b>bold?</b>\nline two');
    expect(el.querySelector('b')).toBeNull();
    expect(fake.state.colorized).toEqual([]);
  });

  it('highlights code with a known language into static spans', async () => {
    const el = await show(file({ name: 'main.go', file_type: 'text/x-golang' }), 'package main');
    await vi.waitFor(() => expect(el.querySelector('pre.text-viewer__text--highlighted')).not.toBeNull());
    expect(fake.state.colorized).toEqual(['go']);
    expect(el.querySelector('pre.text-viewer__text span.mtk1')?.textContent).toBe('package main');
  });

  it('renders Markdown, with the raw text a click away', async () => {
    const el = await show(file({ name: 'notes.md', file_type: 'text/markdown' }), '# Title\n\nSome *text*.');
    expect(el.querySelector('markdown')).not.toBeNull();
    expect(el.querySelector('pre.text-viewer__text')).toBeNull();

    button(el, 'Raw')!.click();
    fixture.detectChanges();
    expect(el.querySelector('markdown')).toBeNull();
    expect(el.querySelector('pre.text-viewer__text')?.textContent).toBe('# Title\n\nSome *text*.');
  });

  it('renders LaTeX math in Markdown', async () => {
    const el = await show(file({ name: 'notes.md', file_type: 'text/markdown' }), 'Norm: $\\lVert x \\rVert$\n\n$$\nx_i^2\n$$');
    await vi.waitFor(() => expect(el.querySelectorAll('.katex')).toHaveLength(2));
    expect(el.querySelectorAll('.katex-display')).toHaveLength(1);
  });

  it('shows a CSV as a table with its header row', async () => {
    const el = await show(file({ name: 'scores.csv', file_type: 'text/csv' }), 'name,score\nAda,"9,5"\n');
    expect([...el.querySelectorAll('th')].map(th => th.textContent)).toEqual(['name', 'score']);
    expect([...el.querySelectorAll('td')].map(td => td.textContent)).toEqual(['Ada', '9,5']);
  });

  it('says so when a "text" file is really binary', async () => {
    const el = await show(file({ name: 'data.json', file_type: 'application/json' }), new Uint8Array([0x00, 0x01, 0x02, 0x00]));
    expect(el.textContent).toContain('does not look like text');
    expect(el.querySelector('pre.text-viewer__text')).toBeNull();
  });

  it('shows the newest bytes when the blob changes', async () => {
    await show(file(), 'first');
    const el = await show(file(), 'second');
    expect(el.querySelector('pre.text-viewer__text')?.textContent).toBe('second');
  });
});
