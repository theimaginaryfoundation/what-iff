import type { Mock } from "vitest";
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Event as RouterEvent, NavigationEnd, Router } from '@angular/router';
import { Subject } from 'rxjs';

import { NavService, SIDEBAR_COLLAPSED_KEY } from './nav.service';

describe('NavService', () => {
    let routerEvents: Subject<RouterEvent>;
    let routerStub: {
        url: string;
        events: any;
        navigate: Mock;
    };

    beforeEach(() => {
        localStorage.removeItem(SIDEBAR_COLLAPSED_KEY);

        routerEvents = new Subject<RouterEvent>();
        routerStub = {
            url: '/chat',
            events: routerEvents.asObservable(),
            navigate: vi.fn().mockName('navigate'),
        };

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                { provide: Router, useValue: routerStub },
                NavService,
            ],
        });
    });

    afterEach(() => {
        localStorage.removeItem(SIDEBAR_COLLAPSED_KEY);
    });

    it('starts in app mode for /chat', () => {
        const service = TestBed.inject(NavService);
        expect(service.mode()).toBe('app');
    });

    it('flips to config when the route changes to /memories', async () => {
        const service = TestBed.inject(NavService);
        expect(service.mode()).toBe('app');

        routerStub.url = '/memories';
        routerEvents.next(new NavigationEnd(1, '/memories', '/memories'));
        await new Promise(resolve => setTimeout(resolve, 0));
        expect(service.mode()).toBe('config');

        routerStub.url = '/agent-jobs';
        routerEvents.next(new NavigationEnd(2, '/agent-jobs', '/agent-jobs'));
        await new Promise(resolve => setTimeout(resolve, 0));
        expect(service.mode()).toBe('config');

        routerStub.url = '/integrations';
        routerEvents.next(new NavigationEnd(3, '/integrations', '/integrations'));
        await new Promise(resolve => setTimeout(resolve, 0));
        expect(service.mode()).toBe('config');

        routerStub.url = '/mode';
        routerEvents.next(new NavigationEnd(4, '/mode', '/mode'));
        await new Promise(resolve => setTimeout(resolve, 0));
        expect(service.mode()).toBe('config');

        routerStub.url = '/chat';
        routerEvents.next(new NavigationEnd(5, '/chat', '/chat'));
        await new Promise(resolve => setTimeout(resolve, 0));
        expect(service.mode()).toBe('app');
    });

    it('setMode("config") navigates to /memories', () => {
        const service = TestBed.inject(NavService);
        service.setMode('config');
        expect(routerStub.navigate).toHaveBeenCalledWith(['/memories']);
    });

    it('setMode("app") navigates to /chat', () => {
        const service = TestBed.inject(NavService);
        service.setMode('app');
        expect(routerStub.navigate).toHaveBeenCalledWith(['/chat']);
    });

    it('toggleCollapsed flips the signal and persists to localStorage', () => {
        const service = TestBed.inject(NavService);
        expect(service.collapsed()).toBe(false);

        service.toggleCollapsed();
        expect(service.collapsed()).toBe(true);
        expect(localStorage.getItem(SIDEBAR_COLLAPSED_KEY)).toBe('true');

        service.toggleCollapsed();
        expect(service.collapsed()).toBe(false);
        expect(localStorage.getItem(SIDEBAR_COLLAPSED_KEY)).toBe('false');
    });

    it('hydrates collapsed signal from localStorage on instantiation', () => {
        localStorage.setItem(SIDEBAR_COLLAPSED_KEY, 'true');
        const service = TestBed.inject(NavService);
        expect(service.collapsed()).toBe(true);
    });

    it('setCollapsed forces a specific value and persists it', () => {
        const service = TestBed.inject(NavService);
        service.setCollapsed(true);
        expect(service.collapsed()).toBe(true);
        expect(localStorage.getItem(SIDEBAR_COLLAPSED_KEY)).toBe('true');
    });
});
