import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal, WritableSignal } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { ActivatedRoute, ParamMap, Router, convertToParamMap, provideRouter } from '@angular/router';
import { BehaviorSubject, of, throwError } from 'rxjs';

import { CommentsPanelState } from '../office/comments/comments-panel-state.service';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ActivePersonalityMediaJob } from '../../core/models/personality-media-job.model';
import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { GalleryViewService } from '../../core/services/gallery-view.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { PersonalityMediaJobService } from '../../core/services/personality-media-job.service';
import { PersonalityService } from '../../core/services/personality.service';
import { GalleryPageComponent } from './gallery-page.component';
import { GALLERY_LAYOUT_KEY } from './helpers/gallery-list.helpers';

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
      lastMove: signal(null),
      images: signal([]),
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

describe('GalleryPageComponent file viewer', () => {
  const doc = (id: string, created_at: string) =>
    ({ id, user_id: 'u', name: `${id}.md`, file_type: 'text/markdown', created_at }) as FileAttachment;
  const picture = {
    id: 'pic',
    user_id: 'u',
    name: 'pic.png',
    file_type: 'image/png',
    created_at: '2026-02-15T00:00:00Z',
  } as FileAttachment;
  const rows = [doc('a', '2026-03-01T00:00:00Z'), picture, doc('b', '2026-02-01T00:00:00Z'), doc('c', '2026-01-01T00:00:00Z')];

  let params: BehaviorSubject<ParamMap>;
  let navigate: ReturnType<typeof vi.fn>;
  let getFileInfo: ReturnType<typeof vi.fn>;
  type Move = { ids: ReadonlySet<string>; folder: string } | null;
  let view: Record<string, unknown> & { openDetail: ReturnType<typeof vi.fn>; lastMove: WritableSignal<Move> };
  let component: GalleryPageComponent;

  beforeEach(() => {
    params = new BehaviorSubject(convertToParamMap({}));
    navigate = vi.fn().mockResolvedValue(true);
    getFileInfo = vi.fn((id: string) => of({ ...rows.find(row => row.id === id)!, chat_id: 'chat-1' }));
    view = {
      importRequestTick: signal(0),
      lastMove: signal<Move>(null),
      images: signal(rows),
      filteredImages: signal(rows),
      setMode: vi.fn(),
      loadInitial: vi.fn(),
      loadFolders: vi.fn(),
      refresh: vi.fn(),
      openDetail: vi.fn(),
      disableGlobalAssociations: vi.fn(),
      setSelectedPersonalityIds: vi.fn(),
    };
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
        { provide: ImageGalleryService, useValue: { getImageUrl: () => '', getFileInfo } },
        { provide: PersonalityService, useValue: { listPersonalities: () => of({ results: [] }), listExpressions: () => of([]) } },
        { provide: ActivatedRoute, useValue: { queryParamMap: params } },
        { provide: Router, useValue: { navigate } },
        { provide: PersonalityMediaJobService, useValue: { activeJob$: new BehaviorSubject(null), refreshActiveJob: () => of(null) } },
      ],
    });
    const fixture = TestBed.createComponent(GalleryPageComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('opens a file tile in the viewer through the URL, and an image in the popup', () => {
    component.onOpenImage('b');
    expect(navigate).toHaveBeenCalledWith([], { queryParams: { file: 'b' }, queryParamsHandling: 'merge' });
    expect(view.openDetail).not.toHaveBeenCalled();

    component.onOpenImage('pic');
    expect(view.openDetail).toHaveBeenCalledWith('pic');
  });

  it('shows the file named by ?file=, then the server copy with its thread', () => {
    params.next(convertToParamMap({ file: 'b' }));

    expect(component.viewerOpen()).toBe(true);
    expect(getFileInfo).toHaveBeenCalledWith('b');
    expect(component.openFile()?.chat_id).toBe('chat-1');
  });

  it('focuses the thread a ?comment= link names, with or without a personality', () => {
    const focus = vi.spyOn(TestBed.inject(CommentsPanelState), 'focusThread');
    params.next(convertToParamMap({ file: 'b', comment: 't-1' }));
    expect(focus).toHaveBeenCalledWith('t-1');
    params.next(convertToParamMap({ file: 'b', comment: 't-2', personality_id: 'p-1' }));
    expect(focus).toHaveBeenCalledWith('t-2');
  });

  it('opens a linked file that is not on a loaded page', () => {
    getFileInfo.mockReturnValue(of(doc('elsewhere', '2025-01-01T00:00:00Z')));
    params.next(convertToParamMap({ file: 'elsewhere' }));
    expect(component.openFile()?.id).toBe('elsewhere');
    expect(component.hasPrevFile()).toBe(false);
    expect(component.hasNextFile()).toBe(false);
  });

  it('says so when a linked file does not exist', () => {
    getFileInfo.mockReturnValue(throwError(() => new Error('404')));
    params.next(convertToParamMap({ file: 'gone' }));
    expect(component.openFile()).toBeNull();
    expect(component.openFileError()).toContain('could not be found');
  });

  it('steps through the files in grid order, skipping images, without a history entry per step', () => {
    params.next(convertToParamMap({ file: 'a' }));
    expect(component.hasPrevFile()).toBe(false);
    expect(component.hasNextFile()).toBe(true);

    component.stepFile(1);
    expect(navigate).toHaveBeenLastCalledWith([], { queryParams: { file: 'b' }, queryParamsHandling: 'merge', replaceUrl: true });

    params.next(convertToParamMap({ file: 'c' }));
    expect(component.hasNextFile()).toBe(false);
  });

  it('closes by dropping the parameter', () => {
    params.next(convertToParamMap({ file: 'a' }));
    component.closeFile();
    expect(navigate).toHaveBeenLastCalledWith([], { queryParams: { file: null }, queryParamsHandling: 'merge' });

    params.next(convertToParamMap({}));
    expect(component.viewerOpen()).toBe(false);
    expect(component.openFile()).toBeNull();
  });

  it('shows a move made from the viewer in its details', () => {
    params.next(convertToParamMap({ file: 'a' }));
    view.lastMove.set({ ids: new Set(['a']), folder: 'notes' });
    TestBed.tick();
    expect(component.openFile()?.folder).toBe('notes');
  });
});

describe('GalleryPageComponent layout toggle', () => {
  const rows = [
    { id: 'p1', user_id: 'u', name: 'one.png', file_type: 'image/png', created_at: '2026-02-01T00:00:00Z' },
    { id: 'p2', user_id: 'u', name: 'two.png', file_type: 'image/png', created_at: '2026-01-01T00:00:00Z' },
  ] as FileAttachment[];
  let fixture: ComponentFixture<GalleryPageComponent>;
  const el = () => fixture.nativeElement as HTMLElement;
  const layoutButton = (label: 'Grid' | 'List') =>
    Array.from(el().querySelectorAll('[aria-label="Gallery layout"] button')).find(
      button => button.textContent?.trim() === label,
    ) as HTMLButtonElement;

  beforeEach(() => {
    localStorage.removeItem(GALLERY_LAYOUT_KEY);
    TestBed.configureTestingModule({
      imports: [GalleryPageComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        {
          provide: ImageGalleryService,
          useValue: {
            getImageUrl: () => '',
            listImages: () => of({ results: rows, total_count: 2, page: 1 }),
            listFolders: () => of([]),
          },
        },
        { provide: PersonalityService, useValue: { listPersonalities: () => of({ results: [] }), listExpressions: () => of([]) } },
        { provide: ActivatedRoute, useValue: { queryParamMap: of(convertToParamMap({})) } },
        { provide: PersonalityMediaJobService, useValue: { activeJob$: new BehaviorSubject(null), refreshActiveJob: () => of(null) } },
      ],
    });
    fixture = TestBed.createComponent(GalleryPageComponent);
    fixture.detectChanges();
  });

  afterEach(() => localStorage.removeItem(GALLERY_LAYOUT_KEY));

  it('switches between the grid and the list table, and says which is on', () => {
    expect(layoutButton('Grid').getAttribute('aria-pressed')).toBe('true');
    expect(el().querySelector('app-gallery-grid')).not.toBeNull();
    expect(el().querySelector('app-gallery-list')).toBeNull();

    layoutButton('List').click();
    fixture.detectChanges();

    expect(layoutButton('List').getAttribute('aria-pressed')).toBe('true');
    expect(el().querySelector('app-gallery-grid')).toBeNull();
    expect(el().querySelectorAll('app-gallery-list .list-row__select')).toHaveLength(2);
    expect(localStorage.getItem(GALLERY_LAYOUT_KEY)).toBe('list');

    layoutButton('Grid').click();
    fixture.detectChanges();
    expect(el().querySelector('app-gallery-grid')).not.toBeNull();
  });

  it('ticks rows in the list into the bulk bar, and select all picks every loaded row', () => {
    layoutButton('List').click();
    fixture.detectChanges();
    expect(el().querySelector('ui-bulk-action-bar')).toBeNull();

    (el().querySelector('.list-row__select') as HTMLInputElement).click();
    fixture.detectChanges();
    expect(el().querySelector('ui-bulk-action-bar')?.textContent).toContain('1 selected');

    (el().querySelector('.ui-data-table__select-all') as HTMLInputElement).click();
    fixture.detectChanges();
    expect(el().querySelector('ui-bulk-action-bar')?.textContent).toContain('2 selected');
  });
});
