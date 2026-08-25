import type { Mock, MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { signal } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideRouter, Router } from '@angular/router';
import { BehaviorSubject } from 'rxjs';

import { AuthService } from '../../core/services/auth.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { CommandPaletteService } from '../../core/services/command-palette.service';
import { GalleryViewService } from '../../core/services/gallery-view.service';
import { MemoryViewService } from '../../core/services/memory-view.service';
import { ModeViewService } from '../../core/services/mode-view.service';
import { NavService } from '../../core/services/nav.service';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualViewService } from '../../core/services/ritual-view.service';
import { ThreadListService } from '../../core/services/thread-list.service';
import { AppSidebarComponent } from './app-sidebar.component';

describe('AppSidebarComponent', () => {
    let navSpy: Pick<MockedObject<NavService>, 'setMode' | 'toggleCollapsed' | 'setCollapsed' | 'mode' | 'collapsed'>;
    let paletteSpy: Pick<MockedObject<CommandPaletteService>, 'open' | 'close' | 'toggle' | 'register' | 'registerCommand' | 'setQuery' | 'runCommand' | 'visible' | 'query' | 'sections' | 'loading' | 'commandResults'>;
    let threadsStub: any;
    let galleryViewStub: any;
    let memoryViewStub: any;
    let ritualViewStub: any;
    let modeViewStub: any;
    let router: Router;
    let routerUrlSpy: Mock;
    let navMode: 'app' | 'config';
    let confirmationSpy: Pick<MockedObject<ConfirmationService>, 'confirm'>;

    beforeEach(async () => {
        navMode = 'app';

        navSpy = {
            setMode: vi.fn().mockName("NavService.setMode"),
            toggleCollapsed: vi.fn().mockName("NavService.toggleCollapsed"),
            setCollapsed: vi.fn().mockName("NavService.setCollapsed"),
            mode: (() => navMode) as any,
            collapsed: (() => false) as any
        } as unknown as Pick<MockedObject<NavService>, 'setMode' | 'toggleCollapsed' | 'setCollapsed' | 'mode' | 'collapsed'>;

        paletteSpy = {
            open: vi.fn().mockName("CommandPaletteService.open"),
            close: vi.fn().mockName("CommandPaletteService.close"),
            toggle: vi.fn().mockName("CommandPaletteService.toggle"),
            register: vi.fn().mockName("CommandPaletteService.register"),
            registerCommand: vi.fn().mockName("CommandPaletteService.registerCommand"),
            setQuery: vi.fn().mockName("CommandPaletteService.setQuery"),
            runCommand: vi.fn().mockName("CommandPaletteService.runCommand"),
            visible: (() => false) as any,
            query: (() => '') as any,
            sections: (() => []) as any,
            loading: (() => false) as any,
            commandResults: (() => []) as any
        } as unknown as Pick<MockedObject<CommandPaletteService>, 'open' | 'close' | 'toggle' | 'register' | 'registerCommand' | 'setQuery' | 'runCommand' | 'visible' | 'query' | 'sections' | 'loading' | 'commandResults'>;

        const authSpy = {
            currentUser: (() => ({ id: '1', username: 'jane', email: 'j@x.io' })) as any
        };
        const personalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        };
        personalityService.listPersonalities.mockReturnValue(new BehaviorSubject({
            results: [
                {
                    id: 'persona-1',
                    name: 'Ada',
                    system_prompt: '',
                    auto_pin_memories: false,
                    expressions_enabled: true,
                    image_style: 'auto', cover_image_id: null,
                    cover_image_url: '/zook.jpg',
                    created_at: '',
                    updated_at: '',
                    stats: { chat_count: 1, last_used_at: null },
                },
            ],
            total_count: 1,
            page: 1,
        }).asObservable());
        threadsStub = {
            refresh: vi.fn().mockName('refresh').mockResolvedValue(undefined),
            unpinAll: vi.fn().mockName('unpinAll').mockResolvedValue(undefined),
            setSidebarPersonalityFilter: vi.fn().mockName('setSidebarPersonalityFilter'),
            setActiveThreadId: vi.fn().mockName('setActiveThreadId'),
            activeThreadId: signal<string | null>('thread-1'),
            selectedPersonalityId: signal<string | null>(null),
            personalities: signal([
                { id: 'persona-1', label: 'Ada' },
                { id: 'persona-2', label: 'Vera' },
            ]),
            filteredThreads: signal([{ id: 'thread-1', name: 'Thread 1', is_favorite: true }]),
            recentOpenedIds: signal<string[]>([]),
            pinnedThreads: signal([
                { id: 'thread-1', name: 'Thread 1', is_favorite: true, personality_id: 'persona-1' },
            ]),
        };
        galleryViewStub = {
            mode: signal<'gallery' | 'expressions'>('gallery'),
            associationFilterMode: signal<'all' | 'global' | 'personality'>('all'),
            selectedPersonalityIds: signal<string[]>([]),
            importRequestTick: signal(0),
            setSelectedPersonalityIds: vi.fn().mockName('setSelectedPersonalityIds'),
            selectAllAssociations: vi.fn().mockName('selectAllAssociations'),
            selectGlobalAssociations: vi.fn().mockName('selectGlobalAssociations'),
            requestImportModalOpen: vi.fn().mockName('requestImportModalOpen'),
        };
        memoryViewStub = {
            associationFilterMode: signal<'all' | 'global' | 'personality'>('all'),
            selectedPersonalityIds: signal<string[]>([]),
            filters: signal({
                scope: 'all',
                level: 'all',
                sort: 'created_desc',
                query: '',
                personalityId: '',
                chatId: '',
                minDate: '',
                maxDate: '',
            }),
            setFilters: vi.fn().mockName('setFilters'),
            setSelectedPersonalityIds: vi.fn().mockName('setSelectedPersonalityIds'),
            selectAllAssociations: vi.fn().mockName('selectAllAssociations'),
            selectGlobalAssociations: vi.fn().mockName('selectGlobalAssociations'),
        };
        ritualViewStub = {
            filters: signal({
                query: '',
                personalityId: '',
                personalityIds: [],
                globalOnly: false,
                hasHotkey: 'all',
                sort: 'name_asc',
                minDate: '',
                maxDate: '',
            }),
            setFilters: vi.fn().mockName('setFilters'),
        };
        confirmationSpy = {
            confirm: vi.fn().mockName("ConfirmationService.confirm")
        } as unknown as Pick<MockedObject<ConfirmationService>, 'confirm'>;
        confirmationSpy.confirm.mockResolvedValue(true);

        modeViewStub = {
            associationFilterMode: signal<'all' | 'personality'>('all'),
            selectedPersonalityIds: signal<string[]>([]),
            createRequestTick: signal(0),
            setSelectedPersonalityIds: vi.fn().mockName('setSelectedPersonalityIds'),
            selectAllAssociations: vi.fn().mockName('selectAllAssociations'),
            requestCreateModalOpen: vi.fn().mockName('requestCreateModalOpen'),
        };

        await TestBed.configureTestingModule({
            imports: [AppSidebarComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                provideRouter([]),
                { provide: NavService, useValue: navSpy },
                { provide: CommandPaletteService, useValue: paletteSpy },
                { provide: AuthService, useValue: authSpy },
                { provide: PersonalityService, useValue: personalityService },
                { provide: ThreadListService, useValue: threadsStub },
                { provide: GalleryViewService, useValue: galleryViewStub },
                { provide: MemoryViewService, useValue: memoryViewStub },
                { provide: ModeViewService, useValue: modeViewStub },
                { provide: RitualViewService, useValue: ritualViewStub },
                { provide: ConfirmationService, useValue: confirmationSpy },
            ],
        }).compileComponents();

        router = TestBed.inject(Router);
        routerUrlSpy = vi.spyOn(router, 'url', 'get').mockReturnValue('/chat');
    });

    function create() {
        const fixture = TestBed.createComponent(AppSidebarComponent);
        fixture.detectChanges();
        return fixture;
    }

    it('renders top nav tabs by default', () => {
        const fixture = create();
        const tabs = fixture.nativeElement.querySelectorAll('.app-sidebar__tab');
        expect(tabs.length).toBeGreaterThan(0);
    });

    it('shows Tools and Jobs nav entries in config mode when jobs feature is enabled', () => {
        navMode = 'config';
        const fixture = create();
        const tools = fixture.nativeElement.querySelector('a.app-sidebar__tab[aria-label="Tools"]');
        const jobs = fixture.nativeElement.querySelector('a.app-sidebar__tab[aria-label="Jobs"]');
        expect(tools).toBeTruthy();
        expect(jobs).toBeTruthy();
    });

    it('keeps Jobs nav entry visible in config mode', () => {
        navMode = 'config';
        const fixture = create();
        const tools = fixture.nativeElement.querySelector('a.app-sidebar__tab[aria-label="Tools"]');
        const jobs = fixture.nativeElement.querySelector('a.app-sidebar__tab[aria-label="Jobs"]');
        expect(tools).toBeTruthy();
        expect(jobs).toBeTruthy();
    });

    it('the config toggle button switches mode', () => {
        const fixture = create();
        const configButton = fixture.nativeElement.querySelector('button[aria-label="Switch to configuration mode"]') as HTMLButtonElement;
        configButton.click();
        expect(navSpy.setMode).toHaveBeenCalledWith('config');
    });

    it('the collapse button still toggles collapsed state', () => {
        const fixture = create();
        const collapse = fixture.nativeElement.querySelector('.app-sidebar__collapse-handle button') as HTMLButtonElement;
        expect(collapse.getAttribute('aria-expanded')).toBe('true');
        collapse.click();
        expect(navSpy.toggleCollapsed).toHaveBeenCalled();
    });

    it('renders starred threads and allows unstar-all after confirmation', async () => {
        const fixture = create();
        expect(fixture.nativeElement.textContent).toContain('Starred Threads');
        const unstar = fixture.nativeElement.querySelector('.app-sidebar__pinned-header button') as HTMLButtonElement;
        unstar.click();
        await fixture.whenStable();
        expect(confirmationSpy.confirm).toHaveBeenCalledWith({
            title: 'Unstar all threads',
            message: 'Are you sure you want to unstar all threads?',
            confirmText: 'Unstar all',
            cancelText: 'Cancel',
            type: 'warning',
        });
        expect(threadsStub.unpinAll).toHaveBeenCalled();
    });

    it('does not unstar all when confirmation is cancelled', async () => {
        confirmationSpy.confirm.mockResolvedValue(false);
        const fixture = create();
        const unstar = fixture.nativeElement.querySelector('.app-sidebar__pinned-header button') as HTMLButtonElement;
        unstar.click();
        await fixture.whenStable();
        expect(confirmationSpy.confirm).toHaveBeenCalled();
        expect(threadsStub.unpinAll).not.toHaveBeenCalled();
    });

    it('renders recent threads below starred threads', () => {
        threadsStub.filteredThreads.set([
            { id: 'thread-1', name: 'Starred', is_favorite: true, personality_id: 'persona-1' },
            { id: 'thread-2', name: 'Recent One', is_favorite: false, personality_id: 'persona-1' },
        ]);
        threadsStub.recentOpenedIds.set(['thread-2']);
        const fixture = create();
        expect(fixture.nativeElement.textContent).toContain('Recent Threads');
        expect(fixture.nativeElement.textContent).toContain('Recent One');
    });

    it('shows personality search input and opens dropdown on focus', () => {
        const fixture = create();
        const input = fixture.nativeElement.querySelector('.app-sidebar__personality-search') as HTMLInputElement;
        expect(input?.getAttribute('placeholder')).toBe('Add more…');
        input.dispatchEvent(new Event('focus'));
        fixture.detectChanges();
        const dropdown = fixture.nativeElement.querySelector('.app-sidebar__personality-dropdown');
        expect(dropdown).toBeTruthy();
    });

    it('toggles personality selection from dropdown options', () => {
        const fixture = create();
        const input = fixture.nativeElement.querySelector('.app-sidebar__personality-search') as HTMLInputElement;
        input.dispatchEvent(new Event('focus'));
        fixture.detectChanges();
        const option = fixture.nativeElement.querySelector('.app-sidebar__personality-option') as HTMLButtonElement;
        option.dispatchEvent(new MouseEvent('mousedown'));
        fixture.detectChanges();
        const carousel = fixture.nativeElement.querySelector('.app-sidebar__personality-carousel');
        expect(carousel).toBeTruthy();
    });

    it('the footer search launcher opens the palette', () => {
        const fixture = create();
        const launcher = fixture.nativeElement.querySelector('button.app-sidebar__palette-launcher') as HTMLButtonElement;
        expect(launcher.getAttribute('aria-label')).toBe('Open command palette');
        launcher.click();
        expect(paletteSpy.open).toHaveBeenCalled();
    });

    it('emits a new-thread request from the chat control button', () => {
        const fixture = create();
        const emitted = vi.fn().mockName('newThread');
        fixture.componentInstance.newThread.subscribe(emitted);

        const button = fixture.nativeElement.querySelector('button.app-sidebar__new-thread') as HTMLButtonElement;
        button.click();

        expect(emitted).toHaveBeenCalled();
    });

    it('shows personality actions panel on personality route', async () => {
        routerUrlSpy.mockReturnValue('/personality');
        const fixture = create();
        const emitGenerate = vi.fn().mockName('generate');
        fixture.componentInstance.generatePersonality.subscribe(emitGenerate);
        vi.spyOn(router, 'navigate').mockResolvedValue(true);

        const section = fixture.nativeElement.querySelector('.app-sidebar__personality-actions');
        expect(section?.textContent).toContain('ADD PERSONALITY');

        const buttons = fixture.nativeElement.querySelectorAll('.app-sidebar__personality-actions-buttons button') as NodeListOf<HTMLButtonElement>;
        buttons[0].click();
        buttons[1].click();
        await fixture.whenStable();

        expect(emitGenerate).toHaveBeenCalled();
        expect(router.navigate).toHaveBeenCalledWith(['/personality'], { queryParams: { create: '1' } });
    });

    it('shows gallery controls on gallery route and opens import modal request', () => {
        routerUrlSpy.mockReturnValue('/gallery');
        const fixture = create();

        const gallerySection = fixture.nativeElement.querySelector('.app-sidebar__gallery-controls');
        expect(gallerySection).toBeTruthy();

        const importButton = fixture.nativeElement.querySelector('section.app-sidebar__gallery-controls button.app-sidebar__new-thread') as HTMLButtonElement;
        importButton.click();

        expect(galleryViewStub.requestImportModalOpen).toHaveBeenCalled();
    });

    it('shows mode controls on mode route and requests create modal', () => {
        routerUrlSpy.mockReturnValue('/mode');
        const fixture = create();
        const section = fixture.nativeElement.querySelector('.app-sidebar__gallery-controls');
        expect(section).toBeTruthy();
        const createButton = section.querySelector('button.app-sidebar__new-thread') as HTMLButtonElement;
        expect(createButton.textContent).toContain('Create Mode');
        createButton.click();
        expect(modeViewStub.requestCreateModalOpen).toHaveBeenCalled();
    });

    it('disables global filter button in expression manager mode', () => {
        routerUrlSpy.mockReturnValue('/gallery');
        galleryViewStub.mode.set('expressions');
        const fixture = create();
        const globalButton = fixture.nativeElement.querySelector('section.app-sidebar__gallery-controls .app-sidebar__pill:nth-child(2)') as HTMLButtonElement;
        expect(globalButton.disabled).toBe(true);
    });

    it('shows memory controls on memories route and handles filter + add actions', () => {
        routerUrlSpy.mockReturnValue('/memories');
        vi.spyOn(router, 'navigate').mockResolvedValue(true);
        const fixture = create();
        const memorySection = fixture.nativeElement.querySelector('.app-sidebar__gallery-controls');
        expect(memorySection).toBeTruthy();
        const pills = memorySection.querySelectorAll('.app-sidebar__pill') as NodeListOf<HTMLButtonElement>;
        pills[0].click();
        pills[1].click();
        const addButton = memorySection.querySelector('button.app-sidebar__new-thread') as HTMLButtonElement;
        addButton.click();
        expect(memoryViewStub.selectAllAssociations).toHaveBeenCalled();
        expect(memoryViewStub.selectGlobalAssociations).toHaveBeenCalled();
        expect(router.navigate).toHaveBeenCalledWith(['/memories'], { queryParams: { create: '1' }, queryParamsHandling: 'merge' });
    });

    it('shows skills controls on skills route and handles filter + create actions', () => {
        routerUrlSpy.mockReturnValue('/skills');
        vi.spyOn(router, 'navigate').mockResolvedValue(true);
        const fixture = create();
        const skillsSection = fixture.nativeElement.querySelector('.app-sidebar__gallery-controls');
        expect(skillsSection).toBeTruthy();

        const pills = skillsSection.querySelectorAll('.app-sidebar__pill') as NodeListOf<HTMLButtonElement>;
        pills[0].click();
        pills[1].click();

        const createButton = skillsSection.querySelector('button.app-sidebar__new-thread') as HTMLButtonElement;
        createButton.click();

        expect(ritualViewStub.setFilters).toHaveBeenCalledWith({ globalOnly: false, personalityIds: [], personalityId: '' });
        expect(ritualViewStub.setFilters).toHaveBeenCalledWith({ globalOnly: true, personalityIds: [], personalityId: '' });
        expect(router.navigate).toHaveBeenCalledWith(['/skills'], { queryParams: { create: '1' }, queryParamsHandling: 'merge' });
    });
});
