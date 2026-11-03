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
        expect((fixture.nativeElement.textContent ?? '') as string).toContain('Send a message');
    });

    it('renders one legend row per segment, ordered by token weight', () => {
        fixture.componentRef.setInput('breakdown', sampleBreakdown);
        fixture.detectChanges();
        const rows = fixture.nativeElement.querySelectorAll('.legend__row');
        expect(rows.length).toBe(4);
        expect((rows[0].textContent ?? '')).toContain('History');
    });

    it('computes fullness against the budget', () => {
        fixture.componentRef.setInput('breakdown', sampleBreakdown);
        fixture.detectChanges();
        expect(fixture.componentInstance.fillPct()).toBe(19);
        expect(fixture.componentInstance.total()).toBe(5720);
        expect(fixture.componentInstance.overBudget()).toBe(false);
    });

    it('shows the estimated input API cost near the token gauge', () => {
        fixture.componentRef.setInput('breakdown', sampleBreakdown);
        fixture.detectChanges();
        const gauge = fixture.nativeElement.querySelector('.gauge') as HTMLElement;
        expect(gauge.textContent ?? '').toContain('Est. $0.007');
        expect(gauge.textContent ?? '').not.toContain('Estimated input API cost');
    });

    it('does not invent an API cost when provider/model pricing is unknown', () => {
        fixture.componentRef.setInput('breakdown', {
            ...sampleBreakdown,
            provider: 'local',
            model: 'my-custom-model',
        } satisfies ContextBreakdown);
        fixture.detectChanges();
        const gauge = fixture.nativeElement.querySelector('.gauge') as HTMLElement;
        expect(gauge.querySelector('.gauge__cost')).toBeNull();
    });

    it('mounts the cost outlet when only a message id is available, without a dollar estimate', () => {
        // An unknown-priced model has no cost estimate, but the outlet must still mount so a
        // private build can render its per-turn credit cost from the message id. The default
        // build renders nothing there.
        fixture.componentRef.setInput('breakdown', {
            ...sampleBreakdown,
            provider: 'local',
            model: 'my-custom-model',
        } satisfies ContextBreakdown);
        fixture.componentRef.setInput('messageId', 'msg-1');
        fixture.detectChanges();
        const gauge = fixture.nativeElement.querySelector('.gauge') as HTMLElement;
        expect(gauge.querySelector('app-context-cost-outlet')).not.toBeNull();
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

    it('marks where the budget ends on an over-budget track and says what happens past it', () => {
        fixture.componentRef.setInput('breakdown', {
            ...sampleBreakdown,
            segments: [{ kind: 'history_turn', segments: 40, tokens: 42000, cacheable: false }],
            total_tokens: 42000,
            budget_tokens: 30000,
        } satisfies ContextBreakdown);
        fixture.detectChanges();
        const el = fixture.nativeElement as HTMLElement;
        expect(el.querySelector('.gauge')?.classList).toContain('gauge--over');
        const ceiling = el.querySelector('.gauge__ceiling') as HTMLElement;
        expect(ceiling).not.toBeNull();
        expect(parseFloat(ceiling.style.left)).toBeCloseTo((30000 / 42000) * 100, 1);
        expect(el.querySelector('.gauge__past')).not.toBeNull();
        const scale = el.querySelector('.gauge__scale')?.textContent ?? '';
        expect(scale).toContain('140% of checkpoint budget');
        expect(scale).toContain('12,000 over');
        expect(el.querySelector('.gauge__notice')?.textContent ?? '').toContain('compressed into the thread summary');
        expect(el.querySelector('.gauge__track')?.getAttribute('aria-label') ?? '').toContain('12,000 tokens over');
    });

    it('shows no ceiling marker within budget', () => {
        fixture.componentRef.setInput('breakdown', sampleBreakdown);
        fixture.detectChanges();
        const el = fixture.nativeElement as HTMLElement;
        expect(el.querySelector('.gauge__ceiling')).toBeNull();
        expect(el.querySelector('.gauge__notice')).toBeNull();
        expect(el.querySelector('.gauge__scale')?.textContent ?? '').toContain('19% of checkpoint budget');
    });

    // #397: an Advanced (ReAct) turn showed "60,000 / 30,000" and read as over budget, although
    // checkpointing measures the run's first call and the run itself compacts at its own limit.
    it('reads an Advanced turn against its run limit, not the checkpoint budget (60k of 30k)', () => {
        fixture.componentRef.setInput('breakdown', {
            ...sampleBreakdown,
            segments: [
                { kind: 'history_turn', segments: 6, tokens: 18000, cacheable: false },
                { kind: 'tool_result', segments: 12, tokens: 42000, cacheable: false },
            ],
            total_tokens: 60000,
            budget_tokens: 30000,
            agent_loop: 'advanced',
            run_budget_tokens: 140000,
            checkpoint_input_tokens: 20000,
        } satisfies ContextBreakdown);
        fixture.detectChanges();
        const el = fixture.nativeElement as HTMLElement;
        const text = el.textContent ?? '';
        expect(fixture.componentInstance.overBudget()).toBe(false);
        expect(text).toContain('/ 140,000 tokens');
        expect(text).not.toContain('/ 30,000 tokens');
        expect(el.querySelector('.gauge__scale')?.textContent ?? '').toContain('43% of run limit');
        expect(el.querySelector('.gauge__checkpoint')?.textContent ?? '').toContain('20,000 / 30,000 at turn start');
        expect(el.querySelector('.gauge__checkpoint--over')).toBeNull();
        expect(el.querySelector('.gauge__ceiling')).toBeNull();
    });

    it('explains an Advanced run past its own limit and its compactions', () => {
        fixture.componentRef.setInput('breakdown', {
            ...sampleBreakdown,
            segments: [{ kind: 'tool_result', segments: 30, tokens: 150000, cacheable: false }],
            total_tokens: 150000,
            budget_tokens: 30000,
            agent_loop: 'advanced',
            run_budget_tokens: 120000,
            checkpoint_input_tokens: 32000,
            run_compactions: 2,
        } satisfies ContextBreakdown);
        fixture.detectChanges();
        const el = fixture.nativeElement as HTMLElement;
        expect(fixture.componentInstance.overBudget()).toBe(true);
        expect(el.querySelector('.gauge__notice')?.textContent ?? '').toContain('work log');
        expect(el.querySelector('.gauge__checkpoint--over')?.textContent ?? '').toContain('older turns get summarized');
        expect(el.querySelector('.gauge__note')?.textContent ?? '').toContain('Compacted 2 times');
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
