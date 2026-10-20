import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { TurnSettingsPickerComponent } from './turn-settings-picker.component';

describe('TurnSettingsPickerComponent', () => {
    let fixture: ComponentFixture<TurnSettingsPickerComponent>;
    let host: HTMLElement;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [TurnSettingsPickerComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(TurnSettingsPickerComponent);
        host = fixture.nativeElement as HTMLElement;
    });

    function trigger(): HTMLButtonElement {
        return host.querySelector('.turn-settings__trigger') as HTMLButtonElement;
    }

    function open(): void {
        trigger().click();
        fixture.detectChanges();
    }

    it('shows the thread effort and loop, and emits the thread changes', () => {
        fixture.componentRef.setInput('reasoningSupport', 'optional');
        fixture.componentRef.setInput('threadReasoningEffort', 'high');
        fixture.componentRef.setInput('threadAgentLoop', 'advanced');
        fixture.detectChanges();
        expect(trigger().textContent).toContain('High · Advanced');
        expect(trigger().textContent).not.toContain('set by');

        const efforts: unknown[] = [];
        const loops: unknown[] = [];
        fixture.componentInstance.reasoningEffortChange.subscribe(value => efforts.push(value));
        fixture.componentInstance.agentLoopChange.subscribe(value => loops.push(value));
        open();

        const radios = [...host.querySelectorAll<HTMLInputElement>('input[type=radio]')];
        const labels = radios.map(radio => {
            const label = radio.closest('label')!;
            return (label.querySelector('.turn-settings__option-label') ?? label).textContent?.trim();
        });
        expect(labels).toEqual(['Model default', 'Off', 'Low', 'Medium', 'High', 'Standard', 'Advanced']);
        radios[0].click();
        radios[5].click();
        expect(efforts).toEqual([null]);
        expect(loops).toEqual(['standard']);
    });

    it('hides the reasoning part when the model has no effort control', () => {
        fixture.componentRef.setInput('reasoningSupport', 'none');
        fixture.componentRef.setInput('threadReasoningEffort', 'high');
        fixture.detectChanges();
        expect(trigger().textContent).toContain('Standard');
        open();
        expect(host.querySelectorAll('legend').length).toBe(1);
        expect(host.querySelector('legend')?.textContent).toContain('Agent loop');
    });

    it('offers Off / On for a toggle model and checks On for any level', () => {
        fixture.componentRef.setInput('reasoningSupport', 'toggle');
        fixture.componentRef.setInput('threadReasoningEffort', 'low');
        fixture.detectChanges();
        expect(trigger().textContent).toContain('On');
        open();
        const checked = host.querySelector<HTMLInputElement>('input[type=radio]:checked');
        expect(checked?.closest('label')?.textContent?.trim()).toBe('On');
    });

    it('shows the mode value as effective and says the mode set it', () => {
        fixture.componentRef.setInput('reasoningSupport', 'optional');
        fixture.componentRef.setInput('threadReasoningEffort', 'low');
        fixture.componentRef.setInput('activeMode', { name: 'Deep work', reasoning_effort: 'high', agent_loop: null });
        fixture.detectChanges();

        expect(trigger().textContent).toContain('High');
        expect(trigger().textContent).toContain('set by mode');
        expect(trigger().getAttribute('aria-label')).toContain('set by the Deep work mode');

        open();
        expect(host.querySelector('.turn-settings__mode-note')?.textContent).toContain('Deep work mode sets High');
        // The popover still edits the thread's own value.
        const checked = host.querySelector<HTMLInputElement>('input[type=radio]:checked');
        expect(checked?.closest('label')?.textContent?.trim()).toBe('Low');
    });

    it('closes on an outside click but not on a click inside the panel', () => {
        fixture.detectChanges();
        open();
        expect(host.querySelector('[role=dialog]')).not.toBeNull();
        expect(trigger().getAttribute('aria-expanded')).toBe('true');
        expect(trigger().getAttribute('aria-controls')).toBe(fixture.componentInstance.panelId);

        host.querySelector<HTMLElement>('[role=dialog]')!.click();
        fixture.detectChanges();
        expect(host.querySelector('[role=dialog]')).not.toBeNull();

        const outside = document.createElement('div');
        document.body.appendChild(outside);
        outside.click();
        fixture.detectChanges();
        outside.remove();
        expect(host.querySelector('[role=dialog]')).toBeNull();
        expect(trigger().getAttribute('aria-controls')).toBeNull();
    });

    it('closes on Escape and returns focus to the trigger', () => {
        document.body.appendChild(host);
        fixture.detectChanges();
        open();

        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
        fixture.detectChanges();

        expect(host.querySelector('[role=dialog]')).toBeNull();
        expect(document.activeElement).toBe(trigger());
        host.remove();
    });

    it('ignores Escape while closed', () => {
        fixture.detectChanges();
        const focus = vi.spyOn(trigger(), 'focus');
        fixture.componentInstance.onEscape();
        expect(focus).not.toHaveBeenCalled();
        expect(fixture.componentInstance.open()).toBe(false);
    });

    it('does not open while disabled', () => {
        fixture.componentRef.setInput('disabled', true);
        fixture.detectChanges();
        expect(trigger().disabled).toBe(true);
        fixture.componentInstance.toggle();
        fixture.detectChanges();
        expect(host.querySelector('[role=dialog]')).toBeNull();
    });

    it('toggles closed from the trigger', () => {
        fixture.detectChanges();
        open();
        open();
        expect(host.querySelector('[role=dialog]')).toBeNull();
    });

    it('offers Low / Medium / High for an always-on model and runs a stored Off as Low', () => {
        fixture.componentRef.setInput('reasoningSupport', 'always');
        fixture.componentRef.setInput('threadReasoningEffort', 'off');
        fixture.detectChanges();
        expect(trigger().textContent).toContain('Low');

        const efforts: unknown[] = [];
        fixture.componentInstance.reasoningEffortChange.subscribe(value => efforts.push(value));
        open();
        const radios = [...host.querySelectorAll<HTMLInputElement>('input[type=radio]')];
        const labels = radios.slice(0, 4).map(radio => radio.closest('label')?.textContent?.trim());
        expect(labels).toEqual(['Model default', 'Low', 'Medium', 'High']);
        expect(radios[1].checked).toBe(true);
        radios[3].click();
        expect(efforts).toEqual(['high']);
    });

    it('labels an unset effort as the model default and does not highlight it', () => {
        fixture.componentRef.setInput('reasoningSupport', 'optional');
        fixture.detectChanges();
        expect(trigger().textContent?.trim()).toBe('Default');
        expect(trigger().classList).not.toContain('turn-settings__trigger--active');
        expect(trigger().getAttribute('aria-label')).toBe('Thread settings: reasoning Model default, agent loop Standard');
        expect(fixture.componentInstance.triggerTooltip()).toBe('Reasoning effort and agent loop for this thread');

        open();
        const checked = host.querySelector<HTMLInputElement>('input[type=radio]:checked');
        expect(checked?.closest('label')?.textContent?.trim()).toBe('Model default');
    });

    it('highlights the trigger for an advanced loop even without an effort control', () => {
        fixture.componentRef.setInput('threadAgentLoop', 'advanced');
        fixture.detectChanges();
        expect(trigger().textContent?.trim()).toBe('Advanced');
        expect(trigger().classList).toContain('turn-settings__trigger--active');
        expect(trigger().getAttribute('aria-label')).toBe('Thread settings: agent loop Advanced');
    });

    it('notes a mode-set agent loop and explains the override in the tooltip', () => {
        fixture.componentRef.setInput('reasoningSupport', 'none');
        fixture.componentRef.setInput('activeMode', { name: 'Research', agent_loop: 'advanced' });
        fixture.detectChanges();

        expect(trigger().textContent).toContain('set by mode');
        const tooltip = fixture.componentInstance.triggerTooltip();
        expect(tooltip).toContain('The Research mode sets this while it\'s active');
        expect(tooltip).toContain('applies when the mode is off');

        const loops: unknown[] = [];
        fixture.componentInstance.agentLoopChange.subscribe(value => loops.push(value));
        open();
        expect(host.querySelector('.turn-settings__mode-note')?.textContent).toContain('Research mode sets Advanced');
        // The thread's own loop (unset → Standard) stays checked; picking edits the thread.
        const radios = [...host.querySelectorAll<HTMLInputElement>('input[type=radio]')];
        expect(radios[0].checked).toBe(true);
        radios[1].click();
        expect(loops).toEqual(['advanced']);
    });

    it('does not credit the mode for an effort the model cannot show', () => {
        fixture.componentRef.setInput('reasoningSupport', 'none');
        fixture.componentRef.setInput('activeMode', { name: 'Deep work', reasoning_effort: 'high' });
        fixture.detectChanges();
        expect(trigger().textContent).not.toContain('set by');
        expect(trigger().classList).not.toContain('turn-settings__trigger--active');
    });

    it('falls back to generic wording for an unnamed mode', () => {
        fixture.componentRef.setInput('reasoningSupport', 'optional');
        fixture.componentRef.setInput('activeMode', { name: '  ', reasoning_effort: 'low' });
        fixture.detectChanges();
        expect(trigger().getAttribute('aria-label')).toContain('set by the active mode');
        expect(fixture.componentInstance.modeOverrideTooltip()).toContain('The active mode sets this');
        open();
        expect(host.querySelector('.turn-settings__mode-note')?.textContent).toContain('This mode sets Low');
    });
});
