import { provideZonelessChangeDetection, signal, computed } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, Params, Router } from '@angular/router';
import { Observable, of, Subject, throwError } from 'rxjs';

import { Memory, MemoryMergeEvent } from '../../core/models/memory.model';
import { MemoryService } from '../../core/services/memory.service';
import { MemoryViewService } from '../../core/services/memory-view.service';
import { PersonalityService } from '../../core/services/personality.service';
import { Personality } from '../../core/models/personality.model';
import { DEFAULT_MEMORY_VIEW_FILTERS, MemoryViewFilters, normalizeDateRange } from './helpers/memory-filter.helpers';
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

function fakeMediaQueryList(media: string, matches: boolean): MediaQueryList & { setMatches: (next: boolean) => void } {
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

function makeViewService(
  overrides: {
    memories?: Memory[];
    totalCount?: number;
    currentPage?: number;
    totalPages?: number;
    filters?: Partial<MemoryViewFilters>;
    selectedIds?: string[];
  } = {},
) {
  const memories = signal<Memory[]>(overrides.memories ?? [SAMPLE_MEMORY]);
  const selectedIds = signal<string[]>(overrides.selectedIds ?? []);
  const filters = signal<MemoryViewFilters>({ ...DEFAULT_MEMORY_VIEW_FILTERS, ...overrides.filters });
  return {
    pageSize: 24,
    memories,
    totalCount: signal(overrides.totalCount ?? 1),
    currentPage: signal(overrides.currentPage ?? 1),
    loading: signal(false),
    deleting: signal(false),
    mutating: signal(false),
    error: signal<string | null>(null),
    filters,
    selectedIds,
    associationFilterMode: signal<'all' | 'global' | 'personality'>('all'),
    selectedPersonalityIds: signal<string[]>([]),
    hasMemories: computed(() => memories().length > 0),
    totalPages: computed(() => overrides.totalPages ?? 1),
    allSelected: computed(() => false),
    selectedCount: computed(() => selectedIds().length),
    load: vi.fn(),
    // Mirrors the real service just enough for multi-step wiring tests (e.g. a min-date
    // change followed by a max-date change) to see the previously applied partial filter.
    setFilters: vi.fn((partial: Partial<MemoryViewFilters>) => {
      filters.set({ ...filters(), ...partial });
    }),
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

function makeMemoryService(
  overrides: Partial<{
    listMergeEvents: ReturnType<typeof vi.fn>;
    patchMemory: ReturnType<typeof vi.fn>;
    updateMemoryPin: ReturnType<typeof vi.fn>;
    exportMemories: ReturnType<typeof vi.fn>;
  }> = {},
) {
  return {
    listMergeEvents: overrides.listMergeEvents ?? vi.fn().mockReturnValue(of({ results: [], total_count: 0, page: 1 })),
    patchMemory: overrides.patchMemory ?? vi.fn().mockReturnValue(of(SAMPLE_MEMORY)),
    updateMemoryPin: overrides.updateMemoryPin ?? vi.fn().mockReturnValue(of(SAMPLE_MEMORY)),
    exportMemories: overrides.exportMemories ?? vi.fn().mockReturnValue(of(new Blob())),
  };
}

const PERSONALITY_A: Personality = {
  id: 'persona-a',
  name: 'Ada',
  system_prompt: 'Prompt A',
  auto_pin_memories: false,
  cover_image_id: null,
  cover_image_url: null,
  expressions_enabled: true,
  image_style: 'auto',
  created_at: '2026-08-11T00:00:00Z',
  updated_at: '2026-08-11T00:00:00Z',
  stats: { chat_count: 1, last_used_at: null },
};
const PERSONALITY_B: Personality = { ...PERSONALITY_A, id: 'persona-b', name: 'Bo' };

function mergeEvent(id: string, survivorId = 'm-1'): MemoryMergeEvent {
  return {
    id,
    survivor_memory_id: survivorId,
    merge_type: 'fold_live',
    content: 'Merged memory content',
    duplicates_folded: 1,
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
  };
}

interface HarnessOptions {
  desktop?: boolean;
  view?: ReturnType<typeof makeViewService>;
  personalities?: Personality[];
  personalityError?: boolean;
  memoryService?: ReturnType<typeof makeMemoryService>;
  queryParams?: Observable<Params>;
  initialTab?: string | null;
}

/**
 * Shared harness for the describe blocks below (distinct from the local `setup()`
 * used by the "focus layout" spec above, which stays untouched). Self-resets the
 * testing module so a single test may call this more than once if needed.
 */
async function createComponent(options: HarnessOptions = {}): Promise<{
  fixture: ComponentFixture<MemoriesListTabComponent>;
  component: MemoriesListTabComponent;
  view: ReturnType<typeof makeViewService>;
  router: { navigate: ReturnType<typeof vi.fn> };
  memoryService: ReturnType<typeof makeMemoryService>;
}> {
  TestBed.resetTestingModule();

  const mediaQuery = fakeMediaQueryList('(min-width: 961px)', options.desktop ?? true);
  vi.spyOn(window, 'matchMedia').mockImplementation(() => mediaQuery);

  const view = options.view ?? makeViewService();
  const memoryService = options.memoryService ?? makeMemoryService();
  const personalities = options.personalities ?? [];
  const personalityService = {
    listPersonalities: vi
      .fn()
      .mockReturnValue(
        options.personalityError
          ? throwError(() => new Error('Failed to load personalities'))
          : of({ results: personalities, total_count: personalities.length, page: 1 }),
      ),
  };
  const router = { navigate: vi.fn().mockName('navigate') };

  await TestBed.configureTestingModule({
    imports: [MemoriesListTabComponent],
    providers: [
      provideZonelessChangeDetection(),
      { provide: MemoryViewService, useValue: view },
      { provide: PersonalityService, useValue: personalityService },
      { provide: MemoryService, useValue: memoryService },
      {
        provide: ActivatedRoute,
        useValue: {
          queryParams: options.queryParams ?? of({}),
          snapshot: { queryParamMap: { get: () => options.initialTab ?? null } },
        },
      },
      { provide: Router, useValue: router },
    ],
  }).compileComponents();

  const fixture = TestBed.createComponent(MemoriesListTabComponent);
  const component = fixture.componentInstance;
  fixture.detectChanges();

  return { fixture, component, view, router, memoryService };
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

afterEach(() => {
  TestBed.resetTestingModule();
});

describe('MemoriesListTabComponent bootstrap / ngOnInit', () => {
  it('populates personalities and the Persona select on load success', async () => {
    const { fixture, component } = await createComponent({ personalities: [PERSONALITY_A, PERSONALITY_B] });

    expect(component.personalityNames()).toEqual({
      [PERSONALITY_A.id]: PERSONALITY_A.name,
      [PERSONALITY_B.id]: PERSONALITY_B.name,
    });
    const host = fixture.nativeElement as HTMLElement;
    const select = host.querySelectorAll('.memories-list__filters select')[0] as HTMLSelectElement;
    const options = Array.from(select.querySelectorAll('option'));
    expect(options.map(o => o.textContent?.trim())).toEqual(['All personas', PERSONALITY_A.name, PERSONALITY_B.name]);
  });

  it('falls back to an empty personality list on load error', async () => {
    const { fixture, component } = await createComponent({ personalityError: true });

    expect(component.personalities()).toEqual([]);
    const host = fixture.nativeElement as HTMLElement;
    const select = host.querySelectorAll('.memories-list__filters select')[0] as HTMLSelectElement;
    const options = Array.from(select.querySelectorAll('option'));
    expect(options.map(o => o.textContent?.trim())).toEqual(['All personas']);
  });

  it('applies a second route queryParams emission, overwriting the search box and clearing a stale date error', async () => {
    const paramsSubject = new Subject<Params>();
    const view = makeViewService();
    const { component } = await createComponent({ view, queryParams: paramsSubject.asObservable() });

    paramsSubject.next({ query: 'first term' });
    expect(component.searchDraft()).toBe('first term');
    expect(view.applyFilters).toHaveBeenCalledTimes(1);

    component.dateRangeError.set('stale error');

    paramsSubject.next({});
    expect(component.searchDraft()).toBe('');
    expect(component.dateRangeError()).toBeNull();
    expect(view.applyFilters).toHaveBeenCalledTimes(2);
  });
});

describe('MemoriesListTabComponent filters wiring', () => {
  it('trims the search query and applies it as a filter, merging into the current URL, on Enter', async () => {
    const { fixture, view, router } = await createComponent();
    const host = fixture.nativeElement as HTMLElement;
    const input = host.querySelector('input[type="search"]') as HTMLInputElement;

    input.value = '  coffee  ';
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    fixture.detectChanges();

    expect(view.setFilters).toHaveBeenCalledWith({ query: 'coffee' });
    expect(router.navigate).toHaveBeenCalledWith([], {
      queryParams: { query: 'coffee', tab: null },
      replaceUrl: true,
    });
  });

  it('sets a single selected personality id when a specific persona is chosen', async () => {
    const { fixture, view } = await createComponent({ personalities: [PERSONALITY_A] });
    const host = fixture.nativeElement as HTMLElement;
    const select = host.querySelectorAll('.memories-list__filters select')[0] as HTMLSelectElement;

    select.value = PERSONALITY_A.id;
    select.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    expect(view.setSelectedPersonalityIds).toHaveBeenCalledWith([PERSONALITY_A.id]);
    expect(view.selectAllAssociations).not.toHaveBeenCalled();
    expect(view.setFilters).toHaveBeenCalledWith({ personalityId: PERSONALITY_A.id });
  });

  it('selects all associations when "All personas" is chosen', async () => {
    const { fixture, view } = await createComponent({ personalities: [PERSONALITY_A] });
    const host = fixture.nativeElement as HTMLElement;
    const select = host.querySelectorAll('.memories-list__filters select')[0] as HTMLSelectElement;

    select.value = '';
    select.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    expect(view.selectAllAssociations).toHaveBeenCalled();
    expect(view.setSelectedPersonalityIds).not.toHaveBeenCalled();
  });

  it('applies a valid date range without an error', async () => {
    const { fixture, component, view } = await createComponent();
    const host = fixture.nativeElement as HTMLElement;
    const minInput = host.querySelectorAll('input[type="date"]')[0] as HTMLInputElement;

    minInput.value = '2026-01-05';
    minInput.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    expect(component.dateRangeError()).toBeNull();
    expect(view.setFilters).toHaveBeenCalledWith({ minDate: '2026-01-05', maxDate: '' });
  });

  it('clamps an inverted date range, shows the error banner, and still applies the clamped filter', async () => {
    const { fixture, component, view } = await createComponent();
    const host = fixture.nativeElement as HTMLElement;
    const minInput = host.querySelectorAll('input[type="date"]')[0] as HTMLInputElement;
    const maxInput = host.querySelectorAll('input[type="date"]')[1] as HTMLInputElement;

    minInput.value = '2026-05-10';
    minInput.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    maxInput.value = '2026-01-01';
    maxInput.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    const expected = normalizeDateRange('2026-05-10', '2026-01-01');
    expect(expected.error).toBeTruthy();
    expect(component.dateRangeError()).toBe(expected.error);
    const alert = host.querySelector('.memories-list__date-error[role="alert"]');
    expect(alert?.textContent).toBe(expected.error);
    expect(view.setFilters).toHaveBeenLastCalledWith({ minDate: expected.minDate, maxDate: expected.maxDate });
  });

  describe('openDatePicker', () => {
    it('focuses the input without calling showPicker when unsupported (default jsdom input)', async () => {
      const { fixture } = await createComponent();
      const host = fixture.nativeElement as HTMLElement;
      const minInput = host.querySelectorAll('input[type="date"]')[0] as HTMLInputElement & {
        showPicker?: () => void;
      };
      expect(minInput.showPicker).toBeUndefined();
      const focusSpy = vi.spyOn(minInput, 'focus');

      (host.querySelector('[aria-label="Open start date calendar"]') as HTMLButtonElement).click();

      expect(focusSpy).toHaveBeenCalledTimes(1);
    });

    it('focuses then calls showPicker once when supported', async () => {
      const { fixture } = await createComponent();
      const host = fixture.nativeElement as HTMLElement;
      const minInput = host.querySelectorAll('input[type="date"]')[0] as HTMLInputElement & {
        showPicker?: () => void;
      };
      const showPicker = vi.fn();
      minInput.showPicker = showPicker;
      const focusSpy = vi.spyOn(minInput, 'focus');

      (host.querySelector('[aria-label="Open start date calendar"]') as HTMLButtonElement).click();

      expect(focusSpy).toHaveBeenCalledTimes(1);
      expect(showPicker).toHaveBeenCalledTimes(1);
    });

    it('swallows a showPicker error silently and still focuses exactly once', async () => {
      const { fixture } = await createComponent();
      const host = fixture.nativeElement as HTMLElement;
      const minInput = host.querySelectorAll('input[type="date"]')[0] as HTMLInputElement & {
        showPicker?: () => void;
      };
      minInput.showPicker = vi.fn(() => {
        throw new Error('gesture required');
      });
      const focusSpy = vi.spyOn(minInput, 'focus');
      const btn = host.querySelector('[aria-label="Open start date calendar"]') as HTMLButtonElement;

      expect(() => btn.click()).not.toThrow();
      expect(focusSpy).toHaveBeenCalledTimes(1);
    });
  });

  it.each([['created_desc'], ['created_asc'], ['updated_desc']] as const)('sets sort=%s from the Sort select', async sort => {
    const { fixture, view } = await createComponent();
    const host = fixture.nativeElement as HTMLElement;
    const select = host.querySelectorAll('.memories-list__filters select')[1] as HTMLSelectElement;

    select.value = sort;
    select.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    expect(view.setFilters).toHaveBeenCalledWith({ sort });
  });

  it('resets local state and navigates with only the tab query param on Clear', async () => {
    const { fixture, component, view, router } = await createComponent({ initialTab: 'archived' });
    component.searchDraft.set('leftover');
    component.dateRangeError.set('some error');

    const host = fixture.nativeElement as HTMLElement;
    (host.querySelector('.memories-list__clear') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(component.searchDraft()).toBe('');
    expect(component.dateRangeError()).toBeNull();
    expect(view.clearFilters).toHaveBeenCalled();
    expect(router.navigate).toHaveBeenLastCalledWith([], { queryParams: { tab: 'archived' }, replaceUrl: true });
  });

  it('refreshes using whatever page is currently loaded, not hardcoded to 1', async () => {
    const view = makeViewService({ currentPage: 3 });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;

    (host.querySelector('[aria-label="Refresh"]') as HTMLButtonElement).click();

    expect(view.load).toHaveBeenCalledWith(3);
  });
});

describe('MemoriesListTabComponent status tabs and Summaries read-only enforcement', () => {
  it('drives setStatusFilter from each status tab button', async () => {
    const { fixture, view } = await createComponent();
    const host = fixture.nativeElement as HTMLElement;
    const tabs = host.querySelectorAll('[role="tab"]');

    (tabs[1] as HTMLButtonElement).click();
    expect(view.setFilters).toHaveBeenLastCalledWith({ status: 'inactive', level: 'all' });

    (tabs[2] as HTMLButtonElement).click();
    expect(view.setFilters).toHaveBeenLastCalledWith({ status: 'summaries', level: 'all' });
  });

  it('on Summaries: clears selection, clears focus, filters, then selects all associations — bypassing personality selection', async () => {
    const { fixture, component, view } = await createComponent();
    component.focusMemory('m-1');
    fixture.detectChanges();
    expect(component.focusedId()).toBe('m-1');

    const callOrder: string[] = [];
    view.clearSelection.mockImplementation(() => callOrder.push('clearSelection'));
    view.setFilters.mockImplementation(() => callOrder.push('setFilters'));
    view.selectAllAssociations.mockImplementation(() => callOrder.push('selectAllAssociations'));

    const host = fixture.nativeElement as HTMLElement;
    (host.querySelectorAll('[role="tab"]')[2] as HTMLButtonElement).click();

    expect(callOrder).toEqual(['clearSelection', 'setFilters', 'selectAllAssociations']);
    expect(view.setSelectedPersonalityIds).not.toHaveBeenCalled();
    expect(component.focusedId()).toBeNull();
  });

  it('preserves the current level when switching to Active normally', async () => {
    const view = makeViewService({ filters: { level: 'personality', status: 'inactive' } });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;

    (host.querySelectorAll('[role="tab"]')[0] as HTMLButtonElement).click();

    expect(view.setFilters).toHaveBeenCalledWith({ status: 'active', level: 'personality' });
  });

  it('resets a stale level="summary" to "all" instead of leaking it into Active', async () => {
    const view = makeViewService({ filters: { level: 'summary', status: 'summaries' } });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;

    (host.querySelectorAll('[role="tab"]')[0] as HTMLButtonElement).click();

    expect(view.setFilters).toHaveBeenCalledWith({ status: 'active', level: 'all' });
  });

  it('reflects the active status tab via aria-selected and the active class', async () => {
    const view = makeViewService({ filters: { status: 'inactive' } });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;
    const tabs = Array.from(host.querySelectorAll('[role="tab"]')) as HTMLButtonElement[];

    expect(tabs[1].getAttribute('aria-selected')).toBe('true');
    expect(tabs[1].classList.contains('memories-list__status--active')).toBe(true);
    expect(tabs[0].getAttribute('aria-selected')).toBe('false');
    expect(tabs[0].classList.contains('memories-list__status--active')).toBe(false);
  });

  it('marks the card grid read-only and hides the bulk toolbar in Summaries even with a selection', async () => {
    const view = makeViewService({ filters: { status: 'summaries' }, selectedIds: ['m-1'] });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;

    expect(host.querySelector('[role="toolbar"]')).toBeNull();
    expect(host.querySelector('.memory-card__checkbox')).toBeNull();
    expect(host.querySelector('[aria-label="More actions"]')).toBeNull();
    expect(host.querySelector('.memory-card__footer')?.textContent).toContain('Thread checkpoint summary');
  });

  it('shows the bulk toolbar outside Summaries once something is selected', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;

    expect(host.querySelector('[role="toolbar"]')).toBeTruthy();
  });

  it('renders the read-only footnote and hides mutating footer actions on Summaries', async () => {
    const view = makeViewService({ filters: { status: 'summaries' } });
    const { fixture, component } = await createComponent({ view });
    component.focusMemory('m-1');
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('.focus-panel__footnote')?.textContent).toContain('Summaries are managed with the conversation checkpoint');
    expect(host.querySelector('.focus-panel__actions')).toBeNull();
  });

  it('treats a level="summary" memory as read-only even outside the Summaries tab', async () => {
    const summaryMemory: Memory = { ...SAMPLE_MEMORY, id: 'm-summary', level: 'summary' };
    const view = makeViewService({ memories: [summaryMemory] });
    const { fixture, component } = await createComponent({ view });
    component.focusMemory('m-summary');
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    expect(component.isSummariesView()).toBe(false);
    expect(host.querySelector('.focus-panel__footnote')).toBeTruthy();
    expect(host.querySelector('.focus-panel__actions')).toBeNull();
  });

  it('shows the Summaries hint text on the Summaries tab', async () => {
    const view = makeViewService({ filters: { status: 'summaries' } });
    const { fixture } = await createComponent({ view });
    expect((fixture.nativeElement as HTMLElement).querySelector('.memories-list__hint')?.textContent).toContain(
      'conversation checkpoint summaries (read-only here)',
    );
  });

  it('shows the default hint text outside Summaries', async () => {
    const { fixture } = await createComponent();
    expect((fixture.nativeElement as HTMLElement).querySelector('.memories-list__hint')?.textContent).toContain("retrieval isn't perfect");
  });

  it.each([
    [0, false, '0 memories'],
    [1, false, '1 memory'],
    [2, false, '2 memories'],
    [0, true, '0 summaries'],
    [1, true, '1 summary'],
    [2, true, '2 summaries'],
  ] as const)('countLabel() is %j for n=%s summariesView=%s', async (n, summaries, expected) => {
    const view = makeViewService({ totalCount: n, filters: summaries ? { status: 'summaries' } : {} });
    const { component } = await createComponent({ view });
    expect(component.countLabel()).toBe(expected);
  });
});

describe('MemoriesListTabComponent selection and bulk toolbar', () => {
  it('toggles selection via view.toggleSelection', async () => {
    const { component, view } = await createComponent();
    component.toggleSelection('m-1');
    expect(view.toggleSelection).toHaveBeenCalledWith('m-1');
  });

  it('sets all-selected true/false via the bulk select-all checkbox', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;
    const checkbox = host.querySelector('.memories-list__checkbox') as HTMLInputElement;

    checkbox.checked = true;
    checkbox.dispatchEvent(new Event('change'));
    expect(view.setAllSelected).toHaveBeenCalledWith(true);

    checkbox.checked = false;
    checkbox.dispatchEvent(new Event('change'));
    expect(view.setAllSelected).toHaveBeenCalledWith(false);
  });

  it('clears selection via the toolbar Cancel button', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;
    const buttons = Array.from(host.querySelectorAll('.memories-list__bulk button')) as HTMLButtonElement[];
    const cancelBtn = buttons.find(b => b.textContent?.trim() === 'Cancel')!;

    cancelBtn.click();

    expect(view.clearSelection).toHaveBeenCalled();
  });

  it('hides the bulk toolbar when nothing is selected', async () => {
    const { fixture } = await createComponent();
    expect((fixture.nativeElement as HTMLElement).querySelector('.memories-list__bulk')).toBeNull();
  });

  it('shows the selected count in the toolbar', async () => {
    const view = makeViewService({
      selectedIds: ['m-1'],
      memories: [SAMPLE_MEMORY, { ...SAMPLE_MEMORY, id: 'm-2' }],
    });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('.memories-list__bulk-count')?.textContent).toContain('1 selected');
  });

  it('disables Move, Archive, Export, and Delete while their operations are in-flight, but never Cancel', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    const { fixture } = await createComponent({ view });
    view.mutating.set(true);
    view.deleting.set(true);
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    const buttons = Array.from(host.querySelectorAll('.memories-list__bulk button')) as HTMLButtonElement[];
    const moveBtn = buttons.find(b => b.textContent?.trim() === 'Move')!;
    const archiveBtn = buttons.find(b => /^(Archive|Unarchive)$/.test(b.textContent?.trim() ?? ''))!;
    const exportBtn = buttons.find(b => b.textContent?.trim() === 'Export')!;
    const deleteBtn = host.querySelector('.memories-list__bulk-danger') as HTMLButtonElement;
    const cancelBtn = buttons.find(b => b.textContent?.trim() === 'Cancel')!;

    expect(moveBtn.disabled).toBe(true);
    expect(archiveBtn.disabled).toBe(true);
    expect(exportBtn.disabled).toBe(false);
    expect(deleteBtn.disabled).toBe(true);
    expect(cancelBtn.disabled).toBe(false);
  });

  it('labels the bulk action button Archive outside the Archived tab', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    const { fixture } = await createComponent({ view });
    const buttons = Array.from(
      (fixture.nativeElement as HTMLElement).querySelectorAll('.memories-list__bulk button'),
    ) as HTMLButtonElement[];
    expect(buttons.some(b => b.textContent?.trim() === 'Archive')).toBe(true);
  });

  it('labels the bulk action button Unarchive on the Archived tab', async () => {
    const view = makeViewService({ selectedIds: ['m-1'], filters: { status: 'inactive' } });
    const { fixture } = await createComponent({ view });
    const buttons = Array.from(
      (fixture.nativeElement as HTMLElement).querySelectorAll('.memories-list__bulk button'),
    ) as HTMLButtonElement[];
    expect(buttons.some(b => b.textContent?.trim() === 'Unarchive')).toBe(true);
  });
});

describe('MemoriesListTabComponent bulk actions', () => {
  it('does nothing when Delete Selected is invoked with an empty selection', async () => {
    const { component } = await createComponent();
    component.onDeleteSelected();
    expect(component.deleteModalOpen()).toBe(false);
  });

  it('opens the delete modal with a snapshot copy of the current selection', async () => {
    const view = makeViewService({ selectedIds: ['m-1', 'm-2'] });
    const { component } = await createComponent({ view });

    component.onDeleteSelected();

    expect(component.deleteModalOpen()).toBe(true);
    expect(component.deleteTargetIds()).toEqual(['m-1', 'm-2']);

    view.selectedIds.set(['m-3']);
    expect(component.deleteTargetIds()).toEqual(['m-1', 'm-2']);
  });

  it('uses the batch delete path (not deleteOne) for more than one target', async () => {
    const view = makeViewService({ selectedIds: ['m-1', 'm-2'] });
    view.deleteSelected.mockReturnValue(of(void 0));
    const { component } = await createComponent({ view });

    component.onDeleteSelected();
    component.confirmDelete();

    expect(view.deleteSelected).toHaveBeenCalled();
    expect(view.deleteOne).not.toHaveBeenCalled();
  });

  it('clears the focused memory on delete success when it was among the deleted ids', async () => {
    const view = makeViewService({ selectedIds: ['m-1', 'm-2'] });
    view.deleteSelected.mockReturnValue(of(void 0));
    const { component } = await createComponent({ view });
    component.focusMemory('m-1');

    component.onDeleteSelected();
    component.confirmDelete();

    expect(component.focusedId()).toBeNull();
    expect(component.deleteModalOpen()).toBe(false);
    expect(component.deleteTargetIds()).toEqual([]);
    expect(view.load).toHaveBeenCalledWith(1);
  });

  it('keeps the focused memory on delete success when it was not among the deleted ids', async () => {
    const view = makeViewService({
      selectedIds: ['m-2', 'm-3'],
      memories: [SAMPLE_MEMORY, { ...SAMPLE_MEMORY, id: 'm-2' }, { ...SAMPLE_MEMORY, id: 'm-3' }],
    });
    view.deleteSelected.mockReturnValue(of(void 0));
    const { component } = await createComponent({ view });
    component.focusMemory('m-1');

    component.onDeleteSelected();
    component.confirmDelete();

    expect(component.focusedId()).toBe('m-1');
  });

  it('on delete error: closes the modal but keeps the target ids and does not reload', async () => {
    const view = makeViewService({ selectedIds: ['m-1', 'm-2'] });
    view.deleteSelected.mockReturnValue(throwError(() => new Error('nope')));
    const { component } = await createComponent({ view });

    component.onDeleteSelected();
    component.confirmDelete();

    expect(component.deleteModalOpen()).toBe(false);
    expect(component.deleteTargetIds()).toEqual(['m-1', 'm-2']);
    expect(view.load).not.toHaveBeenCalled();
  });

  it('closeDeleteModal clears both the open flag and the target ids (unlike the error path)', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    const { component } = await createComponent({ view });

    component.onDeleteSelected();
    component.closeDeleteModal();

    expect(component.deleteModalOpen()).toBe(false);
    expect(component.deleteTargetIds()).toEqual([]);
  });

  it('shows the Deleting… label and disables both delete-modal buttons while deleting', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    view.deleting.set(true);
    const { fixture, component } = await createComponent({ view });

    component.onDeleteSelected();
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    const buttons = Array.from(host.querySelectorAll('.ui-modal__footer button')) as HTMLButtonElement[];
    expect(buttons.map(b => b.textContent?.trim())).toEqual(['Cancel', 'Deleting…']);
    expect(buttons.every(b => b.disabled)).toBe(true);
  });

  it('archives the selection, flips status by archived view, and reloads on both success and error', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    view.patchSelected.mockReturnValueOnce(of(void 0)).mockReturnValueOnce(throwError(() => new Error('x')));
    const { component } = await createComponent({ view });

    component.onArchiveSelected();
    expect(view.patchSelected).toHaveBeenCalledWith({ status: 'inactive' });
    expect(view.load).toHaveBeenCalledWith(1);

    view.load.mockClear();
    component.onArchiveSelected();
    expect(view.load).toHaveBeenCalledWith(1);
  });

  it('flips the archive patch to "active" when already on the Archived view', async () => {
    const view = makeViewService({ selectedIds: ['m-1'], filters: { status: 'inactive' } });
    view.patchSelected.mockReturnValue(of(void 0));
    const { component } = await createComponent({ view });

    component.onArchiveSelected();

    expect(view.patchSelected).toHaveBeenCalledWith({ status: 'active' });
  });

  it('opens the move menu with a snapshot copy of the selection, not a live reference', async () => {
    const view = makeViewService({ selectedIds: ['m-1', 'm-2'] });
    const { component } = await createComponent({ view });

    component.onMoveSelected();
    expect(component.moveTargetIds()).toEqual(['m-1', 'm-2']);

    view.selectedIds.set(['m-3']);
    expect(component.moveTargetIds()).toEqual(['m-1', 'm-2']);
  });

  it('moves multiple selected memories by calling setSelectedIds before patchSelected', async () => {
    const view = makeViewService({ selectedIds: ['m-1', 'm-2'] });
    const { component } = await createComponent({ view });

    const order: string[] = [];
    view.setSelectedIds.mockImplementation(() => order.push('setSelectedIds'));
    view.patchSelected.mockImplementation(() => {
      order.push('patchSelected');
      return of(void 0);
    });

    component.onMoveSelected();
    component.moveToPersonality('persona-1');

    expect(order).toEqual(['setSelectedIds', 'patchSelected']);
    expect(view.setSelectedIds).toHaveBeenCalledWith(['m-1', 'm-2']);
    expect(view.patchSelected).toHaveBeenCalledWith({ level: 'personality', pinned_personality_id: 'persona-1' });
  });

  it('moves to Global/shared with a null personality pin', async () => {
    const view = makeViewService({ selectedIds: ['m-1', 'm-2'] });
    view.patchSelected.mockReturnValue(of(void 0));
    const { component } = await createComponent({ view });

    component.onMoveSelected();
    component.moveToPersonality(null);

    expect(view.patchSelected).toHaveBeenCalledWith({ level: 'global', pinned_personality_id: null });
  });

  it('closes the move menu and reloads on success; closes only (no reload) on error', async () => {
    const view = makeViewService({ selectedIds: ['m-1'] });
    const { component } = await createComponent({ view });

    view.patchOne.mockReturnValue(of(void 0));
    component.onMoveOne('m-1');
    component.moveToPersonality('persona-1');
    expect(component.moveMenuOpen()).toBe(false);
    expect(view.load).toHaveBeenCalledWith(1);

    view.load.mockClear();
    view.patchOne.mockReturnValue(throwError(() => new Error('nope')));
    component.onMoveOne('m-1');
    component.moveToPersonality('persona-1');
    expect(component.moveMenuOpen()).toBe(false);
    expect(view.load).not.toHaveBeenCalled();
  });

  it('closes the move menu when clicking the backdrop overlay', async () => {
    const { fixture, component } = await createComponent();
    component.openMoveMenu(['m-1']);
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    (host.querySelector('.memories-list__move-overlay') as HTMLElement).click();

    expect(component.moveMenuOpen()).toBe(false);
  });

  it('does not close the move menu when clicking inside the menu panel (stopPropagation guard)', async () => {
    const { fixture, component } = await createComponent();
    component.openMoveMenu(['m-1']);
    fixture.detectChanges();

    const host = fixture.nativeElement as HTMLElement;
    (host.querySelector('.memories-list__move-menu') as HTMLElement).click();

    expect(component.moveMenuOpen()).toBe(true);
  });

  it('exports memories: creates a download anchor, clicks it, and revokes the URL', async () => {
    const blob = new Blob(['data']);
    const memoryService = makeMemoryService({ exportMemories: vi.fn().mockReturnValue(of(blob)) });
    const { component } = await createComponent({ memoryService });

    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:export');
    vi.spyOn(URL, 'revokeObjectURL').mockReturnValue(undefined);
    const click = vi.fn();
    const anchor = document.createElement('a');
    const realCreateElement = document.createElement.bind(document);
    vi.spyOn(anchor, 'click').mockImplementation(click);
    vi.spyOn(document, 'createElement').mockImplementation(((tagName: string) =>
      tagName === 'a' ? anchor : realCreateElement(tagName)) as typeof document.createElement);

    component.exportMemories();

    expect(anchor.href).toBe('blob:export');
    expect(anchor.download).toBe('memories-export.zip');
    expect(click).toHaveBeenCalled();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:export');
    expect(component.exporting()).toBe(false);
  });

  it('toggles exporting() true then false around an in-flight export', async () => {
    const exportSubject = new Subject<Blob>();
    const memoryService = makeMemoryService({ exportMemories: vi.fn().mockReturnValue(exportSubject.asObservable()) });
    const { component } = await createComponent({ memoryService });

    component.exportMemories();
    expect(component.exporting()).toBe(true);

    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:x');
    vi.spyOn(URL, 'revokeObjectURL').mockReturnValue(undefined);
    const anchor = document.createElement('a');
    const realCreateElement = document.createElement.bind(document);
    vi.spyOn(anchor, 'click').mockImplementation(() => {});
    vi.spyOn(document, 'createElement').mockImplementation(((tagName: string) =>
      tagName === 'a' ? anchor : realCreateElement(tagName)) as typeof document.createElement);

    exportSubject.next(new Blob(['x']));
    exportSubject.complete();

    expect(component.exporting()).toBe(false);
  });

  it('resets exporting() on failure without touching the DOM', async () => {
    const memoryService = makeMemoryService({
      exportMemories: vi.fn().mockReturnValue(throwError(() => new Error('fail'))),
    });
    const { component } = await createComponent({ memoryService });
    const createElementSpy = vi.spyOn(document, 'createElement');

    component.exportMemories();

    expect(component.exporting()).toBe(false);
    expect(createElementSpy).not.toHaveBeenCalled();
  });
});

describe('MemoriesListTabComponent per-card single actions', () => {
  it('opens the delete modal for a single memory id, distinct from the batch path', async () => {
    const view = makeViewService();
    view.deleteOne.mockReturnValue(of(void 0));
    const { component } = await createComponent({ view });

    component.onDeleteSingle('m-1');
    expect(component.deleteTargetIds()).toEqual(['m-1']);
    expect(component.deleteModalOpen()).toBe(true);

    component.confirmDelete();
    expect(view.deleteOne).toHaveBeenCalledWith('m-1');
    expect(view.deleteSelected).not.toHaveBeenCalled();
  });

  it('reloads the list on both inline-save success and failure', async () => {
    const patchMemory = vi
      .fn()
      .mockReturnValueOnce(of(SAMPLE_MEMORY))
      .mockReturnValueOnce(throwError(() => new Error('fail')));
    const memoryService = makeMemoryService({ patchMemory });
    const view = makeViewService({ currentPage: 2 });
    const { component } = await createComponent({ view, memoryService });

    component.onInlineSaveMemory({ id: 'm-1', content: 'Updated' });
    expect(patchMemory).toHaveBeenCalledWith('m-1', { content: 'Updated' });
    expect(view.load).toHaveBeenCalledWith(2);

    view.load.mockClear();
    component.onInlineSaveMemory({ id: 'm-1', content: 'Updated again' });
    expect(view.load).toHaveBeenCalledWith(2);
  });

  it('navigates to the memory detail route on focus-panel edit', async () => {
    const { component, router } = await createComponent();
    component.onFocusEdit('m-1');
    expect(router.navigate).toHaveBeenCalledWith(['/memories', 'm-1']);
  });

  it('tracks pinUpdatingId mid-flight and clears it (reloading) on success', async () => {
    const pinSubject = new Subject<Memory>();
    const updateMemoryPin = vi.fn().mockReturnValue(pinSubject.asObservable());
    const memoryService = makeMemoryService({ updateMemoryPin });
    const view = makeViewService();
    const { component } = await createComponent({ view, memoryService });

    component.onMemoryPinChange({ id: 'm-1', pinnedPersonalityId: 'persona-1' });
    expect(component.pinUpdatingId()).toBe('m-1');
    expect(updateMemoryPin).toHaveBeenCalledWith('m-1', 'persona-1');

    pinSubject.next(SAMPLE_MEMORY);
    pinSubject.complete();

    expect(component.pinUpdatingId()).toBeNull();
    expect(view.load).toHaveBeenCalledWith(1);
  });

  it('clears pinUpdatingId and reloads on pin update error too', async () => {
    const updateMemoryPin = vi.fn().mockReturnValue(throwError(() => new Error('fail')));
    const memoryService = makeMemoryService({ updateMemoryPin });
    const view = makeViewService();
    const { component } = await createComponent({ view, memoryService });

    component.onMemoryPinChange({ id: 'm-1', pinnedPersonalityId: null });

    expect(component.pinUpdatingId()).toBeNull();
    expect(view.load).toHaveBeenCalledWith(1);
  });

  it('toggles star via patchOne and reloads on both success and error', async () => {
    const view = makeViewService();
    view.patchOne.mockReturnValueOnce(of(void 0)).mockReturnValueOnce(throwError(() => new Error('x')));
    const { component } = await createComponent({ view });

    component.onToggleStar({ id: 'm-1', starred: true });
    expect(view.patchOne).toHaveBeenCalledWith('m-1', { starred: true });
    expect(view.load).toHaveBeenCalledWith(1);

    view.load.mockClear();
    component.onToggleStar({ id: 'm-1', starred: false });
    expect(view.load).toHaveBeenCalledWith(1);
  });

  it('archives a single memory, flipping status by archived view, and reloads on both outcomes', async () => {
    const view = makeViewService();
    view.patchOne.mockReturnValueOnce(of(void 0)).mockReturnValueOnce(throwError(() => new Error('x')));
    const { component } = await createComponent({ view });

    component.onArchiveOne('m-1');
    expect(view.patchOne).toHaveBeenCalledWith('m-1', { status: 'inactive' });
    expect(view.load).toHaveBeenCalledWith(1);

    view.load.mockClear();
    component.onArchiveOne('m-1');
    expect(view.load).toHaveBeenCalledWith(1);
  });

  it('opens the move menu for a single id and moves it via patchOne, without touching setSelectedIds', async () => {
    const view = makeViewService();
    view.patchOne.mockReturnValue(of(void 0));
    const { component } = await createComponent({ view });

    component.onMoveOne('m-1');
    expect(component.moveTargetIds()).toEqual(['m-1']);

    component.moveToPersonality('persona-1');
    expect(view.patchOne).toHaveBeenCalledWith('m-1', { level: 'personality', pinned_personality_id: 'persona-1' });
    expect(view.setSelectedIds).not.toHaveBeenCalled();
  });
});

describe('MemoriesListTabComponent focus panel wiring and merge events', () => {
  it('loads merge events once and does not reload for a second focused memory', async () => {
    const listMergeEvents = vi.fn().mockReturnValue(of({ results: [mergeEvent('e-1')], total_count: 1, page: 1 }));
    const memoryService = makeMemoryService({ listMergeEvents });
    const view = makeViewService({ memories: [SAMPLE_MEMORY, { ...SAMPLE_MEMORY, id: 'm-2' }] });
    const { component } = await createComponent({ view, memoryService });

    component.focusMemory('m-1');
    expect(listMergeEvents).toHaveBeenCalledTimes(1);
    expect(component.mergeEventsLoading()).toBe(false);
    expect(component.mergeEvents().map(e => e.id)).toEqual(['e-1']);

    component.focusMemory('m-2');
    expect(listMergeEvents).toHaveBeenCalledTimes(1);
  });

  it('sets mergeEventsLoading true while the request is pending, then false on completion', async () => {
    const subject = new Subject<{ results: MemoryMergeEvent[]; total_count: number; page: number }>();
    const memoryService = makeMemoryService({ listMergeEvents: vi.fn().mockReturnValue(subject.asObservable()) });
    const { component } = await createComponent({ memoryService });

    component.focusMemory('m-1');
    expect(component.mergeEventsLoading()).toBe(true);

    subject.next({ results: [], total_count: 0, page: 1 });
    subject.complete();

    expect(component.mergeEventsLoading()).toBe(false);
  });

  it('clears merge events to an empty list on error and does not retry on a later focus', async () => {
    const listMergeEvents = vi.fn().mockReturnValue(throwError(() => new Error('fail')));
    const memoryService = makeMemoryService({ listMergeEvents });
    const view = makeViewService({ memories: [SAMPLE_MEMORY, { ...SAMPLE_MEMORY, id: 'm-2' }] });
    const { component } = await createComponent({ view, memoryService });

    component.focusMemory('m-1');
    expect(component.mergeEvents()).toEqual([]);
    expect(component.mergeEventsLoading()).toBe(false);

    component.focusMemory('m-2');
    expect(listMergeEvents).toHaveBeenCalledTimes(1);
  });

  it('clearFocus does not reset mergeEvents or the loaded flag', async () => {
    const memoryService = makeMemoryService({
      listMergeEvents: vi.fn().mockReturnValue(of({ results: [mergeEvent('e-1')], total_count: 1, page: 1 })),
    });
    const { component } = await createComponent({ memoryService });

    component.focusMemory('m-1');
    expect(component.mergeEvents().length).toBe(1);

    component.clearFocus();
    expect(component.focusedId()).toBeNull();
    expect(component.mergeEvents().length).toBe(1);

    component.focusMemory('m-1');
    expect(memoryService.listMergeEvents).toHaveBeenCalledTimes(1);
  });

  it('focusedMemory() is null once the focused id no longer matches any loaded memory', async () => {
    const view = makeViewService();
    const { component } = await createComponent({ view });

    component.focusMemory('m-1');
    expect(component.focusedMemory()?.id).toBe('m-1');

    view.memories.set([]);
    expect(component.focusedMemory()).toBeNull();
  });
});

describe('MemoriesListTabComponent pagination', () => {
  it('goes to the next/previous page via the card grid buttons', async () => {
    const view = makeViewService({ currentPage: 2, totalPages: 3 });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;
    const buttons = Array.from(host.querySelectorAll('button')) as HTMLButtonElement[];
    const nextBtn = buttons.find(b => b.textContent?.trim() === 'Next')!;
    const prevBtn = buttons.find(b => b.textContent?.trim() === 'Previous')!;

    expect(prevBtn.disabled).toBe(false);
    expect(nextBtn.disabled).toBe(false);

    nextBtn.click();
    expect(view.load).toHaveBeenCalledWith(3);

    prevBtn.click();
    expect(view.load).toHaveBeenCalledWith(1);
  });

  it('disables Previous on the first page and Next on the last page', async () => {
    const view = makeViewService({ currentPage: 1, totalPages: 3 });
    const { fixture } = await createComponent({ view });
    const host = fixture.nativeElement as HTMLElement;
    let buttons = Array.from(host.querySelectorAll('button')) as HTMLButtonElement[];
    expect(buttons.find(b => b.textContent?.trim() === 'Previous')!.disabled).toBe(true);
    expect(buttons.find(b => b.textContent?.trim() === 'Next')!.disabled).toBe(false);

    view.currentPage.set(3);
    fixture.detectChanges();
    buttons = Array.from(host.querySelectorAll('button')) as HTMLButtonElement[];
    expect(buttons.find(b => b.textContent?.trim() === 'Next')!.disabled).toBe(true);
  });
});

describe('MemoriesListTabComponent desktop focus layout fallback', () => {
  it('defaults to desktop layout and does not crash when matchMedia is unavailable', async () => {
    const original = window.matchMedia;
    // @ts-expect-error simulate a browser without matchMedia support
    delete window.matchMedia;

    try {
      TestBed.resetTestingModule();
      const view = makeViewService();
      await TestBed.configureTestingModule({
        imports: [MemoriesListTabComponent],
        providers: [
          provideZonelessChangeDetection(),
          { provide: MemoryViewService, useValue: view },
          {
            provide: PersonalityService,
            useValue: { listPersonalities: () => of({ results: [], total_count: 0, page: 1 }) },
          },
          { provide: MemoryService, useValue: makeMemoryService() },
          {
            provide: ActivatedRoute,
            useValue: { queryParams: of({}), snapshot: { queryParamMap: { get: () => null } } },
          },
          { provide: Router, useValue: { navigate: vi.fn() } },
        ],
      }).compileComponents();

      const fixture = TestBed.createComponent(MemoriesListTabComponent);
      expect(() => fixture.detectChanges()).not.toThrow();
      expect(fixture.componentInstance.isDesktopFocusLayout()).toBe(true);
    } finally {
      window.matchMedia = original;
    }
  });
});

describe('MemoriesListTabComponent computed signal edge cases', () => {
  it('personalityNames() is an empty map when there are no personalities', async () => {
    const { component } = await createComponent({ personalities: [] });
    expect(component.personalityNames()).toEqual({});
  });

  it('personalityNames() maps id to label for each loaded personality', async () => {
    const { component } = await createComponent({ personalities: [PERSONALITY_A, PERSONALITY_B] });
    expect(component.personalityNames()).toEqual({
      [PERSONALITY_A.id]: PERSONALITY_A.name,
      [PERSONALITY_B.id]: PERSONALITY_B.name,
    });
  });
});
