import type { Mock, MockedObject } from "vitest";
import { Component, provideZonelessChangeDetection, signal, ChangeDetectionStrategy } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Router, provideRouter } from '@angular/router';
import { BehaviorSubject, of, throwError } from 'rxjs';

import { AuthService } from '../core/services/auth.service';
import { ChatService } from '../core/services/chat.service';
import { CommandPaletteService } from '../core/services/command-palette.service';
import { KeyboardShortcutService } from '../core/services/keyboard-shortcut.service';
import { NavService } from '../core/services/nav.service';
import { ThemeService } from '../core/services/theme.service';
import { AppLayoutComponent } from './app-layout.component';
import { RightPanelService } from '../core/services/right-panel.service';
import { GeneratePersonalityModalService } from '../core/services/generate-personality-modal.service';
import type { PersonalityGenFlow } from '../core/models/personality-gen-flow.model';
import { ConfirmationService } from '../core/services/confirmation.service';
import { PersonalityService } from '../core/services/personality.service';

@Component({ selector: 'noop', standalone: true, changeDetection: ChangeDetectionStrategy.Eager,
    template: '' })
class NoopComponent {
}

describe('AppLayoutComponent', () => {
    /** Holds the /slow route's guard open until the test emits, to observe a pending navigation. */
    let slowGate: Promise<boolean>;
    let releaseSlowGate: (allow: boolean) => void;
    let unregister: Mock;
    let unregisterCommand: Mock;
    let unregisterShortcut: Mock;
    let paletteSpy: Pick<MockedObject<CommandPaletteService>, 'open' | 'close' | 'toggle' | 'setQuery' | 'register' | 'registerCommand' | 'runCommand' | 'visible' | 'query' | 'sections' | 'loading' | 'commandResults'>;

    beforeEach(async () => {
        slowGate = new Promise<boolean>(resolve => (releaseSlowGate = resolve));
        unregister = vi.fn().mockName('unregister');
        unregisterCommand = vi.fn().mockName('unregisterCommand');
        unregisterShortcut = vi.fn().mockName('unregisterShortcut');

        const authSpy = {
            currentUser: (() => null) as any
        };

        const themeSpy = {
            setTheme: vi.fn().mockName("ThemeService.setTheme"),
            theme: (() => 'light') as any
        };

        let navCollapsed = false;
        const navSpy = {
            setMode: vi.fn().mockName("NavService.setMode"),
            toggleCollapsed: vi.fn().mockName("NavService.toggleCollapsed"),
            setCollapsed: vi.fn().mockName("NavService.setCollapsed").mockImplementation((value: boolean) => {
                navCollapsed = value;
            }),
            mode: (() => 'app') as any,
            collapsed: (() => navCollapsed) as any
        };

        paletteSpy = {
            open: vi.fn().mockName("CommandPaletteService.open"),
            close: vi.fn().mockName("CommandPaletteService.close"),
            toggle: vi.fn().mockName("CommandPaletteService.toggle"),
            setQuery: vi.fn().mockName("CommandPaletteService.setQuery"),
            register: vi.fn().mockName("CommandPaletteService.register"),
            registerCommand: vi.fn().mockName("CommandPaletteService.registerCommand"),
            runCommand: vi.fn().mockName("CommandPaletteService.runCommand"),
            visible: signal(false),
            query: signal(''),
            sections: signal<readonly any[]>([]),
            loading: signal(false),
            commandResults: signal<any[]>([])
        } as unknown as Pick<MockedObject<CommandPaletteService>, 'open' | 'close' | 'toggle' | 'setQuery' | 'register' | 'registerCommand' | 'runCommand' | 'visible' | 'query' | 'sections' | 'loading' | 'commandResults'>;
        paletteSpy.register.mockReturnValue(unregister as any);
        paletteSpy.registerCommand.mockReturnValue(unregisterCommand as any);

        const shortcutSpy = {
            register: vi.fn().mockName("KeyboardShortcutService.register")
        };
        shortcutSpy.register.mockReturnValue(unregisterShortcut as any);

        await TestBed.configureTestingModule({
            imports: [AppLayoutComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                provideRouter([
                    { path: 'chat', component: NoopComponent },
                    { path: 'chat/:id', component: NoopComponent },
                    { path: 'memories', component: NoopComponent },
                    { path: 'slow', component: NoopComponent, canActivate: [() => slowGate] },
                    { path: '**', component: NoopComponent },
                ]),
                { provide: AuthService, useValue: authSpy },
                { provide: ThemeService, useValue: themeSpy },
                { provide: NavService, useValue: navSpy },
                { provide: CommandPaletteService, useValue: paletteSpy },
                { provide: KeyboardShortcutService, useValue: shortcutSpy },
            ],
        }).compileComponents();
    });

    it('renders sidebar, router-outlet, and palette', () => {
        const fixture = TestBed.createComponent(AppLayoutComponent);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('app-sidebar')).not.toBeNull();
        expect(host.querySelector('router-outlet')).not.toBeNull();
        expect(host.querySelector('app-right-panel-host')).toBeNull();
        expect(host.querySelector('app-command-palette')).not.toBeNull();
    });

    it('shows right panel only on /chat routes', async () => {
        vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, true));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const router = TestBed.inject(Router);
        fixture.detectChanges();
        let host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('app-right-panel-host')).toBeNull();

        await router.navigateByUrl('/chat');
        fixture.detectChanges();
        host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('app-right-panel-host')).toBeNull();

        await router.navigateByUrl('/chat/thread-manager');
        fixture.detectChanges();
        host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('app-right-panel-host')).toBeNull();

        await router.navigateByUrl('/chat/ba83002b-fa33-4bff-a13a-6399376fc798');
        TestBed.inject(RightPanelService).setVisible(true);
        fixture.detectChanges();
        host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('app-right-panel-host')).not.toBeNull();

        TestBed.inject(RightPanelService).setVisible(false);

        await router.navigateByUrl('/memories');
        fixture.detectChanges();
        host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('app-right-panel-host')).toBeNull();
    });

    it('hides right panel on small viewport even with active thread', async () => {
        const matchMediaSpy = vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, false));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const router = TestBed.inject(Router);
        await router.navigateByUrl('/chat/ba83002b-fa33-4bff-a13a-6399376fc798');
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('app-right-panel-host')).toBeNull();
        expect(matchMediaSpy).toHaveBeenCalled();
    });

    it('registers the global ⌘K shortcut and a search handler on init', () => {
        const fixture = TestBed.createComponent(AppLayoutComponent);
        fixture.detectChanges();

        const shortcut = TestBed.inject(KeyboardShortcutService) as MockedObject<KeyboardShortcutService>;
        expect(shortcut.register).toHaveBeenCalledTimes(1);

        const palette = TestBed.inject(CommandPaletteService) as MockedObject<CommandPaletteService>;
        expect(palette.register).toHaveBeenCalledTimes(1);
        expect(vi.mocked(palette.registerCommand).mock.calls.length).toBeGreaterThanOrEqual(4);
    });

    it('opens the personality picker from the New chat command', () => {
        const fixture = TestBed.createComponent(AppLayoutComponent);
        fixture.detectChanges();
        const newChatCommand = vi.mocked(paletteSpy.registerCommand).mock.calls.map(args => args[0])
            .find(command => command.id === 'new-chat');

        newChatCommand?.run();

        expect(fixture.componentInstance.personaPickerOpen()).toBe(true);
    });

    it('collapses the mobile sidebar after a successful New chat creation', async () => {
        vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, false));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const router = TestBed.inject(Router);
        const nav = TestBed.inject(NavService) as MockedObject<NavService>;
        const chatService = TestBed.inject(ChatService);
        fixture.detectChanges();
        vi.mocked(nav.setCollapsed).mockClear();
        vi.spyOn(chatService, 'createChat').mockReturnValue(of({
            id: 'ba83002b-fa33-4bff-a13a-6399376fc798',
            name: 'New thread',
            personality_id: 'personality-1',
        } as any));

        await fixture.componentInstance.onPersonaPicked({ id: 'personality-1' } as any);

        expect(router.url).toBe('/chat/ba83002b-fa33-4bff-a13a-6399376fc798');
        expect(nav.setCollapsed).toHaveBeenCalledWith(true);
    });

    it('closes the mobile drawer when a nav pick changes the page (#348)', async () => {
        vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, false));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const router = TestBed.inject(Router);
        const nav = TestBed.inject(NavService) as MockedObject<NavService>;
        fixture.detectChanges();
        await router.navigateByUrl('/chat');
        nav.setCollapsed(false);
        vi.mocked(nav.setCollapsed).mockClear();

        await router.navigateByUrl('/gallery');
        expect(nav.setCollapsed).toHaveBeenCalledWith(true);

        nav.setCollapsed(false);
        vi.mocked(nav.setCollapsed).mockClear();
        await router.navigateByUrl('/gallery?filter=mine');
        expect(nav.setCollapsed).not.toHaveBeenCalled();
    });

    it('keeps the mobile drawer open when the Config toggle switches modes', async () => {
        vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, false));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const router = TestBed.inject(Router);
        const nav = TestBed.inject(NavService) as MockedObject<NavService> & { isSwitchingMode: boolean };
        fixture.detectChanges();
        await router.navigateByUrl('/chat');
        nav.setCollapsed(false);
        vi.mocked(nav.setCollapsed).mockClear();

        // The toggle only swaps the drawer's menu, so its navigation must not close the drawer.
        nav.isSwitchingMode = true;
        await router.navigateByUrl('/memories');
        nav.isSwitchingMode = false;
        expect(nav.setCollapsed).not.toHaveBeenCalled();

        // A page picked from the config menu afterwards still closes it.
        await router.navigateByUrl('/gallery');
        expect(nav.setCollapsed).toHaveBeenCalledWith(true);
    });

    it('leaves the desktop sidebar alone on navigation', async () => {
        vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, true));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const router = TestBed.inject(Router);
        const nav = TestBed.inject(NavService) as MockedObject<NavService>;
        fixture.detectChanges();
        await router.navigateByUrl('/chat');

        await router.navigateByUrl('/gallery');
        expect(nav.setCollapsed).not.toHaveBeenCalled();
    });

    it('shows the progress bar while a navigation is pending', async () => {
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const router = TestBed.inject(Router);
        fixture.detectChanges();
        await router.navigateByUrl('/chat');

        const pending = router.navigateByUrl('/slow');
        await vi.waitFor(() => expect(fixture.componentInstance.navigating()).toBe(true));
        fixture.detectChanges();
        expect((fixture.nativeElement as HTMLElement).querySelector('.app-layout__nav-progress')).not.toBeNull();

        releaseSlowGate(true);
        await pending;
        fixture.detectChanges();
        expect(fixture.componentInstance.navigating()).toBe(false);
        expect((fixture.nativeElement as HTMLElement).querySelector('.app-layout__nav-progress')).toBeNull();
    });

    it('does not collapse the desktop sidebar after a successful New chat creation', async () => {
        vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, true));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const nav = TestBed.inject(NavService) as MockedObject<NavService>;
        const chatService = TestBed.inject(ChatService);
        fixture.detectChanges();
        vi.mocked(nav.setCollapsed).mockClear();
        vi.spyOn(chatService, 'createChat').mockReturnValue(of({
            id: 'ba83002b-fa33-4bff-a13a-6399376fc798',
            name: 'New thread',
            personality_id: 'personality-1',
        } as any));

        await fixture.componentInstance.onPersonaPicked({ id: 'personality-1' } as any);

        expect(nav.setCollapsed).not.toHaveBeenCalled();
    });

    it('reports a picker selection that cannot start a chat and refreshes personalities (#318)', async () => {
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const router = TestBed.inject(Router);
        fixture.detectChanges();
        const startUrl = router.url;
        vi.spyOn(TestBed.inject(ChatService), 'createChat').mockReturnValue(throwError(() => new Error('Personality not found')));
        const invalidate = vi.spyOn(TestBed.inject(PersonalityService), 'invalidate');
        const alert = vi.spyOn(TestBed.inject(ConfirmationService), 'alert').mockResolvedValue(undefined);

        await fixture.componentInstance.onPersonaPicked({ id: 'gone', name: 'Imported Card' } as any);

        expect(invalidate).toHaveBeenCalled();
        expect(alert).toHaveBeenCalledWith(expect.objectContaining({
            title: 'Could not start a chat with Imported Card',
            message: expect.stringContaining('Personality not found'),
            type: 'danger',
        }));
        expect(router.url).toBe(startUrl);
    });

    describe('Generate Personality modal — phone footer', () => {
        const flowOf = (overrides: Partial<PersonalityGenFlow>): PersonalityGenFlow => ({
            id: 'flow-1',
            status: 'in_progress',
            current_step: 0,
            answers: {},
            generated_prompt: '',
            generated_about_me: '',
            generated_names: [],
            image_style: 'none',
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
            ...overrides,
        });

        async function openModal(flow: PersonalityGenFlow) {
            const fixture = TestBed.createComponent(AppLayoutComponent);
            fixture.detectChanges();
            TestBed.inject(GeneratePersonalityModalService).show();
            fixture.detectChanges();
            const http = TestBed.inject(HttpTestingController);
            http.expectOne(req => req.method === 'GET' && req.url.endsWith('/personality/generate')).flush(flow);
            await fixture.whenStable();
            fixture.detectChanges();
            return fixture.nativeElement as HTMLElement;
        }

        const labelOf = (b: Element) => (b.getAttribute('aria-label') ?? b.textContent ?? '').replace(/\s+/g, ' ').trim();

        it('shows Generate now plus a Start Over / Previous / Next row on wizard pages', async () => {
            const host = await openModal(flowOf({ current_step: 0 }));
            const footer = host.querySelector('.ui-modal__footer') as HTMLElement;
            const nav = footer.querySelector('[role="group"][aria-label="Wizard navigation"]') as HTMLElement;

            expect(nav).not.toBeNull();
            expect(footer.textContent).toContain('Generate now');
            // Nothing to discard yet: Start Over is hidden, Previous is disabled, Next is enabled.
            expect(nav.querySelector('button[aria-label="Start over"]')).toBeNull();
            expect((nav.querySelector('button[aria-label="Previous step"]') as HTMLButtonElement).disabled).toBe(true);
            expect((nav.querySelector('button[aria-label="Next step"]') as HTMLButtonElement).disabled).toBe(false);
        });

        it('shows Start Over once there is progress and disables Next on the last page', async () => {
            const host = await openModal(flowOf({ current_step: 3, answers: { who: 'A fox' } }));
            const nav = host.querySelector('[role="group"][aria-label="Wizard navigation"]') as HTMLElement;

            expect(nav.querySelector('button[aria-label="Start over"]')).not.toBeNull();
            expect((nav.querySelector('button[aria-label="Previous step"]') as HTMLButtonElement).disabled).toBe(false);
            expect((nav.querySelector('button[aria-label="Next step"]') as HTMLButtonElement).disabled).toBe(true);
        });

        it('swaps to Accept & create / Regenerate on the review screen and drops the wizard row', async () => {
            const host = await openModal(flowOf({ status: 'generated', current_step: 3, answers: { who: 'A fox' }, generated_names: ['Fennick'] }));
            const footer = host.querySelector('.ui-modal__footer') as HTMLElement;
            const review = footer.querySelector('[role="group"][aria-label="Review actions"]') as HTMLElement;

            expect(footer.querySelector('[aria-label="Wizard navigation"]')).toBeNull();
            expect(review).not.toBeNull();
            expect(labelOf(footer.querySelector('button.min-h-11.w-full')!)).toBe('Accept & create');
            expect([...review.querySelectorAll('button')].map(labelOf)).toEqual([
                'Start over',
                'Previous step: edit your answers',
                'Regenerate',
            ]);

            // In the modal the in-body review actions hide on phones (the footer has them),
            // without losing their own layout classes.
            const editAnswers = [...host.querySelectorAll('button')].find(b => labelOf(b) === 'Edit answers')!;
            const inBodyActions = editAnswers.closest('div.border-t') as HTMLElement;
            expect(inBodyActions.classList).toContain('max-sm:hidden');
            expect(inBodyActions.classList).toContain('justify-between');
        });
    });

    it('disposes shortcut + handler + commands on destroy', () => {
        const fixture = TestBed.createComponent(AppLayoutComponent);
        fixture.detectChanges();
        fixture.destroy();
        expect(unregisterShortcut).toHaveBeenCalled();
        expect(unregister).toHaveBeenCalled();
        expect(unregisterCommand).toHaveBeenCalled();
    });

    it('wires mobile drawer a11y: sidebar id, menu aria-controls, main tabindex, inert when closed', () => {
        vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, false));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        const sidebar = host.querySelector('#app-sidebar') as HTMLElement;
        expect(sidebar).toBeTruthy();
        expect(sidebar.hasAttribute('inert')).toBe(true);

        const main = host.querySelector('#main-content') as HTMLElement;
        expect(main.getAttribute('tabindex')).toBe('-1');

        const menu = host.querySelector('.app-layout__mobile-menu') as HTMLButtonElement;
        expect(menu.getAttribute('aria-controls')).toBe('app-sidebar');
        expect(menu.getAttribute('aria-expanded')).toBe('false');
    });

    it('closes the mobile drawer on Escape', () => {
        vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => fakeMediaQueryList(query, false));
        const fixture = TestBed.createComponent(AppLayoutComponent);
        const nav = TestBed.inject(NavService) as MockedObject<NavService>;
        fixture.detectChanges();
        nav.setCollapsed(false);
        fixture.detectChanges();
        vi.mocked(nav.setCollapsed).mockClear();

        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
        expect(nav.setCollapsed).toHaveBeenCalledWith(true);
    });
});

function fakeMediaQueryList(media: string, matches: boolean): MediaQueryList {
    return {
        media,
        matches,
        onchange: null,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        dispatchEvent: () => false,
        addListener: () => undefined,
        removeListener: () => undefined,
    };
}
