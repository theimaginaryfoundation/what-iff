import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { ContextBreakdown } from '../../../../../core/models/message.model';
import { ContextBreakdownTabComponent } from './context-breakdown-tab.component';

const sampleBreakdown: ContextBreakdown = {
    segments: [
        { kind: 'system_prompt', segments: 1, tokens: 800, cacheable: true },
        { kind: 'history_turn', segments: 6, tokens: 4200, cacheable: false },
        { kind: 'memory_context', segments: 1, tokens: 600, cacheable: false },
        { kind: 'user_message', segments: 1, tokens: 120, cacheable: false },
    ],
    total_tokens: 5720,
    budget_tokens: 30000,
    model: 'gpt-5',
    provider: 'openai',
    captured_at: '2026-08-17T12:00:00Z',
};

describe('ContextBreakdownTabComponent', () => {
    let fixture: ComponentFixture<ContextBreakdownTabComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ContextBreakdownTabComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(ContextBreakdownTabComponent);
    });

    it('shows an empty state when no breakdown is present', () => {
        fixture.componentRef.setInput('breakdown', null);
        fixture.detectChanges();

        const text = (fixture.nativeElement.textContent ?? '') as string;
        expect(text).toContain('Send a message');
    });

    it('renders one legend row per segment, ordered by token weight', () => {
        fixture.componentRef.setInput('breakdown', sampleBreakdown);
        fixture.detectChanges();

        const rows = fixture.nativeElement.querySelectorAll('.legend__row');
        expect(rows.length).toBe(4);
        // Largest segment (history, 4200 tok) should sort first.
        expect((rows[0].textContent ?? '')).toContain('History');
    });

    it('computes fullness against the budget', () => {
        fixture.componentRef.setInput('breakdown', sampleBreakdown);
        fixture.detectChanges();

        // 5720 / 30000 ≈ 19%
        expect(fixture.componentInstance.fillPct()).toBe(19);
        expect(fixture.componentInstance.total()).toBe(5720);
        expect(fixture.componentInstance.overBudget()).toBe(false);
    });

    it('shows the estimated provider API cost near the token gauge', () => {
        const breakdownWithCost = {
            ...sampleBreakdown,
            api_cost: {
                amount_usd: 0.0134,
                input_tokens: 5720,
                output_tokens: 625,
                input_usd_per_million: 1.25,
                output_usd_per_million: 10,
                pricing_source: 'OpenAI API pricing',
                pricing_updated_at: '2026-08-30T00:00:00Z',
                calculated_at: '2026-08-30T00:00:01Z',
            },
        } as ContextBreakdown;

        fixture.componentRef.setInput('breakdown', breakdownWithCost);
        fixture.detectChanges();

        const gauge = fixture.nativeElement.querySelector('.gauge') as HTMLElement;
        expect(gauge.textContent ?? '').toContain('Estimated API cost');
        expect(gauge.textContent ?? '').toContain('$0.0134');
    });

    it('does not invent an API cost when pricing is unknown', () => {
        fixture.componentRef.setInput('breakdown', sampleBreakdown);
        fixture.detectChanges();

        const gauge = fixture.nativeElement.querySelector('.gauge') as HTMLElement;
        expect(gauge.querySelector('.gauge__cost')).toBeNull();
    });

    it('never lets the denominator fall below usage and flags over-budget', () => {
        fixture.componentRef.setInput('breakdown', {
            ...sampleBreakdown,
            segments: [{ kind: 'history_turn', segments: 40, tokens: 42000, cacheable: false }],
            total_tokens: 42000,
            budget_tokens: 30000,
        } satisfies ContextBreakdown);
        fixture.detectChanges();

        expect(fixture.componentInstance.overBudget()).toBe(true);
        expect(fixture.componentInstance.fillPct()).toBe(100);
        expect(fixture.componentInstance.budget()).toBeGreaterThanOrEqual(42000);
        const text = (fixture.nativeElement.textContent ?? '') as string;
        expect(text).toContain('/ 30,000 tokens');
        expect(text).toContain('Heavy turns can exceed this budget until the next compaction.');
    });

    it('shows segment names and proportions without token counts', () => {
        fixture.componentRef.setInput('breakdown', sampleBreakdown);
        fixture.detectChanges();

        const history = fixture.nativeElement.querySelector('.legend__row') as HTMLElement;
        expect(history.textContent ?? '').toContain('History');
        expect(history.textContent ?? '').not.toContain('4,200');
        expect(history.querySelector('.legend__percent')?.textContent?.trim()).toBe('73%');
    });

    it('merges expression portraits into developer notes', () => {
        fixture.componentRef.setInput('breakdown', {
            ...sampleBreakdown,
            segments: [
                { kind: 'developer_context', segments: 1, tokens: 100, cacheable: false },
                { kind: 'expression_portrait', segments: 1, tokens: 50, cacheable: false, images: 1 },
            ],
            total_tokens: 150,
        } satisfies ContextBreakdown);
        fixture.detectChanges();

        const rows = fixture.nativeElement.querySelectorAll('.legend__row');
        expect(rows.length).toBe(1);
        expect((rows[0].textContent ?? '')).toContain('Developer notes');
        expect((rows[0].textContent ?? '')).not.toContain('Expression portrait');
        expect((fixture.nativeElement.textContent ?? '')).toContain('vendor-reported input usage');
    });
});
