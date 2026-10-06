import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ActivatedRoute, Router, convertToParamMap } from '@angular/router';
import { BehaviorSubject, of } from 'rxjs';

import { ActivePersonalityMediaJob } from '../../core/models/personality-media-job.model';
import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { GalleryViewService } from '../../core/services/gallery-view.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { PersonalityMediaJobService } from '../../core/services/personality-media-job.service';
import { PersonalityService } from '../../core/services/personality.service';
import { GalleryPageComponent } from './gallery-page.component';

function job(status: string): ActivePersonalityMediaJob {
  return { job_id: 'j1', job_type: 'expression_grid', reference: 'r', status };
}

describe('GalleryPageComponent refreshing', () => {
  let view: Record<string, unknown> & {
    refresh: ReturnType<typeof vi.fn>;
    loadFolders: ReturnType<typeof vi.fn>;
    loadInitial: ReturnType<typeof vi.fn>;
  };
  let activeJob: BehaviorSubject<ActivePersonalityMediaJob | null>;
  let component: GalleryPageComponent;

  beforeEach(() => {
    view = {
      importRequestTick: signal(0),
      filteredImages: signal([
        { id: 'old', created_at: '2026-01-01T00:00:00Z' },
        { id: 'new', created_at: '2026-03-01T00:00:00Z' },
        { id: 'mid', created_at: '2026-02-01T00:00:00Z' },
      ]),
      setMode: vi.fn(),
      loadInitial: vi.fn(),
      loadFolders: vi.fn(),
      refresh: vi.fn(),
      disableGlobalAssociations: vi.fn(),
    };
    activeJob = new BehaviorSubject<ActivePersonalityMediaJob | null>(null);
    TestBed.overrideComponent(GalleryPageComponent, {
      set: { imports: [], template: '', templateUrl: undefined, styleUrl: undefined, styleUrls: undefined },
    });
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        { provide: GalleryViewService, useValue: view },
        { provide: ChatService, useValue: {} },
        { provide: FileAttachmentService, useValue: {} },
        { provide: ConfirmationService, useValue: {} },
        { provide: ImageGalleryService, useValue: { getImageUrl: () => '' } },
        { provide: PersonalityService, useValue: { listPersonalities: () => of({ results: [] }), listExpressions: () => of([]) } },
        { provide: ActivatedRoute, useValue: { queryParamMap: of(convertToParamMap({})) } },
        { provide: Router, useValue: {} },
        { provide: PersonalityMediaJobService, useValue: { activeJob$: activeJob, refreshActiveJob: () => of(activeJob.value) } },
      ],
    });
    const fixture = TestBed.createComponent(GalleryPageComponent);
    component = fixture.componentInstance;
    fixture.detectChanges(); // ngOnInit
    view.refresh.mockClear();
    view.loadFolders.mockClear();
    view.loadInitial.mockClear();
  });

  it('sorts by created date, newest first, and flips when the Created sort is clicked again', () => {
    expect(component.sortedImages().map(i => i.id)).toEqual(['new', 'mid', 'old']);

    component.setSort('created');

    expect(component.sortedImages().map(i => i.id)).toEqual(['old', 'mid', 'new']);
  });

  it('reloads the images and folders when you come back from the Expression Manager', () => {
    component.setMode('expressions');
    expect(view.refresh).not.toHaveBeenCalled();

    component.setMode('gallery');

    expect(view.refresh).toHaveBeenCalledTimes(1);
    expect(view.loadFolders).toHaveBeenCalledTimes(1);
  });

  it('does not reload when the mode did not change or when going to the Expression Manager', () => {
    component.setMode('gallery');
    component.setMode('expressions');
    component.setMode('expressions');

    expect(view.refresh).not.toHaveBeenCalled();
    expect(view.loadFolders).not.toHaveBeenCalled();
  });

  it('reloads when an image job that was running finishes', () => {
    activeJob.next(job('processing'));
    expect(view.refresh).not.toHaveBeenCalled();

    activeJob.next(job('complete'));

    expect(view.refresh).toHaveBeenCalledTimes(1);
    expect(view.loadFolders).toHaveBeenCalledTimes(1);
  });

  it('does not reload for a job that was never running', () => {
    activeJob.next(job('complete'));
    activeJob.next(null);
    expect(view.refresh).not.toHaveBeenCalled();
  });
});
