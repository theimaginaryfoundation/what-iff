import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Subject, of, throwError } from 'rxjs';

import { SearchSection } from '../models/search.model';
import { CommandPaletteService } from './command-palette.service';
import { ChatIconComponent, GearIconComponent } from '../../shared/ui/icons/icons';

describe('CommandPaletteService', () => {
    let service: CommandPaletteService;

    beforeEach(() => {
        vi.useFakeTimers();
        TestBed.configureTestingModule({
            providers: [provideZonelessChangeDetection(), CommandPaletteService],
        });
        service = TestBed.inject(CommandPaletteService);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    function chatSection(label: string): SearchSection {
        return {
            type: 'chat',
            results: [
                {
                    id: 'chat-1',
                    label,
                    description: 'Chat',
                    route: '/chat',
                    icon_type: 'chat',
                    score: 80,
                },
            ],
        };
    }

    it('starts hidden with empty query and no sections', () => {
        expect(service.visible()).toBe(false);
        expect(service.query()).toBe('');
        expect(service.sections()).toEqual([]);
        expect(service.loading()).toBe(false);
    });

    it('open() and close() flip visibility', () => {
        service.open();
        expect(service.visible()).toBe(true);
        service.close();
        expect(service.visible()).toBe(false);
    });

    it('toggle() flips visibility', () => {
        expect(service.visible()).toBe(false);
        service.toggle();
        expect(service.visible()).toBe(true);
        service.toggle();
        expect(service.visible()).toBe(false);
    });

    it('register returns a disposer that detaches the handler', () => {
        const handler = vi.fn().mockName('handler').mockReturnValue(of([]));
        const dispose = service.register(handler);
        service.setQuery('atlas');
        vi.advanceTimersByTime(250);
        expect(handler).toHaveBeenCalledTimes(1);

        dispose();
        handler.mockClear();
        service.setQuery('atlas-2');
        vi.advanceTimersByTime(250);
        expect(handler).not.toHaveBeenCalled();
    });

    it('debounces handler calls until the query stops changing for 200ms', () => {
        const handler = vi.fn().mockName('handler').mockReturnValue(of([]));
        service.register(handler);

        service.setQuery('a');
        service.setQuery('at');
        service.setQuery('atl');
        expect(handler).not.toHaveBeenCalled();

        vi.advanceTimersByTime(199);
        expect(handler).not.toHaveBeenCalled();

        vi.advanceTimersByTime(2);
        expect(handler).toHaveBeenCalledTimes(1);
        expect(handler).toHaveBeenCalledWith('atl');
    });

    it('clears sections and skips the network when query is empty', () => {
        const handler = vi.fn().mockName('handler').mockReturnValue(of([chatSection('atlas')]));
        service.register(handler);
        service.setQuery('atlas');
        vi.advanceTimersByTime(250);
        expect(service.sections().length).toBe(1);

        service.setQuery('');
        vi.advanceTimersByTime(250);
        expect(service.sections()).toEqual([]);
        expect(service.loading()).toBe(false);
    });

    it('merges sections from multiple handlers in canonical order', () => {
        service.register(() => of([chatSection('atlas roadmap')]));
        service.register(() => of<SearchSection[]>([
            {
                type: 'memory',
                results: [
                    {
                        id: 'm1',
                        label: 'atlas snippet',
                        description: 'Memory',
                        route: '/memory/m1',
                        icon_type: 'memory',
                        score: 60,
                    },
                ],
            },
        ]));
        service.setQuery('atlas');
        vi.advanceTimersByTime(250);
        expect(service.sections().map(s => s.type)).toEqual(['chat', 'memory']);
    });

    it('survives an erroring handler by keeping the rest', () => {
        service.register(() => throwError(() => new Error('boom')));
        service.register(() => of([chatSection('atlas')]));
        service.setQuery('atlas');
        vi.advanceTimersByTime(250);
        expect(service.sections().map(s => s.type)).toEqual(['chat']);
        expect(service.loading()).toBe(false);
    });

    it('cancels the previous request when the query changes', () => {
        const subjectsByQuery = new Map<string, Subject<SearchSection[]>>();
        service.register(query => {
            const subject = new Subject<SearchSection[]>();
            subjectsByQuery.set(query, subject);
            return subject.asObservable();
        });

        service.setQuery('first');
        vi.advanceTimersByTime(250);
        const firstSubject = subjectsByQuery.get('first')!;
        expect(service.loading()).toBe(true);

        service.setQuery('second');
        vi.advanceTimersByTime(250);

        firstSubject.next([chatSection('first stale')]);
        firstSubject.complete();

        expect(service.sections()).toEqual([]);
    });

    describe('static commands', () => {
        function makeCommand(overrides: Partial<Parameters<typeof service.registerCommand>[0]> = {}) {
            return {
                id: overrides.id ?? 'cmd-id',
                label: overrides.label ?? 'New chat',
                description: overrides.description ?? 'Start a new conversation',
                icon: overrides.icon ?? ChatIconComponent,
                keywords: overrides.keywords,
                run: overrides.run ?? vi.fn().mockName('run'),
            };
        }

        it('lists all commands when query is empty', () => {
            service.registerCommand(makeCommand({ id: 'a', label: 'Aaa' }));
            service.registerCommand(makeCommand({ id: 'b', label: 'Bbb' }));
            const ids = service.commandResults().map(c => c.id);
            expect(ids).toEqual(['a', 'b']);
        });

        it('filters and ranks commands when a query is set', () => {
            service.registerCommand(makeCommand({ id: 'a', label: 'New chat' }));
            service.registerCommand(makeCommand({ id: 'b', label: 'Toggle theme' }));
            service.registerCommand(makeCommand({ id: 'c', label: 'Settings', keywords: ['theme', 'preferences'] }));
            service.setQuery('theme');
            const ids = service.commandResults().map(c => c.id);
            expect(ids).toContain('b');
            expect(ids).toContain('c');
            expect(ids).not.toContain('a');
        });

        it('runCommand invokes the registered handler and closes the palette', () => {
            const run = vi.fn().mockName('run');
            service.registerCommand(makeCommand({ id: 'toggle', icon: GearIconComponent, run }));
            service.open();
            service.runCommand('toggle');
            expect(run).toHaveBeenCalledTimes(1);
            expect(service.visible()).toBe(false);
        });

        it('runCommand is a no-op for unknown ids', () => {
            expect(() => service.runCommand('does-not-exist')).not.toThrow();
        });
    });
});
