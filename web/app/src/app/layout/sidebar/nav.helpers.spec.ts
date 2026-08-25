import { appNavItems, configNavItems, extraConfigItems, isConfigPath, NavItem } from './nav.helpers';

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
        it('lists base items in canonical order including jobs', () => {
            const ids = configNavItems().map(i => i.id);
            expect(ids).toEqual(['memories', 'modes', 'skills', 'tools', 'jobs']);
        });

        it('appends extraConfigItems after the base set', () => {
            const fake: NavItem = {
                id: 'features',
                label: 'Features',
                route: '/features',
                icon: appNavItems()[0].icon,
            };
            (extraConfigItems as NavItem[]).push(fake);
            try {
                const ids = configNavItems().map(i => i.id);
                expect(ids).toEqual(['memories', 'modes', 'skills', 'tools', 'jobs', 'features']);
            }
            finally {
                (extraConfigItems as NavItem[]).pop();
            }
        });

        it('uses config routes that match registered Angular paths', () => {
            const routes = configNavItems().map(i => i.route);
            expect(routes).toEqual(['/memories', '/mode', '/skills', '/integrations', '/agent-jobs']);
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
