import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { Subject, of, throwError } from 'rxjs';

import { ChatContextFilesResponse, FileAttachment } from '../../../../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../../../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../../../../core/services/image-gallery.service';
import { ContextPanelService } from '../../../services/context-panel.service';
import { ContextFilesTabComponent } from './context-files-tab.component';

function file(id: string, name: string, type = 'text/plain'): FileAttachment {
  return { id, user_id: 'u', name, file_type: type, created_at: '2026-11-01T00:00:00Z' };
}

describe('ContextFilesTabComponent', () => {
  let fixture: ComponentFixture<ContextFilesTabComponent>;
  let list: ReturnType<typeof vi.fn>;
  let upload: ReturnType<typeof vi.fn>;
  let panel: ContextPanelService;

  const response: ChatContextFilesResponse = {
    files: [
      { file: file('f-new', 'report.csv', 'text/csv'), workspace_path: 'report.csv', reasons: ['attached_recent', 'attached_since_checkpoint'] },
      { file: file('f-agent', 'journal.md', 'text/markdown'), workspace_path: 'journal.md', reasons: ['agent_written'] },
      { file: file('f-old', 'Report.csv', 'text/csv'), workspace_path: 'Report.csv', reasons: ['recalled'], shadowed_by: 'f-new' },
    ],
    container: { state: 'running', files: { 'report.csv': 'current', 'journal.md': 'stale' } },
  };

  beforeEach(async () => {
    list = vi.fn().mockReturnValue(of(response));
    upload = vi.fn().mockReturnValue(of(file('f-up', 'new.txt')));
    await TestBed.configureTestingModule({
      imports: [ContextFilesTabComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: FileAttachmentService, useValue: { listChatContextFiles: list, uploadChatFileAttachment: upload } },
        { provide: ImageGalleryService, useValue: { getFileInfo: () => of(file('f-new', 'report.csv')) } },
      ],
    }).compileComponents();
    panel = TestBed.inject(ContextPanelService);
    fixture = TestBed.createComponent(ContextFilesTabComponent);
  });

  async function render(chatId: string | null = 'c-1'): Promise<HTMLElement> {
    fixture.componentRef.setInput('chatId', chatId);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('lists the mounted files with their badges and reasons, then the shadowed ones', async () => {
    const el = await render();
    expect(list).toHaveBeenCalledWith('c-1');
    const rows = [...el.querySelectorAll('.files-tab__row')];
    expect(rows.map(r => r.querySelector('.files-tab__name')?.textContent)).toEqual(['report.csv', 'journal.md', 'Report.csv']);
    expect(rows[0].querySelector('.files-tab__badge')?.textContent).toBe('CSV');
    expect([...rows[0].querySelectorAll('.files-tab__reason')].map(r => r.textContent)).toEqual(['Attached', 'current']);
    expect([...rows[1].querySelectorAll('.files-tab__reason')].map(r => r.textContent)).toEqual(['Agent file', 'stale']);
    expect(rows[2].classList.contains('files-tab__row--shadowed')).toBe(true);
    expect(el.textContent).toContain('Container: running');
  });

  it('opens a file in the workspace through the URL', async () => {
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    const el = await render();
    (el.querySelector('button[aria-label="Open journal.md"]') as HTMLButtonElement).click();
    expect(navigate).toHaveBeenCalledWith([], { queryParams: { file: 'f-agent' }, queryParamsHandling: 'merge', replaceUrl: false });
  });

  it('closes the panel when a file is opened, leaving the remembered choice alone', async () => {
    vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    const el = await render();
    panel.setDesktopVisible(true);
    expect(panel.visible()).toBe(true);
    (el.querySelector('button[aria-label="Open journal.md"]') as HTMLButtonElement).click();
    expect(panel.visible()).toBe(false);
    panel.restoreDesktopVisible();
    expect(panel.visible()).toBe(true);
  });

  it('reloads when the panel asks, and shows errors with a retry', async () => {
    await render();
    panel.refreshFiles();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(list).toHaveBeenCalledTimes(2);

    list.mockReturnValue(throwError(() => new Error('down')));
    panel.refreshFiles();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    expect((fixture.nativeElement as HTMLElement).querySelector('[role="alert"]')).not.toBeNull();
  });

  it('uploads a picked file and hands it to the composer; refuses unsupported types', async () => {
    const el = await render();
    const input = el.querySelector('input[type="file"]') as HTMLInputElement;
    const pick = (f: File) => {
      Object.defineProperty(input, 'files', { value: [f], configurable: true });
      input.dispatchEvent(new Event('change'));
    };
    pick(new File(['x'], 'notes.txt', { type: 'text/plain' }));
    expect(upload).toHaveBeenCalledWith('c-1', expect.any(File));
    expect(panel.composerAttachments().map(f => f.id)).toEqual(['f-up']);

    pick(new File(['x'], 'malware.exe', { type: 'application/octet-stream' }));
    fixture.detectChanges();
    expect(upload).toHaveBeenCalledTimes(1);
    expect(el.querySelector('.files-tab__error')?.textContent).toContain('not a supported file type');

    const buttons = [...el.querySelectorAll('button.files-tab__button')] as HTMLButtonElement[];
    buttons.find(b => b.textContent?.trim() === 'From gallery')!.click();
    expect(panel.galleryPickerRequests()).toBe(1);
  });

  it('lists files attached to the next message ahead of the thread\'s, without repeating listed ones', async () => {
    panel.setComposerPending([file('f-pending', 'draft.txt'), file('f-new', 'report.csv', 'text/csv')]);
    const el = await render();
    const pending = [...el.querySelectorAll('[data-testid="context-files-pending"] .files-tab__name')].map(n => n.textContent);
    expect(pending).toEqual(['draft.txt']);
    expect(el.textContent).toContain('Not sent yet');
  });

  it('drops a load in flight when the thread goes away, and is not left loading', async () => {
    const inFlight = new Subject<ChatContextFilesResponse>();
    list.mockReturnValue(inFlight);
    await render();
    expect(fixture.componentInstance.loading()).toBe(true);
    expect(inFlight.observed).toBe(true);

    fixture.componentRef.setInput('chatId', null);
    fixture.detectChanges();
    await fixture.whenStable();
    expect(fixture.componentInstance.loading()).toBe(false);
    expect(inFlight.observed).toBe(false);
  });

  it('says so without a thread, and when the thread has no files', async () => {
    let el = await render(null);
    expect(el.textContent).toContain('Open a thread');
    expect(list).not.toHaveBeenCalled();
    list.mockReturnValue(of({ files: [] }));
    el = await render('c-2');
    expect(el.textContent).toContain('No files yet');
  });
});
