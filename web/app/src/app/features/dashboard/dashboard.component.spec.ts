import type { MockedObject } from "vitest";
import { Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, Routes, provideRouter } from '@angular/router';
import { NEVER, of, throwError } from 'rxjs';

import { UsageStats } from '../../core/models/user.model';
import { AuthService } from '../../core/services/auth.service';
import { DashboardComponent } from './dashboard.component';

/**
 * Stand-ins for the three Quick Actions targets. The tiles only need somewhere
 * real to land — what those pages render is their own specs' business.
 */
@Component({ standalone: true, template: '' })
class RouteStubComponent {}

const QUICK_ACTION_ROUTES: Routes = [
    { path: 'chat', component: RouteStubComponent },
    { path: 'profile', component: RouteStubComponent },
    { path: 'memories', component: RouteStubComponent },
];

/** Every Quick Actions tile, as rendered heading text and the route behind it. */
const QUICK_ACTION_TILES = [
    { name: 'Start New Chat', href: '/chat' },
    { name: 'Profile Settings', href: '/profile' },
    { name: 'Manage Memories', href: '/memories' },
] as const;

describe('DashboardComponent', () => {
    let fixture: ComponentFixture<DashboardComponent>;
    let authService: Pick<MockedObject<AuthService>, 'getUserUsageStats' | 'currentUser'>;

    const stats: UsageStats = { chats: 12, messages: 3456, input_tokens: 7890, output_tokens: 1234 };

    beforeEach(async () => {
        authService = {
            getUserUsageStats: vi.fn().mockName("AuthService.getUserUsageStats"),
            currentUser: signal({
                id: 'user-1',
                username: 'ada',
                email: 'ada@example.com',
                first_name: 'Ada',
                last_name: 'Lovelace',
                created_at: '2026-08-01T12:00:00Z',
                updated_at: '2026-08-01T12:00:00Z',
            })
        } as unknown as Pick<MockedObject<AuthService>, 'getUserUsageStats' | 'currentUser'>;
        authService.getUserUsageStats.mockReturnValue(of(stats));

        await TestBed.configureTestingModule({
            imports: [DashboardComponent],
            providers: [
                provideZonelessChangeDetection(),
                // Real routes, not `provideRouter([])`: the Quick Actions tiles are
                // routerLinks, and a link whose target does not resolve would still
                // render an href while silently failing to navigate.
                provideRouter(QUICK_ACTION_ROUTES),
                { provide: AuthService, useValue: authService },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(DashboardComponent);
    });

    it('creates', () => {
        expect(fixture.componentInstance).toBeTruthy();
    });

    it('renders the loading branch while usage stats are pending', () => {
        authService.getUserUsageStats.mockReturnValue(NEVER);
        fixture.detectChanges();

        expect(fixture.nativeElement.textContent).toContain('Loading statistics...');
        expect(fixture.nativeElement.textContent).not.toContain('12');
    });

    it('renders the error branch when usage stats fail', () => {
        vi.spyOn(console, 'error').mockReturnValue(undefined);
        authService.getUserUsageStats.mockReturnValue(throwError(() => new Error('offline')));
        fixture.detectChanges();

        expect(fixture.nativeElement.textContent).toContain('Failed to load usage statistics');
        expect(fixture.nativeElement.textContent).toContain('Try again');
        expect(fixture.nativeElement.textContent).not.toContain('Loading statistics...');
    });

    it('renders every period option and all populated stats tiles', () => {
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;
        const options = host.querySelectorAll('select option');

        expect(options.length).toBe(3);
        expect(Array.from(options).map(option => option.textContent?.trim())).toEqual([
            'Last 24 Hours',
            'Last 7 Days',
            'Last 30 Days',
        ]);
        expect(host.textContent).toContain('Welcome back, Ada Lovelace!');
        expect(host.textContent).toContain('Chats');
        expect(host.textContent).toContain('12');
        expect(host.textContent).toContain('3,456');
        expect(host.textContent).toContain('7,890');
        expect(host.textContent).toContain('1,234');
        expect(host.textContent).not.toContain('Loading statistics...');
    });
    describe('Quick Actions tiles', () => {
        /**
         * The tile is the whole card: the `<a>` carries a `.absolute.inset-0`
         * overlay span so the clickable area is the card rather than the heading
         * text. Finding it by heading text and walking up to the anchor asserts
         * that pairing holds, rather than assuming a tile order.
         */
        const tileFor = (name: string): HTMLAnchorElement => {
            const host = fixture.nativeElement as HTMLElement;
            const heading = Array.from(host.querySelectorAll('h3')).find(
                element => element.textContent?.trim() === name,
            );
            const anchor = heading?.closest('a');
            if (!anchor) {
                throw new Error(`no Quick Actions tile anchor for "${name}"`);
            }
            return anchor as HTMLAnchorElement;
        };

        /** Returns whether the router handled the click, i.e. cancelled the default. */
        const clickTile = async (
            anchor: HTMLAnchorElement,
            init: MouseEventInit = {},
        ): Promise<boolean> => {
            let handled = false;
            // Registered after `routerLink`'s own host listener, so it reads the
            // verdict, then cancels regardless: an uncancelled click on an anchor
            // makes jsdom try to follow the href and log a "navigation to another
            // Document" warning on every run.
            anchor.addEventListener(
                'click',
                event => {
                    handled = event.defaultPrevented;
                    event.preventDefault();
                },
                { once: true },
            );

            anchor.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, ...init }));
            await fixture.whenStable();
            return handled;
        };

        beforeEach(() => {
            fixture.detectChanges();
        });

        it.each(QUICK_ACTION_TILES)(
            'renders the $name tile as a link to $href',
            ({ name, href }) => {
                // `routerLink` writes the resolved href itself. Asserting the attribute
                // rather than the directive is what pins the behaviour that depends on
                // it: middle-click, open-in-new-tab, copy-link, and the status bar.
                expect(tileFor(name).getAttribute('href')).toBe(href);
            },
        );

        it.each(QUICK_ACTION_TILES)(
            'routes to $href in-app when the $name tile is clicked',
            async ({ name, href }) => {
                const router = TestBed.inject(Router);
                await router.navigateByUrl('/');

                const handled = await clickTile(tileFor(name));

                // Both halves matter. The cancelled default is what makes this a
                // router navigation instead of a full document load — the bug the
                // bare `href` these tiles used to carry actually caused — and the
                // resolved url is what proves the navigation went somewhere.
                expect(handled, 'router cancelled the browser navigation').toBe(true);
                expect(router.url).toBe(href);
            },
        );

        it.each(QUICK_ACTION_TILES)(
            'leaves a modified click on the $name tile to the browser',
            async ({ name }) => {
                const router = TestBed.inject(Router);
                await router.navigateByUrl('/');

                // Ctrl-click, cmd-click and middle-click are how people open a tile in
                // a new tab. `routerLink` deliberately does not intercept those, and
                // swallowing them would be a regression the href assertion cannot see.
                const handled = await clickTile(tileFor(name), { ctrlKey: true });

                expect(handled, 'router left the modified click alone').toBe(false);
                expect(router.url).toBe('/');
            },
        );

        it.each(QUICK_ACTION_TILES)(
            'gives the $name tile a card-sized click target',
            ({ name }) => {
                // The stretched-link overlay is what makes the whole card clickable.
                // It lives inside the anchor and is hidden from assistive tech, so the
                // accessible name stays the heading text.
                const overlay = tileFor(name).querySelector('span.absolute.inset-0');

                expect(overlay, 'stretched-link overlay').not.toBeNull();
                expect(overlay?.getAttribute('aria-hidden')).toBe('true');
            },
        );
    });
});
