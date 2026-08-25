import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { DEFAULT_MEMORY_VIEW_FILTERS } from '../helpers/memory-filter.helpers';
import { MemoryFilterBarComponent } from './memory-filter-bar.component';

describe('MemoryFilterBarComponent', () => {
    let fixture: ComponentFixture<MemoryFilterBarComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [MemoryFilterBarComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(MemoryFilterBarComponent);
        fixture.componentRef.setInput('filters', { ...DEFAULT_MEMORY_VIEW_FILTERS });
    });

    it('creates', () => {
        expect(fixture.componentInstance).toBeTruthy();
    });

    it('renders scope, personality, and chat @for options while level is disabled', () => {
        fixture.componentRef.setInput('personalities', [
            { id: 'p1', label: 'Ada' },
            { id: 'p2', label: 'Grace' },
        ]);
        fixture.componentRef.setInput('chats', [{ id: 'c1', label: 'Project chat' }]);
        fixture.detectChanges();
        const selects = fixture.nativeElement.querySelectorAll('select') as NodeListOf<HTMLSelectElement>;

        expect(selects.length).toBe(3);
        expect(selects[0].options.length).toBe(3);
        expect(selects[1].options.length).toBe(3);
        expect(selects[1].textContent).toContain('Ada');
        expect(selects[1].textContent).toContain('Grace');
        expect(selects[2].options.length).toBe(2);
        expect(selects[2].textContent).toContain('Project chat');
    });

    it('renders every level option only when the feature is enabled', () => {
        fixture.detectChanges();
        expect(fixture.nativeElement.textContent).not.toContain('All levels');

        fixture.componentRef.setInput('levelEnabled', true);
        fixture.detectChanges();
        const selects = fixture.nativeElement.querySelectorAll('select') as NodeListOf<HTMLSelectElement>;

        expect(selects.length).toBe(4);
        expect(Array.from(selects[3].options).map(option => option.text)).toEqual([
            'All levels',
            'Global',
            'Personality',
            'Thread',
            'Summary',
        ]);
    });

    it('enables batch import only when enabled and not loading', () => {
        fixture.componentRef.setInput('importEnabled', true);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;
        const buttons = host.querySelectorAll<HTMLButtonElement>('button');
        const button = Array.from(buttons).find(candidate => candidate.textContent?.includes('Batch import')) as HTMLButtonElement;
        expect(button.disabled).toBe(false);

        fixture.componentRef.setInput('loading', true);
        fixture.detectChanges();
        expect(button.disabled).toBe(true);
    });
});
