import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';

import { CommentPeopleService } from '../office/comments/comment-people.service';
import { fakeTextComments } from './text-comments/fake-text-comments.spec-helper';
import { TextCommentsService } from './text-comments/text-comments.service';
import { of } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { FileWorkspacePanelComponent } from './file-workspace-panel.component';
import { FileWorkspaceService, LARGE_VIEWPORT_QUERY, MEDIUM_VIEWPORT_QUERY } from './file-workspace.service';

const row: FileAttachment = { id: 'f-1', user_id: 'u-1', name: 'notes.txt', file_type: 'text/plain', created_at: '2026-05-01T00:00:00Z' };

describe('FileWorkspacePanelComponent', () => {
  let fixture: ComponentFixture<FileWorkspacePanelComponent>;
  let service: FileWorkspaceService;
  let viewport: 'large' | 'medium' | 'phone';
  let navigate: ReturnType<typeof vi.fn>;
  let rename: ReturnType<typeof vi.fn>;
  let remove: ReturnType<typeof vi.fn>;
  let confirm: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    localStorage.clear();
    viewport = 'large';
    vi.spyOn(window, 'matchMedia').mockImplementation(
      query =>
        ({
          matches: (query === LARGE_VIEWPORT_QUERY && viewport === 'large') || (query === MEDIUM_VIEWPORT_QUERY && viewport !== 'phone'),
        }) as MediaQueryList,
    );
    rename = vi.fn().mockReturnValue(of({ ...row, name: 'renamed.txt' }));
    remove = vi.fn().mockReturnValue(of({}));
    confirm = vi.fn().mockResolvedValue(true);
    await TestBed.configureTestingModule({
      imports: [FileWorkspacePanelComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: ImageGalleryService, useValue: { getFileInfo: () => of(row), renameImage: rename } },
        {
          provide: FileAttachmentService,
          useValue: {
            downloadFileAttachment: () => of(new Blob(['x'])),
            downloadFileContent: () => of({ blob: new Blob(['x']), version: null }),
            deleteFileAttachment: remove,
          },
        },
        { provide: ConfirmationService, useValue: { confirm, alert: vi.fn() } },
        { provide: CommentPeopleService, useValue: { me: signal({ id: 'user:u-1', name: 'Pat Lee' }) } },
        { provide: TextCommentsService, useValue: fakeTextComments() },
      ],
    }).compileComponents();
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true) as never;
    service = TestBed.inject(FileWorkspaceService);
    fixture = TestBed.createComponent(FileWorkspacePanelComponent);
  });

  afterEach(() => vi.restoreAllMocks());

  it('docks beside the thread on a large screen, with a resizer, and expands into an overlay', async () => {
    service.show('chat', 'f-1', { chatId: 'c-1' });
    fixture.detectChanges();
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('.workspace-panel--docked')).not.toBeNull();
    expect(el.querySelector('[role="separator"]')).not.toBeNull();
    expect(el.querySelector('ui-overlay [role="dialog"]')).toBeNull();

    (el.querySelector('button[aria-label="Expand file"]') as HTMLButtonElement).click();
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(el.querySelector('.workspace-panel--docked')).toBeNull();
    });
    expect(el.querySelector('ui-overlay [role="dialog"]')).not.toBeNull();
    expect(el.querySelector('.ui-overlay--inset')).not.toBeNull();
  });

  it('fills the screen on a tablet and a phone', async () => {
    viewport = 'medium';
    window.dispatchEvent(new Event('resize'));
    service.show('chat', 'f-1', { chatId: 'c-1' });
    fixture.detectChanges();
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('ui-overlay [role="dialog"]')).not.toBeNull();
    expect(el.querySelector('.ui-overlay--inset')).toBeNull();
    expect(el.querySelector('button[aria-label="Expand file"]')).toBeNull();
  });

  it('closes through the URL, renames through the gallery, and deletes after confirming', async () => {
    service.show('chat', 'f-1', { chatId: 'c-1' });
    fixture.detectChanges();
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;

    (el.querySelector('button[aria-label="Close file"]') as HTMLButtonElement).click();
    // Closing checks for unsaved edits first, so the navigation follows a tick later.
    await vi.waitFor(() => expect(navigate).toHaveBeenCalledWith([], { queryParams: { file: null }, queryParamsHandling: 'merge' }));

    fixture.componentInstance.rename({ id: 'f-1', name: 'renamed.txt' });
    expect(rename).toHaveBeenCalledWith('f-1', 'renamed.txt');
    expect(service.file()?.name).toBe('renamed.txt');

    await fixture.componentInstance.remove(row);
    expect(confirm).toHaveBeenCalled();
    expect(remove).toHaveBeenCalledWith('f-1');
    expect(navigate).toHaveBeenCalledTimes(2);
  });

  it('resizes with the keyboard within the bounds', async () => {
    service.show('chat', 'f-1', { chatId: 'c-1' });
    fixture.detectChanges();
    await fixture.whenStable();
    const start = service.dockedWidth();
    fixture.componentInstance.onResizerKey(new KeyboardEvent('keydown', { key: 'ArrowLeft' }));
    expect(service.dockedWidth()).toBe(Math.min(fixture.componentInstance.maxWidth(), start + 40));
  });
});
