import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of } from 'rxjs';

import { RightPanelService } from '../../core/services/right-panel.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { FileWorkspaceService, LARGE_VIEWPORT_QUERY, MEDIUM_VIEWPORT_QUERY } from '../../features/file-workspace/file-workspace.service';
import { RightPanelHostComponent } from './right-panel-host.component';

describe('RightPanelHostComponent', () => {
  let fixture: ComponentFixture<RightPanelHostComponent>;

  beforeEach(async () => {
    localStorage.clear();
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: query === LARGE_VIEWPORT_QUERY || query === MEDIUM_VIEWPORT_QUERY }) as MediaQueryList);
    await TestBed.configureTestingModule({
      imports: [RightPanelHostComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: ImageGalleryService, useValue: { getFileInfo: () => of({ id: 'f-1', user_id: 'u', name: 'a.txt', file_type: 'text/plain', created_at: '' }) } },
        { provide: FileAttachmentService, useValue: { downloadFileAttachment: () => of(new Blob(['x'])) } },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(RightPanelHostComponent);
  });

  afterEach(() => vi.restoreAllMocks());

  it('shows the context panel in the column, and as a flyout over a docked file', async () => {
    const el = fixture.nativeElement as HTMLElement;
    TestBed.inject(RightPanelService).setVisible(true);
    fixture.detectChanges();
    expect(el.querySelector('.app-right-panel:not(.app-right-panel--workspace) app-context-panel')).not.toBeNull();
    expect(el.querySelector('app-file-workspace-panel')).toBeNull();

    TestBed.inject(FileWorkspaceService).show('chat', 'f-1', { chatId: 'c-1' });
    fixture.detectChanges();
    await fixture.whenStable();
    expect(el.querySelector('.app-right-panel--workspace app-file-workspace-panel')).not.toBeNull();
    expect(el.querySelector('.app-right-panel__flyout app-context-panel')).not.toBeNull();

    TestBed.inject(RightPanelService).setVisible(false);
    fixture.detectChanges();
    expect(el.querySelector('.app-right-panel__flyout')).toBeNull();
    expect(el.querySelector('app-file-workspace-panel')).not.toBeNull();
  });
});
