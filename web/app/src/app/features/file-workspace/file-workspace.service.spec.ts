import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { FileWorkspaceService, LARGE_VIEWPORT_QUERY, MEDIUM_VIEWPORT_QUERY } from './file-workspace.service';

function file(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u-1', name: 'notes.md', file_type: 'text/markdown', created_at: '2026-11-01T00:00:00Z', ...overrides };
}

describe('FileWorkspaceService', () => {
  let getFileInfo: ReturnType<typeof vi.fn>;
  let navigate: ReturnType<typeof vi.fn>;
  let viewport: 'large' | 'medium' | 'phone';

  function setup(): FileWorkspaceService {
    getFileInfo = vi.fn().mockReturnValue(of(file({ chat_id: 'c-9' })));
    navigate = vi.fn().mockResolvedValue(true);
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
      matches: (query === LARGE_VIEWPORT_QUERY && viewport === 'large') || (query === MEDIUM_VIEWPORT_QUERY && viewport !== 'phone'),
    }) as MediaQueryList);
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        { provide: ImageGalleryService, useValue: { getFileInfo } },
        { provide: Router, useValue: { navigate } },
      ],
    });
    return TestBed.inject(FileWorkspaceService);
  }

  beforeEach(() => {
    localStorage.clear();
    viewport = 'large';
  });
  afterEach(() => vi.restoreAllMocks());

  it('shows the known row at once and the server copy when it arrives', () => {
    const info = new Subject<FileAttachment>();
    const service = setup();
    getFileInfo.mockReturnValue(info);
    service.show('chat', 'f-1', { chatId: 'c-1', known: file({ name: 'draft.md' }) });

    expect(service.open()).toBe(true);
    expect(service.file()?.name).toBe('draft.md');
    info.next(file({ chat_id: 'c-1' }));
    expect(service.file()?.chat_id).toBe('c-1');
    expect(service.rememberedFileFor('c-1')).toBe('f-1');
  });

  it('drops the request for a file shown before when another one is shown', () => {
    const first = new Subject<FileAttachment>();
    const service = setup();
    getFileInfo.mockReturnValueOnce(first);
    service.show('gallery', 'f-1');
    expect(first.observed).toBe(true);
    service.show('gallery', 'f-2');
    expect(first.observed).toBe(false);
  });

  it('reports a file that cannot be found, unless it already has a row', () => {
    const service = setup();
    getFileInfo.mockReturnValue(throwError(() => new Error('404')));
    service.show('gallery', 'gone');
    expect(service.error()).toContain('could not be found');
    service.show('gallery', 'f-1', { known: file() });
    expect(service.error()).toBeNull();
  });

  it('ignores bytes for a file no longer shown and forgets on a close', () => {
    const first = new Subject<FileAttachment>();
    const service = setup();
    getFileInfo.mockReturnValueOnce(first).mockReturnValueOnce(of(file({ id: 'f-2' })));
    service.show('chat', 'f-1', { chatId: 'c-1' });
    service.show('chat', 'f-2', { chatId: 'c-1' });
    first.next(file({ id: 'f-1', name: 'late' }));
    expect(service.file()?.id).toBe('f-2');

    service.hide({ forget: true });
    expect(service.open()).toBe(false);
    expect(service.rememberedFileFor('c-1')).toBeNull();
  });

  it('lays out by host and viewport band', () => {
    const service = setup();
    service.show('gallery', 'f-1');
    expect(service.layout()).toBe('inline');
    service.show('chat', 'f-1', { chatId: 'c-1' });
    expect(service.layout()).toBe('docked');
    expect(service.dockedOpen()).toBe(true);
    service.toggleExpanded();
    expect(service.layout()).toBe('full');
    expect(service.floatingOpen()).toBe(true);
    service.toggleExpanded();

    viewport = 'medium';
    window.dispatchEvent(new Event('resize'));
    expect(service.layout()).toBe('overlay');
    expect(service.canEdit()).toBe(true);
    viewport = 'phone';
    window.dispatchEvent(new Event('resize'));
    expect(service.layout()).toBe('phone');
    expect(service.canEdit()).toBe(false);
  });

  it('keeps the docked width within its bounds and remembers it', () => {
    const service = setup();
    const width = window.innerWidth;
    service.setDockedWidth(10);
    expect(service.dockedWidth()).toBe(Math.round(width * 0.4));
    service.setDockedWidth(width);
    expect(service.dockedWidth()).toBe(Math.round(width * 0.75));
    expect(JSON.parse(localStorage.getItem('fileWorkspace.v1')!).dockedWidth).toBe(Math.round(width * 0.75));
  });

  it('opens and closes through the URL', () => {
    const service = setup();
    service.requestOpen('f-1');
    expect(navigate).toHaveBeenCalledWith([], { queryParams: { file: 'f-1' }, queryParamsHandling: 'merge', replaceUrl: false });
    service.requestClose();
    expect(navigate).toHaveBeenLastCalledWith([], { queryParams: { file: null }, queryParamsHandling: 'merge' });
  });

  it('merges a rename into the open file without losing its thread link', () => {
    const service = setup();
    service.show('chat', 'f-1', { chatId: 'c-1' });
    service.update(file({ name: 'renamed.md' }));
    expect(service.file()?.name).toBe('renamed.md');
    expect(service.file()?.chat_id).toBe('c-9');
    service.update(file({ id: 'other' }));
    expect(service.file()?.id).toBe('f-1');
  });
});
