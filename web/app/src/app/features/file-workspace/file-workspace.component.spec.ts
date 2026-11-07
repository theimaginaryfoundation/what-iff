import { Component, input, provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { NEVER, of, Subject, throwError } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { FileWorkspaceComponent } from './file-workspace.component';
import { VIEWER_LOADER } from './viewers/registry';

@Component({ standalone: true, template: `<p class="fake-viewer">{{ file().name }}:{{ blob().size }}</p>` })
class FakeViewerComponent {
  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();
}

function file(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u-1', name: 'notes.txt', file_type: 'text/plain', created_at: '2026-05-01T00:00:00Z', ...overrides };
}

const textBlob = (text: string) => new Blob([text], { type: 'text/plain' });

describe('FileWorkspaceComponent', () => {
  let fixture: ComponentFixture<FileWorkspaceComponent>;
  let download: ReturnType<typeof vi.fn>;
  let alert: ReturnType<typeof vi.fn>;
  let loadViewer: ReturnType<typeof vi.fn<(kind: string) => Promise<unknown> | null>>;

  beforeEach(async () => {
    download = vi.fn().mockReturnValue(of(textBlob('hello')));
    alert = vi.fn().mockResolvedValue(undefined);
    loadViewer = vi.fn((kind: string) => (kind === 'none' ? null : Promise.resolve(FakeViewerComponent)));
    await TestBed.configureTestingModule({
      imports: [FileWorkspaceComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: FileAttachmentService, useValue: { downloadFileAttachment: download } },
        { provide: ConfirmationService, useValue: { alert } },
        { provide: VIEWER_LOADER, useValue: (kind: string) => loadViewer(kind) },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(FileWorkspaceComponent);
  });

  afterEach(() => vi.restoreAllMocks());

  async function show(value: FileAttachment): Promise<HTMLElement> {
    fixture.componentRef.setInput('file', value);
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  /** Names the workspace saved files as, by watching the temporary download link it clicks. */
  function watchSaves(): string[] {
    const saved: string[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(this.download);
    });
    return saved;
  }

  const button = (el: HTMLElement, label: string) =>
    [...el.querySelectorAll('button')].find(b => b.textContent?.trim() === label) as HTMLButtonElement | undefined;

  it('fetches the bytes once and hands them with the file to the viewer for its kind', async () => {
    const el = await show(file());
    expect(download).toHaveBeenCalledWith('f-1');
    expect(loadViewer).toHaveBeenCalledWith('text');
    expect(el.querySelector('.fake-viewer')?.textContent).toBe('notes.txt:5');
  });

  it('offers a download, without fetching, for a file it cannot show', async () => {
    const saved = watchSaves();
    download.mockReturnValue(of(new Blob(['PK'])));
    const el = await show(file({ name: 'bundle.zip', file_type: 'application/zip' }));
    expect(download).not.toHaveBeenCalled();
    expect(el.textContent).toContain('There is no preview for this kind of file.');
    button(el, 'Download')!.click();
    expect(download).toHaveBeenCalledWith('f-1');
    expect(saved).toEqual(['bundle.zip']);
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
    slow.next(textBlob('first!'));
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    expect(el.querySelector('.fake-viewer')?.textContent).toBe('notes.txt:6');
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
    expect(el.querySelector('.file-workspace__meta')?.textContent).toContain('Document of Aster');
  });

  it('shows the host-specific controls it is asked for', async () => {
    download.mockReturnValue(NEVER);
    fixture.componentRef.setInput('backLabel', 'Gallery');
    fixture.componentRef.setInput('showNavigation', true);
    fixture.componentRef.setInput('showMove', true);
    let el = await show(file());
    expect(el.querySelector('.file-workspace__back')?.textContent).toContain('Gallery');
    expect(el.querySelector('button[aria-label="Close file"]')).toBeNull();
    expect(button(el, 'Prev')).toBeDefined();
    expect(button(el, 'Move to…')).toBeDefined();

    fixture.componentRef.setInput('backLabel', null);
    fixture.componentRef.setInput('showNavigation', false);
    fixture.componentRef.setInput('showMove', false);
    fixture.componentRef.setInput('expandable', true);
    fixture.componentRef.setInput('showGalleryLink', true);
    el = await show(file({ chat_id: 'c-1' }));
    expect(el.querySelector('.file-workspace__back')).toBeNull();
    expect(el.querySelector('button[aria-label="Close file"]')).not.toBeNull();
    expect(el.querySelector('button[aria-label="Expand file"]')).not.toBeNull();
    expect(button(el, 'Prev')).toBeUndefined();
    expect(el.querySelector('a.file-workspace__link')?.textContent).toContain('Open in gallery');
  });
});
