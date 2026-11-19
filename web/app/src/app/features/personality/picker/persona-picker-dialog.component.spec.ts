import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Router } from '@angular/router';
import { environment } from '@environments/environment';

import { Personality } from '../../../core/models/personality.model';
import { makeTestPersonality } from '../../../core/models/personality.test-fixtures';
import { PersonalityService } from '../../../core/services/personality.service';
import { PersonaPickerDialogComponent } from './persona-picker-dialog.component';

const LIST_URL = `${environment.apiUrl}/personality?page=1&limit=100`;

/**
 * #318: the app layout keeps one picker dialog mounted for the whole session, so these specs
 * drive a single instance through open/close cycles the way the layout does.
 */
describe('PersonaPickerDialogComponent', () => {
    let fixture: ComponentFixture<PersonaPickerDialogComponent>;
    let component: PersonaPickerDialogComponent;
    let httpMock: HttpTestingController;
    let personalities: PersonalityService;

    const vera = makeTestPersonality({ id: 'p-vera', name: 'Vera' });
    const card = makeTestPersonality({ id: 'p-card', name: 'Imported Card' });

    beforeEach(() => {
        TestBed.configureTestingModule({
            imports: [PersonaPickerDialogComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                { provide: Router, useValue: { navigateByUrl: vi.fn() } },
            ],
        });
        httpMock = TestBed.inject(HttpTestingController);
        personalities = TestBed.inject(PersonalityService);
        fixture = TestBed.createComponent(PersonaPickerDialogComponent);
        component = fixture.componentInstance;
        fixture.detectChanges();
    });

    afterEach(() => httpMock.verify());

    function setOpen(open: boolean): void {
        fixture.componentRef.setInput('open', open);
        fixture.detectChanges();
    }

    function flushList(results: Personality[]): void {
        httpMock.expectOne(LIST_URL).flush({ results, total_count: results.length, page: 1 });
        fixture.detectChanges();
    }

    function rowNames(): string[] {
        return Array.from(fixture.nativeElement.querySelectorAll('[role="option"]') as NodeListOf<HTMLElement>)
            .map(row => row.textContent ?? '');
    }

    it('does not fetch until it is opened', () => {
        httpMock.expectNone(LIST_URL);
        setOpen(true);
        flushList([vera]);
        expect(rowNames().some(name => name.includes('Vera'))).toBe(true);
    });

    it('lists a personality imported after the first open without a page reload', () => {
        setOpen(true);
        flushList([vera]);
        setOpen(false);

        // Character-card import from the personalities page, while the dialog stays mounted.
        personalities.importSillyTavernCard('{"spec":"chara_card_v2"}').subscribe();
        httpMock.expectOne(`${environment.apiUrl}/personality/import/sillytavern`)
            .flush({ personality: card, warnings: [], lore_files: [] });
        // Closed: the change is noted, the fetch waits for the next open.
        httpMock.expectNone(LIST_URL);

        setOpen(true);
        // Stale-while-revalidate: the previous list stays up (no loading flash) until the refetch lands.
        expect(component.loading()).toBe(false);
        expect(rowNames().some(name => name.includes('Vera'))).toBe(true);
        flushList([card, vera]);

        expect(component.personalities().map(p => p.id)).toEqual(['p-card', 'p-vera']);
        expect(rowNames().some(name => name.includes('Imported Card'))).toBe(true);
    });

    it('refetches when reopened even with no change seen here (created in another tab or by a tool)', () => {
        setOpen(true);
        flushList([vera]);
        setOpen(false);

        setOpen(true);
        flushList([vera, card]);
        expect(component.personalities().map(p => p.id)).toEqual(['p-vera', 'p-card']);
    });

    it('refetches while open when a personality is created', () => {
        setOpen(true);
        flushList([vera]);

        personalities.createPersonality({ name: 'Imported Card', system_prompt: 'sp' }).subscribe();
        httpMock.expectOne(req => req.method === 'POST' && req.url === `${environment.apiUrl}/personality`).flush(card);
        flushList([vera, card]);

        expect(component.personalities().map(p => p.id)).toEqual(['p-vera', 'p-card']);
    });

    it('drops a deleted personality from the list', () => {
        setOpen(true);
        flushList([vera, card]);

        personalities.deletePersonality('p-card').subscribe();
        httpMock.expectOne(req => req.method === 'DELETE').flush(null);
        flushList([vera]);

        expect(component.personalities().map(p => p.id)).toEqual(['p-vera']);
    });

    it('keeps the visible list when a background refresh fails', () => {
        setOpen(true);
        flushList([vera]);
        setOpen(false);

        setOpen(true);
        httpMock.expectOne(LIST_URL).flush('boom', { status: 500, statusText: 'Server Error' });
        fixture.detectChanges();

        expect(component.errorMessage()).toBeNull();
        expect(component.personalities().map(p => p.id)).toEqual(['p-vera']);
    });

    it('shows an error when the first load fails', () => {
        setOpen(true);
        expect(component.loading()).toBe(true);
        httpMock.expectOne(LIST_URL).flush('boom', { status: 500, statusText: 'Server Error' });
        fixture.detectChanges();

        expect(component.loading()).toBe(false);
        expect(component.errorMessage()).toContain('Failed to load personalities');
    });

    describe('new-thread form', () => {
        function openForm(): void {
            fixture.componentRef.setInput('threadForm', true);
            setOpen(true);
            flushList([vera, card]);
        }

        function el(selector: string): HTMLElement | null {
            return fixture.nativeElement.querySelector(selector);
        }

        function startButton(): HTMLButtonElement {
            return el('[data-testid="new-thread-start"] button') as HTMLButtonElement;
        }

        it('orders Thread Title, Choose a Personality, then Private, with only the buttons in the footer', () => {
            openForm();

            const title = el('app-thread-title-field') as HTMLElement;
            const personality = el('#new-thread-personality-heading') as HTMLElement;
            const privateRow = el('[data-testid="new-thread-sandbox"]') as HTMLElement;
            expect(title.compareDocumentPosition(personality) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
            expect(personality.compareDocumentPosition(privateRow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
            expect(fixture.nativeElement.textContent).not.toContain('Privacy');
            // The footer's full-width top border is the line above the buttons, so Private sits above it.
            expect(el('footer [data-testid="new-thread-sandbox"]')).toBeNull();
            expect(el('footer [data-testid="new-thread-start"]')).not.toBeNull();
        });

        it('starts with the default thread name in the title', () => {
            openForm();

            expect((el('[data-testid="new-thread-title"]') as HTMLInputElement).value).toBe('New thread');
            expect(component.threadTitle()).toBe('New thread');
        });

        it('does not pick a personality when the title text is selected on focus', () => {
            const hostSelect = vi.fn();
            fixture.nativeElement.addEventListener('select', hostSelect);
            const picked: Personality[] = [];
            component.select.subscribe(p => picked.push(p));
            openForm();

            (el('[data-testid="new-thread-title"]') as HTMLInputElement).dispatchEvent(new Event('select', { bubbles: true }));

            expect(hostSelect).not.toHaveBeenCalled();
            expect(picked).toEqual([]);
        });

        it('puts Start thread before Cancel in the DOM (primary first)', () => {
            openForm();

            const labels = Array.from(fixture.nativeElement.querySelectorAll('footer ui-button') as NodeListOf<HTMLElement>)
                .map(b => b.textContent?.trim());
            expect(labels).toEqual(['Start thread', 'Cancel']);
        });

        it('only marks a personality on click; Start thread is what picks it', () => {
            const picked: Personality[] = [];
            component.select.subscribe(p => picked.push(p));
            openForm();
            expect(startButton().disabled).toBe(true);

            (fixture.nativeElement.querySelectorAll('[role="option"]')[1] as HTMLElement).click();
            fixture.detectChanges();
            expect(picked).toEqual([]);
            expect(component.chosen()?.id).toBe('p-card');
            expect(startButton().disabled).toBe(false);

            startButton().click();
            expect(picked.map(p => p.id)).toEqual(['p-card']);
        });

        it('carries the Privacy choice and the Thread Title to the parent', () => {
            openForm();
            const input = el('[data-testid="new-thread-title"]') as HTMLInputElement;
            input.value = 'Q3 retro';
            input.dispatchEvent(new Event('input'));
            component.sandboxed.set(true);

            expect(component.threadTitle()).toBe('Q3 retro');
            expect(component.sandboxed()).toBe(true);
        });

        it('reopens fresh: no last pick, and the title back to the default name', () => {
            openForm();
            component.chosen.set(vera);
            component.threadTitle.set('Old title');
            setOpen(false);

            setOpen(true);
            flushList([vera, card]);

            expect(component.chosen()).toBeNull();
            expect(component.threadTitle()).toBe('New thread');
        });

        it('keeps the one-click pick for every other caller', () => {
            const picked: Personality[] = [];
            component.select.subscribe(p => picked.push(p));
            setOpen(true);
            flushList([vera]);

            (fixture.nativeElement.querySelectorAll('[role="option"]')[0] as HTMLElement).click();

            expect(picked.map(p => p.id)).toEqual(['p-vera']);
        });
    });
});
