import type { Mock, MockedObject } from "vitest";
import { Component, provideZonelessChangeDetection, signal, ChangeDetectionStrategy } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { Router, provideRouter } from '@angular/router';
import { BehaviorSubject, of } from 'rxjs';

import { AuthService } from '../core/services/auth.service';
import { ChatService } from '../core/services/chat.service';
import { CommandPaletteService } from '../core/services/command-palette.service';
import { KeyboardShortcutService } from '../core/services/keyboard-shortcut.service';
import { NavService } from '../core/services/nav.service';
import { ThemeService } from '../core/services/theme.service';
import { AppLayoutComponent } from './app-layout.component';
import { RightPanelService } from '../core/services/right-panel.service';

@Component({ selector: 'noop', standalone: true, changeDetection: ChangeDetectionStrategy.Eager,
    template: '' })
class NoopComponent {
}

describe('AppLayoutComponent', () => {
    let unregister: Mock;
    let unregisterCommand: Mock;
    let unregisterShortcut: Mock;
    let paletteSpy: Pick<MockedObject<CommandPaletteService>, 'open' | 'close' | 'toggle' | 'setQuery' | 'register' | 'registerCommand' | 'runCommand' | 'visible' | 'query' | 'sections' | 'loading' | 'commandResults'>;

    beforeEach(async () => {
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

        const navSpy = {
            setMode: vi.fn().mockName("NavService.setMode"),
            toggleCollapsed: vi.fn().mockName("NavService.toggleCollapsed"),
            setCollapsed: vi.fn().mockName("NavService.setCollapsed"),
            mode: (() => 'app') as any,
            collapsed: (() => false) as any
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
            name: 'New Chat',
            personality_id: 'personality-1',
        } as any));

        await fixture.componentInstance.onPersonaPicked({ id: 'personality-1' } as any);

        expect(router.url).toBe('/chat/ba83002b-fa33-4bff-a13a-6399376fc798');
        expect(nav.setCollapsed).toHaveBeenCalledWith(true);
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
            name: 'New Chat',
            personality_id: 'personality-1',
        } as any));

        await fixture.componentInstance.onPersonaPicked({ id: 'personality-1' } as any);

        expect(nav.setCollapsed).not.toHaveBeenCalled();
    });

    it('disposes shortcut + handler + commands on destroy', () => {
        const fixture = TestBed.createComponent(AppLayoutComponent);
        fixture.detectChanges();
        fixture.destroy();
        expect(unregisterShortcut).toHaveBeenCalled();
        expect(unregister).toHaveBeenCalled();
        expect(unregisterCommand).toHaveBeenCalled();
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
