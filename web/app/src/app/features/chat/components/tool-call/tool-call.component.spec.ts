import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ToolCall } from '../../../../core/models/toolcall.model';
import { ToolCallComponent } from './tool-call.component';

describe('ToolCallComponent', () => {
    let fixture: ComponentFixture<ToolCallComponent>;

    const toolCall: ToolCall = {
        id: 'tool-1',
        chat_message_id: 'message-1',
        tool_name: 'Web_Search',
        tool_input: '{"query":"Angular"}',
        tool_output: 'Found the result',
        tool_error: '',
        created_at: '2026-08-01T12:00:00Z',
        updated_at: '2026-08-01T12:00:00Z',
    };

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ToolCallComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(ToolCallComponent);
        fixture.componentRef.setInput('toolCall', toolCall);
    });

    it('creates', () => {
        expect(fixture.componentInstance).toBeTruthy();
    });

    it('keeps the detail panel collapsed until the toggle is clicked', () => {
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;
        const toggle = host.querySelector('.tool-call__toggle') as HTMLButtonElement;

        expect(host.querySelector('.tool-call__panel')).toBeNull();
        expect(toggle.getAttribute('aria-expanded')).toBe('false');
        expect(host.querySelector('.tool-call__name')?.textContent).toBe('web search');
        expect(host.querySelector('.tool-call__summary')?.textContent).toBe('"Angular" · Found the result');
        expect(host.textContent).toContain('Complete');

        toggle.click();
        fixture.detectChanges();

        expect(host.querySelector('.tool-call__panel')).not.toBeNull();
        expect(sections(host)).toEqual([
            ['Input', '{\n  "query": "Angular"\n}'],
            ['Output', 'Found the result'],
        ]);
        expect(toggle.getAttribute('aria-expanded')).toBe('true');
    });

    it('renders the failed state and error output branch', () => {
        fixture.componentRef.setInput('toolCall', {
            ...toolCall,
            tool_output: '',
            tool_error: 'Network unavailable',
        });
        fixture.componentInstance.expanded.set(true);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.querySelector('.tool-call__status-dot--error')).not.toBeNull();
        expect(host.textContent).toContain('Failed');
        expect(sections(host).at(-1)).toEqual(['Error', 'Network unavailable']);
    });

    it('pretty-prints JSON output', () => {
        fixture.componentRef.setInput('toolCall', { ...toolCall, tool_output: '{"memories":[{"id":"m1"}]}' });
        fixture.componentInstance.expanded.set(true);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.querySelector('.tool-call__summary')?.textContent).toBe('"Angular" · 1 memory');
        expect(sections(host).at(-1)).toEqual(['Output', '{\n  "memories": [\n    {\n      "id": "m1"\n    }\n  ]\n}']);
    });

    it('renders a live call as running until it settles', () => {
        fixture.componentRef.setInput('toolCall', { ...toolCall, tool_output: '' });
        fixture.componentRef.setInput('status', 'running');
        fixture.componentInstance.expanded.set(true);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.querySelector('.tool-call__status-dot--running')).not.toBeNull();
        expect(host.querySelector('.tool-call__status')?.textContent).toBe('Running…');
        expect(host.querySelector('.tool-call__summary')?.textContent).toBe('"Angular"');
        expect(sections(host).at(-1)).toEqual(['Output', 'Waiting for result…']);
        expect(host.querySelector('.tool-call__details')).toBeNull();

        fixture.componentRef.setInput('toolCall', toolCall);
        fixture.componentRef.setInput('status', 'complete');
        fixture.detectChanges();
        expect(host.querySelector('.tool-call__status-dot--running')).toBeNull();
        expect(host.querySelector('.tool-call__status')?.textContent).toBe('Complete');
    });

    it('emits the tool call from the expanded detail action', () => {
        let emitted: ToolCall | undefined;
        fixture.componentInstance.openDetail.subscribe(value => (emitted = value));
        fixture.componentInstance.expanded.set(true);
        fixture.detectChanges();

        (fixture.nativeElement.querySelector('.tool-call__details') as HTMLButtonElement).click();
        expect(emitted).toEqual(toolCall);
    });
});

/** [label, body] for each Input/Output/Error section of the expanded panel. */
function sections(host: HTMLElement): string[][] {
    return Array.from(host.querySelectorAll('.tool-call__section')).map(section => [
        section.querySelector('.tool-call__section-label')?.textContent?.trim() ?? '',
        section.querySelector('pre')?.textContent ?? '',
    ]);
}
