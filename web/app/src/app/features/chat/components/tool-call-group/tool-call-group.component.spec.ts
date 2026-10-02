import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { ToolCallGroupComponent } from './tool-call-group.component';
import { ToolCall } from '../../../../core/models/toolcall.model';

describe('ToolCallGroupComponent', () => {
    let fixture: ComponentFixture<ToolCallGroupComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ToolCallGroupComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(ToolCallGroupComponent);
    });

    it('renders a grouped toggle for multiple tool calls', () => {
        fixture.componentRef.setInput('toolCalls', [toolCall('1'), toolCall('2')]);
        fixture.detectChanges();

        const toggle = fixture.nativeElement.querySelector('.tool-call-group__toggle') as HTMLButtonElement;
        expect(toggle).not.toBeNull();
        expect(toggle.textContent).toContain('2 tool calls');

        toggle.click();
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelectorAll('app-tool-call').length).toBe(2);
    });

    it('shows a live group open with a working title while a call runs', () => {
        fixture.componentRef.setInput('toolCalls', [{ ...toolCall('1'), status: 'complete' }, { ...toolCall('2'), status: 'running' }]);
        fixture.componentRef.setInput('live', true);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.querySelector('.tool-call-group__toggle')?.textContent).toContain('working · 2 tool calls');
        expect(host.querySelectorAll('app-tool-call').length).toBe(2);
        expect(host.querySelector('.tool-call__status-dot--running')).not.toBeNull();

        (host.querySelector('.tool-call-group__toggle') as HTMLButtonElement).click();
        fixture.detectChanges();
        expect(host.querySelectorAll('app-tool-call').length).toBe(0);
    });

    it('renders a single tool call without the grouped wrapper', () => {
        fixture.componentRef.setInput('toolCalls', [toolCall('1')]);
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.tool-call-group__toggle')).toBeNull();
        expect(fixture.nativeElement.querySelectorAll('app-tool-call').length).toBe(1);
    });
});

function toolCall(id: string): ToolCall {
    return {
        id,
        chat_message_id: 'message-1',
        tool_name: 'web_search',
        tool_input: '{"query":"hello"}',
        tool_output: '{"answer":"hi"}',
        tool_error: '',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
    };
}
