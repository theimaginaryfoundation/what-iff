import { provideZonelessChangeDetection, signal, computed } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { of } from 'rxjs';

import { Memory } from '../../core/models/memory.model';
import { MemoryService } from '../../core/services/memory.service';
import { MemoryViewService } from '../../core/services/memory-view.service';
import { PersonalityService } from '../../core/services/personality.service';
import { DEFAULT_MEMORY_VIEW_FILTERS } from './helpers/memory-filter.helpers';
import { MemoriesListTabComponent } from './memories-list-tab.component';

const SAMPLE_MEMORY: Memory = {
  id: 'm-1',
  content: 'Lives in Brooklyn.',
  level: 'global',
  type: 'Context',
  status: 'active',
  confidence: 0.6,
  starred: false,
  created_at: '2026-08-28T00:00:00Z',
  updated_at: '2026-08-29T00:00:00Z',
};

function fakeMediaQueryList(
  media: string,
  matches: boolean,
): MediaQueryList & { setMatches: (next: boolean) => void } {
  const listeners = new Set<(event: Event) => void>();
  const mql = {
    media,
    matches,
    onchange: null,
    addEventListener: (_type: string, listener: EventListener) => {
      listeners.add(listener);
    },
    removeEventListener: (_type: string, listener: EventListener) => {
      listeners.delete(listener);
    },
    dispatchEvent: () => false,
    addListener: () => undefined,
    removeListener: () => undefined,
    setMatches(next: boolean) {
      mql.matches = next;
      listeners.forEach(listener => listener({ matches: next } as MediaQueryListEvent));
    },
  };
  return mql;
}

function makeViewService() {
  const memories = signal<Memory[]>([SAMPLE_MEMORY]);
  const selectedIds = signal<string[]>([]);
  return {
    pageSize: 24,
    memories,
    totalCount: signal(1),
    currentPage: signal(1),
    loading: signal(false),
    deleting: signal(false),
    mutating: signal(false),
    error: signal<string | null>(null),
    filters: signal({ ...DEFAULT_MEMORY_VIEW_FILTERS }),
    selectedIds,
    associationFilterMode: signal<'all' | 'global' | 'personality'>('all'),
    selectedPersonalityIds: signal<string[]>([]),
    hasMemories: computed(() => memories().length > 0),
    totalPages: computed(() => 1),
    allSelected: computed(() => false),
    selectedCount: computed(() => selectedIds().length),
    load: vi.fn(),
    setFilters: vi.fn(),
    applyFilters: vi.fn(),
    clearFilters: vi.fn(),
    toggleSelection: vi.fn(),
    setAllSelected: vi.fn(),
    setSelectedIds: vi.fn(),
    clearSelection: vi.fn(),
    selectAllAssociations: vi.fn(),
    setSelectedPersonalityIds: vi.fn(),
    deleteOne: vi.fn(),
    deleteSelected: vi.fn(),
    patchOne: vi.fn(),
    patchSelected: vi.fn(),
  };
}

describe('MemoriesListTabComponent focus layout', () => {
  let fixture: ComponentFixture<MemoriesListTabComponent>;
  let component: MemoriesListTabComponent;
  let mediaQuery: ReturnType<typeof fakeMediaQueryList>;

  async function setup(desktop: boolean): Promise<void> {
    mediaQuery = fakeMediaQueryList('(min-width: 961px)', desktop);
    vi.spyOn(window, 'matchMedia').mockImplementation(() => mediaQuery);

    await TestBed.configureTestingModule({
      imports: [MemoriesListTabComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: MemoryViewService, useValue: makeViewService() },
        {
          provide: PersonalityService,
          useValue: { listPersonalities: () => of({ results: [], total_count: 0, page: 1 }) },
        },
        {
          provide: MemoryService,
          useValue: { listMergeEvents: () => of({ results: [], total_count: 0, page: 1 }) },
        },
        {
          provide: ActivatedRoute,
          useValue: {
            queryParams: of({}),
            snapshot: { queryParamMap: { get: () => null } },
          },
        },
        { provide: Router, useValue: { navigate: vi.fn().mockName('navigate') } },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(MemoriesListTabComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  }

  afterEach(() => {
    TestBed.resetTestingModule();
    vi.restoreAllMocks();
  });

  it('shows the sticky focus rail on desktop viewports', async () => {
    await setup(true);
    component.focusMemory('m-1');
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('.memories-list--with-panel')).toBeTruthy();
    expect(host.querySelector('.memories-list__panel')).toBeTruthy();
    expect(host.querySelector('[role="dialog"][aria-labelledby="memory-focus-title"]')).toBeNull();
    expect(host.querySelector('[aria-label="Close details"]')).toBeTruthy();
  });

  it('shows a closable modal instead of the rail on mobile viewports', async () => {
    await setup(false);
    component.focusMemory('m-1');
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('.memories-list--with-panel')).toBeNull();
    expect(host.querySelector('.memories-list__panel')).toBeNull();

    const dialog = host.querySelector('[role="dialog"][aria-labelledby="memory-focus-title"]');
    expect(dialog).toBeTruthy();
    expect(host.querySelector('#memory-focus-title')?.textContent).toContain('Memory details');
    expect(host.querySelector('[aria-label="Close"]')).toBeTruthy();
    expect(host.querySelector('[aria-label="Close details"]')).toBeNull();
  });

  it('switches between rail and modal when the breakpoint changes', async () => {
    await setup(false);
    component.focusMemory('m-1');
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('[role="dialog"][aria-labelledby="memory-focus-title"]')).toBeTruthy();

    mediaQuery.setMatches(true);
    fixture.detectChanges();

    expect(host.querySelector('.memories-list__panel')).toBeTruthy();
    expect(host.querySelector('[role="dialog"][aria-labelledby="memory-focus-title"]')).toBeNull();
  });

  it('clears focus when the mobile modal dismisses', async () => {
    await setup(false);
    component.focusMemory('m-1');
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    (host.querySelector('[aria-label="Close"]') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(component.focusedId()).toBeNull();
    expect(host.querySelector('[role="dialog"][aria-labelledby="memory-focus-title"]')).toBeNull();
  });
});
