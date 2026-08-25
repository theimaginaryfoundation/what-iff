import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { JobViewFilters } from '../../../core/services/job-view.service';
import { JobFilterBarComponent } from './job-filter-bar.component';

describe('JobFilterBarComponent', () => {
    let fixture: ComponentFixture<JobFilterBarComponent>;

    const filters: JobViewFilters = { search: '', status: '', scheduleType: '' };

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [JobFilterBarComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(JobFilterBarComponent);
        fixture.componentRef.setInput('filters', filters);
    });

    it('creates', () => {
        expect(fixture.componentInstance).toBeTruthy();
    });

    it('renders every status and schedule option from the @for blocks', () => {
        fixture.detectChanges();
        const selects = fixture.nativeElement.querySelectorAll('select') as NodeListOf<HTMLSelectElement>;

        expect(selects[0].options.length).toBe(5);
        expect(Array.from(selects[0].options).map(option => option.text)).toEqual([
            'All statuses',
            'Active',
            'Paused',
            'Complete',
            'Failed',
        ]);
        expect(Array.from(selects[1].options).map(option => option.text)).toEqual([
            'All schedule types',
            'One-off',
            'Recurring',
        ]);
    });

    it('renders both loading labels', () => {
        fixture.detectChanges();
        expect(fixture.nativeElement.textContent).toContain('Filters auto-apply');

        fixture.componentRef.setInput('loading', true);
        fixture.detectChanges();
        expect(fixture.nativeElement.textContent).toContain('Refreshing jobs...');
    });
});
