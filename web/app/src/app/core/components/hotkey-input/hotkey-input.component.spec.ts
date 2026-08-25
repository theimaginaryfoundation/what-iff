import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { HotkeyInputComponent } from './hotkey-input.component';

describe('HotkeyInputComponent', () => {
    let fixture: ComponentFixture<HotkeyInputComponent>;
    let component: HotkeyInputComponent;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [HotkeyInputComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(HotkeyInputComponent);
        component = fixture.componentInstance;
    });

    it('creates', () => {
        expect(component).toBeTruthy();
    });

    it('renders the empty, focused, and help branches', () => {
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.textContent).toContain('Click to set hotkey combination');
        expect(host.textContent).toContain('Must include Ctrl, Alt, or Cmd/Win');
        expect(host.querySelectorAll('span.inline-flex').length).toBe(0);

        component.onFocus();
        fixture.detectChanges();

        expect(host.textContent).toContain('Press keys to set hotkey combination...');
    });

    it('renders every parsed key and the enabled clear action', () => {
        component.writeValue('Ctrl+Shift+KeyG');
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(Array.from(host.querySelectorAll('span.inline-flex')).map(chip => chip.textContent?.trim())).toEqual([
            'Ctrl',
            'Shift',
            'G',
        ]);
        expect(host.querySelector('button')).not.toBeNull();

        component.clearHotkeys();
        fixture.detectChanges();
        expect(host.textContent).toContain('Click to set hotkey combination');
        expect(host.textContent).toContain('Hotkeys cleared');
    });

    it('hides help and clear controls when disabled', () => {
        component.writeValue('Alt+KeyR');
        component.setDisabledState(true);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.querySelectorAll('span.inline-flex').length).toBe(2);
        expect(host.querySelector('button')).toBeNull();
        expect(host.textContent).not.toContain('Valid examples');
    });

    it('renders error, success, and recording indicators', () => {
        const state = component as any;
        state.errorMessage.set('Invalid shortcut');
        state.successMessage.set('Shortcut saved');
        state.isRecording.set(true);
        fixture.detectChanges();

        expect(fixture.nativeElement.textContent).toContain('Invalid shortcut');
        expect(fixture.nativeElement.textContent).toContain('Shortcut saved');
        expect(fixture.nativeElement.textContent).toContain('Recording key combination...');
    });
});
