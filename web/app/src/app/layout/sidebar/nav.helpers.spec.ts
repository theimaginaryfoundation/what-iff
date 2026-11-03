import { appNavItems, configNavItems, extraConfigItems, isConfigPath, NavItem, navTooltip } from './nav.helpers';

describe('nav.helpers', () => {
    describe('appNavItems', () => {
        it('returns chat / personalities / gallery in canonical order', () => {
            const ids = appNavItems().map(i => i.id);
            expect(ids).toEqual(['chat', 'personalities', 'gallery']);
        });

        it('uses app routes that match the registered Angular paths', () => {
            const routes = appNavItems().map(i => i.route);
            expect(routes).toEqual(['/chat', '/personality', '/gallery']);
        });

        it('every item carries a label and an icon component', () => {
            for (const item of appNavItems()) {
                expect(item.label.length).toBeGreaterThan(0);
                expect(item.icon).toBeDefined();
            }
        });
    });

    describe('configNavItems', () => {
        // The build may contribute entries through extensions/nav-items.ts, so
        // the base set is asserted as a prefix and the extras as the suffix.
        const BASE_IDS = ['memories', 'modes', 'skills', 'tools', 'jobs'];
        const extraIds = () => extraConfigItems.map(i => i.id);

        it('lists base items in canonical order including jobs', () => {
            const ids = configNavItems().map(i => i.id);
            expect(ids.slice(0, BASE_IDS.length)).toEqual(BASE_IDS);
            expect(ids.slice(BASE_IDS.length)).toEqual(extraIds());
        });

        it('appends extraConfigItems after the base set', () => {
            const fake: NavItem = {
                id: 'features',
                label: 'Features',
                hint: 'Feature toggles',
                route: '/features',
                icon: appNavItems()[0].icon,
            };
            (extraConfigItems as NavItem[]).push(fake);
            try {
                const ids = configNavItems().map(i => i.id);
                expect(ids).toEqual([...BASE_IDS, ...extraIds()]);
                expect(ids[ids.length - 1]).toBe('features');
            }
            finally {
                (extraConfigItems as NavItem[]).pop();
            }
        });

        it('treats an extra config item\'s route as a config path', () => {
            const fake: NavItem = { id: 'features', label: 'Features', hint: 'Feature toggles', route: '/features-x', icon: appNavItems()[0].icon };
            expect(isConfigPath('/features-x/123')).toBe(false);
            (extraConfigItems as NavItem[]).push(fake);
            try {
                expect(isConfigPath('/features-x/123')).toBe(true);
            }
            finally {
                (extraConfigItems as NavItem[]).pop();
            }
        });

        it('uses config routes that match registered Angular paths', () => {
            const routes = configNavItems().map(i => i.route);
            expect(routes.slice(0, BASE_IDS.length)).toEqual(['/memories', '/mode', '/skills', '/integrations', '/agent-jobs']);
            for (const item of extraConfigItems) {
                expect(item.route.startsWith('/'), `${item.id} route`).toBe(true);
            }
        });
    });

    describe('navTooltip', () => {
        it('explains the section when its label is visible', () => {
            expect(navTooltip('Skills', "Saved instructions you add to a message with '/'", true))
                .toBe("Saved instructions you add to a message with '/'");
        });

        it('leads with the label when the control is icon-only', () => {
            expect(navTooltip('Skills', "Saved instructions you add to a message with '/'", false))
                .toBe("Skills: saved instructions you add to a message with '/'");
        });

        it('gives every nav item a hint that says more than its label', () => {
            for (const item of [...appNavItems(), ...configNavItems()]) {
                expect(item.hint.length).toBeGreaterThan(item.label.length);
                expect(item.hint.toLowerCase()).not.toBe(item.label.toLowerCase());
            }
        });
    });

    describe('isConfigPath', () => {
        const configCases = [
            '/memories',
            '/memories/123',
            '/memory',
            '/memory/123',
            '/mode',
            '/mode/abc-123',
            '/skills',
            '/skills/xyz',
            '/integrations',
            '/rituals',
            '/rituals/xyz',
            '/ritual',
            '/ritual/xyz',
            '/agent-jobs',
            '/agent-jobs/job-1',
            '/tools',
            '/features',
        ];

        for (const url of configCases) {
            it(`returns true for ${url}`, () => {
                expect(isConfigPath(url)).toBe(true);
            });
        }

        const appCases = [
            '/chat',
            '/personality',
            '/personality/getting-started',
            '/gallery',
            '/profile',
            '/dashboard',
            '/billing',
            '/subscription',
            '/auth/login',
            '/',
            '',
        ];

        for (const url of appCases) {
            it(`returns false for ${url || '(empty)'}`, () => {
                expect(isConfigPath(url)).toBe(false);
            });
        }

        it('strips query strings and hashes before matching', () => {
            expect(isConfigPath('/memories/abc?foo=bar')).toBe(true);
            expect(isConfigPath('/memory/abc?foo=bar')).toBe(true);
            expect(isConfigPath('/agent-jobs#anchor')).toBe(true);
            expect(isConfigPath('/profile?tab=preferences')).toBe(false);
        });
    });
});
