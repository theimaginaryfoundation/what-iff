import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideRouter } from '@angular/router';
import { provideMarkdown } from 'ngx-markdown';
import { NEVER, of, Subject, throwError } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { FileViewerComponent } from './file-viewer.component';

function file(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return {
    id: 'f-1',
    user_id: 'u-1',
    name: 'notes.txt',
    file_type: 'text/plain',
    created_at: '2026-05-01T00:00:00Z',
    ...overrides,
  };
}

const textBlob = (text: string, type = 'text/plain') => new Blob([text], { type });

describe('FileViewerComponent', () => {
  let fixture: ComponentFixture<FileViewerComponent>;
  let download: ReturnType<typeof vi.fn>;
  let alert: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    download = vi.fn().mockReturnValue(of(textBlob('hello')));
    alert = vi.fn().mockResolvedValue(undefined);
    globalThis.URL.createObjectURL ??= () => '';
    globalThis.URL.revokeObjectURL ??= () => undefined;
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:viewer');
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);

    await TestBed.configureTestingModule({
      imports: [FileViewerComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        provideMarkdown(),
        { provide: FileAttachmentService, useValue: { downloadFileAttachment: download } },
        { provide: ConfirmationService, useValue: { alert } },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(FileViewerComponent);
  });

  afterEach(() => vi.restoreAllMocks());

  async function show(value: FileAttachment): Promise<HTMLElement> {
    fixture.componentRef.setInput('file', value);
    fixture.detectChanges();
    await fixture.whenStable();
    // The text is decoded after an await on the blob; give it a turn, then render.
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  /** Names the viewer saved files as, by watching the temporary download link it clicks. */
  function watchSaves(): string[] {
    const saved: string[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(this.download);
    });
    return saved;
  }

  const button = (el: HTMLElement, label: string) =>
    [...el.querySelectorAll('button')].find(b => b.textContent?.trim() === label) as HTMLButtonElement | undefined;

  it('shows a text file as text, never as markup', async () => {
    download.mockReturnValue(of(textBlob('<b>bold?</b>\nline two')));
    const el = await show(file({ name: 'page.html', file_type: 'text/html' }));

    expect(download).toHaveBeenCalledWith('f-1');
    const pre = el.querySelector('pre.file-viewer__text');
    expect(pre?.textContent).toBe('<b>bold?</b>\nline two');
    expect(el.querySelector('b')).toBeNull();
  });

  it('renders Markdown, with the raw text a click away', async () => {
    download.mockReturnValue(of(textBlob('# Title\n\nSome *text*.')));
    const el = await show(file({ name: 'notes.md', file_type: 'text/markdown' }));

    expect(el.querySelector('markdown')).not.toBeNull();
    expect(el.querySelector('pre.file-viewer__text')).toBeNull();

    button(el, 'Raw')!.click();
    fixture.detectChanges();
    expect(el.querySelector('markdown')).toBeNull();
    expect(el.querySelector('pre.file-viewer__text')?.textContent).toBe('# Title\n\nSome *text*.');
  });

  it('shows a CSV as a table with its header row', async () => {
    download.mockReturnValue(of(textBlob('name,score\nAda,"9,5"\n')));
    const el = await show(file({ name: 'scores.csv', file_type: 'text/csv' }));

    const headers = [...el.querySelectorAll('th')].map(th => th.textContent);
    const cells = [...el.querySelectorAll('td')].map(td => td.textContent);
    expect(headers).toEqual(['name', 'score']);
    expect(cells).toEqual(['Ada', '9,5']);
  });

  it('shows a PDF in the browser viewer from a PDF-typed object URL', async () => {
    download.mockReturnValue(of(new Blob(['%PDF-1.7'], { type: 'application/octet-stream' })));
    const el = await show(file({ name: 'report.pdf', file_type: 'application/pdf' }));

    if (!fixture.componentInstance.pdfInline) {
      expect(button(el, 'Download')).toBeDefined();
      return;
    }
    const blob = vi.mocked(URL.createObjectURL).mock.lastCall?.[0] as Blob;
    expect(blob.type).toBe('application/pdf');
    expect(el.querySelector('iframe.file-viewer__frame')).not.toBeNull();
  });

  it('offers a download, without fetching, for a file it cannot show', async () => {
    const saved = watchSaves();
    download.mockReturnValue(of(new Blob(['PK'])));
    const el = await show(file({ name: 'letter.docx', file_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' }));

    expect(download).not.toHaveBeenCalled();
    expect(el.textContent).toContain('There is no preview for this kind of file.');

    button(el, 'Download')!.click();
    expect(download).toHaveBeenCalledWith('f-1');
    expect(saved).toEqual(['letter.docx']);
  });

  it('downloads the bytes it already has instead of fetching them again', async () => {
    const saved = watchSaves();
    const el = await show(file());
    download.mockClear();

    (el.querySelector('button[aria-label="Download file"]') as HTMLButtonElement).click();

    expect(download).not.toHaveBeenCalled();
    expect(saved).toEqual(['notes.txt']);
  });

  it('ignores a second download click while one is in flight', async () => {
    const pending = new Subject<Blob>();
    const saved = watchSaves();
    await show(file({ name: 'bundle.zip', file_type: 'application/zip' }));
    download.mockReturnValue(pending);

    fixture.componentInstance.download();
    fixture.componentInstance.download();
    expect(download).toHaveBeenCalledTimes(1);

    pending.next(new Blob(['PK']));
    expect(saved).toEqual(['bundle.zip']);
    expect(fixture.componentInstance.downloading()).toBe(false);
  });

  it('says so when a "text" file is really binary', async () => {
    download.mockReturnValue(of(new Blob([new Uint8Array([0x00, 0x01, 0x02, 0x00])])));
    const el = await show(file({ name: 'data.json', file_type: 'application/json' }));
    expect(el.textContent).toContain('does not look like text');
    expect(el.querySelector('pre.file-viewer__text')).toBeNull();
  });

  it('reports a failed load and still offers the download', async () => {
    download.mockReturnValue(throwError(() => new Error('boom')));
    const el = await show(file());
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('Could not load this file.');
    expect(button(el, 'Try downloading it')).toBeDefined();
  });

  it('does not refetch when only the metadata changes (a move or rename)', async () => {
    await show(file());
    download.mockClear();
    await show(file({ folder: 'docs', name: 'renamed.txt' }));
    expect(download).not.toHaveBeenCalled();
  });

  it('drops bytes for a file that is no longer shown', async () => {
    const slow = new Subject<Blob>();
    download.mockReturnValueOnce(slow).mockReturnValueOnce(of(textBlob('second')));
    fixture.componentRef.setInput('file', file({ id: 'first' }));
    fixture.detectChanges();
    const el = await show(file({ id: 'second' }));

    slow.next(textBlob('first'));
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    expect(el.querySelector('pre.file-viewer__text')?.textContent).toBe('second');
  });

  it('renames keeping the extension, and emits nothing for an unchanged name', async () => {
    const renamed = vi.fn();
    fixture.componentInstance.rename.subscribe(renamed);
    await show(file({ name: 'notes.txt' }));

    fixture.componentInstance.startRename();
    expect(fixture.componentInstance.renameDraft()).toBe('notes');
    fixture.componentInstance.renameDraft.set('journal');
    fixture.componentInstance.submitRename();
    expect(renamed).toHaveBeenCalledWith({ id: 'f-1', name: 'journal.txt' });

    renamed.mockClear();
    fixture.componentInstance.startRename();
    fixture.componentInstance.submitRename();
    expect(renamed).not.toHaveBeenCalled();
  });

  it('names a personality document as such', async () => {
    download.mockReturnValue(NEVER);
    fixture.componentRef.setInput('personalityName', 'Aster');
    const el = await show(file({ personality_id: 'p-1', source: 'imported' }));
    expect(el.querySelector('.file-viewer__meta')?.textContent).toContain('Document of Aster');
  });
});
