import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { ChatTurnAdvancedProgress } from '../../../../core/models/job.model';
import { AdvancedRunProgressComponent, PLAN_STATUS_DISPLAY } from './advanced-run-progress.component';

describe('AdvancedRunProgressComponent', () => {
    let fixture: ComponentFixture<AdvancedRunProgressComponent>;
    let host: HTMLElement;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [AdvancedRunProgressComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();
        fixture = TestBed.createComponent(AdvancedRunProgressComponent);
        host = fixture.nativeElement as HTMLElement;
    });

    function render(progress: ChatTurnAdvancedProgress): void {
        fixture.componentRef.setInput('progress', progress);
        fixture.detectChanges();
    }

    it('shows only the step while there is no plan', () => {
        render({ step: 1, plan: [] });
        expect(host.querySelector('[role=status]')?.textContent?.trim()).toBe('Step 1');
        expect(host.querySelector('ul')).toBeNull();
    });

    it('lists each plan item with its marker and screen-reader label, and counts progress without dropped items', () => {
        render({
            step: 3,
            plan: [
                { content: 'Search', status: 'completed' },
                { content: 'Read', status: 'in_progress' },
                { content: 'Write', status: 'pending' },
                { content: 'Translate', status: 'cancelled' },
            ],
        });

        expect(host.querySelector('[role=status]')?.textContent).toContain('Step 3');
        expect(host.querySelector('.advanced-run__summary')?.textContent).toContain('1 of 3 done');

        const items = [...host.querySelectorAll('li')];
        expect(items.map(item => item.querySelector('.advanced-run__marker')?.textContent)).toEqual([
            PLAN_STATUS_DISPLAY.completed.marker,
            PLAN_STATUS_DISPLAY.in_progress.marker,
            PLAN_STATUS_DISPLAY.pending.marker,
            PLAN_STATUS_DISPLAY.cancelled.marker,
        ]);
        expect(items.map(item => item.querySelector('.sr-only')?.textContent)).toEqual(['Done: ', 'In progress: ', 'To do: ', 'Dropped: ']);
        expect(items[1].classList).toContain('advanced-run__item--in_progress');
        expect(items[3].querySelector('.advanced-run__content')?.textContent).toBe('Translate');
    });

    it('omits the summary when every item was dropped', () => {
        render({ step: 2, plan: [{ content: 'Old idea', status: 'cancelled' }] });
        expect(host.querySelector('.advanced-run__summary')).toBeNull();
        expect(host.querySelectorAll('li').length).toBe(1);
    });
});
