import type { MockedObject } from "vitest";
import { TestBed, ComponentFixture } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { Router } from '@angular/router';

import { Personality } from '../../../core/models/personality.model';
import { PersonaPickerComponent } from './persona-picker.component';

function makePersonality(id: string, name: string, chat_count = 0): Personality {
    return {
        id,
        name,
        system_prompt: 'sp',
        auto_pin_memories: false,
        expressions_enabled: true,
        image_style: 'auto', cover_image_id: null,
        cover_image_url: null,
        created_at: '2026-04-28T00:00:00Z',
        updated_at: '2026-04-28T00:00:00Z',
        stats: { chat_count, last_used_at: null },
    };
}

describe('PersonaPickerComponent', () => {
    let fixture: ComponentFixture<PersonaPickerComponent>;
    let component: PersonaPickerComponent;
    let router: Pick<MockedObject<Router>, 'navigateByUrl'>;

    beforeEach(() => {
        router = {
            navigateByUrl: vi.fn().mockName("Router.navigateByUrl")
        } as unknown as Pick<MockedObject<Router>, 'navigateByUrl'>;
        TestBed.configureTestingModule({
            imports: [PersonaPickerComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                { provide: Router, useValue: router },
            ],
        });
        fixture = TestBed.createComponent(PersonaPickerComponent);
        component = fixture.componentInstance;
    });

    function setPersonalities(list: Personality[]): void {
        fixture.componentRef.setInput('personalities', list);
        fixture.detectChanges();
    }

    it('renders one row per personality', () => {
        setPersonalities([
            makePersonality('p-1', 'Vera', 3),
            makePersonality('p-2', 'Filbolt', 0),
        ]);
        const rows = fixture.nativeElement.querySelectorAll('[role="option"]');
        expect(rows.length).toBe(2);
        expect(rows[0].textContent).toContain('Vera');
        expect(rows[0].textContent).toContain('3 threads');
        expect(rows[1].textContent).toContain('New thread');
    });

    it('filters by name query', () => {
        setPersonalities([
            makePersonality('p-1', 'Vera Calder'),
            makePersonality('p-2', 'Filbolt Pottsworth'),
            makePersonality('p-3', 'Quinn Hawthorne'),
        ]);
        component.onQueryInput('quinn');
        fixture.detectChanges();
        const rows = fixture.nativeElement.querySelectorAll('[role="option"]');
        expect(rows.length).toBe(1);
        expect(rows[0].textContent).toContain('Quinn');
    });

    it('can render the compact filter-list appearance', () => {
        fixture.componentRef.setInput('appearance', 'filter-list');
        setPersonalities([
            makePersonality('p-1', 'Vera Calder'),
            makePersonality('p-2', 'Filbolt Pottsworth'),
        ]);

        const input = fixture.nativeElement.querySelector('input') as HTMLInputElement;
        expect(fixture.nativeElement.querySelector('[role="option"]')).toBeNull();

        input.dispatchEvent(new Event('focus'));
        fixture.detectChanges();

        expect(input.placeholder).toBe('Filter by personality…');
        expect(fixture.nativeElement.querySelectorAll('.app-sidebar__personality-option').length).toBe(2);
    });

    it('navigates highlighted index with arrow keys', () => {
        setPersonalities([
            makePersonality('p-1', 'A'),
            makePersonality('p-2', 'B'),
            makePersonality('p-3', 'C'),
        ]);
        expect(component.highlightedIndex()).toBe(0);

        const downEvent = new KeyboardEvent('keydown', { key: 'ArrowDown' });
        component.onKeydown(downEvent);
        expect(component.highlightedIndex()).toBe(1);
        component.onKeydown(downEvent);
        expect(component.highlightedIndex()).toBe(2);
        // clamps at end
        component.onKeydown(downEvent);
        expect(component.highlightedIndex()).toBe(2);

        const upEvent = new KeyboardEvent('keydown', { key: 'ArrowUp' });
        component.onKeydown(upEvent);
        expect(component.highlightedIndex()).toBe(1);
    });

    it('jumps to first/last with Home/End', () => {
        setPersonalities([
            makePersonality('p-1', 'A'),
            makePersonality('p-2', 'B'),
            makePersonality('p-3', 'C'),
        ]);
        component.onKeydown(new KeyboardEvent('keydown', { key: 'End' }));
        expect(component.highlightedIndex()).toBe(2);
        component.onKeydown(new KeyboardEvent('keydown', { key: 'Home' }));
        expect(component.highlightedIndex()).toBe(0);
    });

    it('emits select on Enter', () => {
        setPersonalities([
            makePersonality('p-1', 'Vera'),
            makePersonality('p-2', 'Filbolt'),
        ]);
        const events: Personality[] = [];
        component.select.subscribe(p => events.push(p));

        component.onKeydown(new KeyboardEvent('keydown', { key: 'ArrowDown' }));
        component.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));
        expect(events.length).toBe(1);
        expect(events[0].id).toBe('p-2');
    });

    it('emits cancel on Escape', () => {
        setPersonalities([makePersonality('p-1', 'Vera')]);
        let cancelled = false;
        component.cancel.subscribe(() => { cancelled = true; });
        component.onKeydown(new KeyboardEvent('keydown', { key: 'Escape' }));
        expect(cancelled).toBe(true);
    });

    it('"create new" routes to /personality?create=1', () => {
        setPersonalities([makePersonality('p-1', 'Vera')]);
        component.onCreateNew();
        expect(router.navigateByUrl).toHaveBeenCalledWith('/personality?create=1');
    });

    it('shows empty state when no rows match', () => {
        setPersonalities([makePersonality('p-1', 'Vera')]);
        component.onQueryInput('zzz');
        fixture.detectChanges();
        const empty = fixture.nativeElement.querySelector('[role="status"]');
        expect(empty?.textContent ?? '').toContain('No personalities');
    });
});
