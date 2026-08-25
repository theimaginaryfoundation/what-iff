import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { Chat } from '../../../../core/models/chat.model';
import { ChatImportPickerComponent } from './chat-import-picker.component';

describe('ChatImportPickerComponent', () => {
    let fixture: ComponentFixture<ChatImportPickerComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ChatImportPickerComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(ChatImportPickerComponent);
        fixture.componentRef.setInput('candidates', []);
    });

    it('creates', () => {
        expect(fixture.componentInstance).toBeTruthy();
    });

    it('renders loading and empty branches', () => {
        fixture.componentRef.setInput('loading', true);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.import__progress')).not.toBeNull();
        expect(fixture.nativeElement.textContent).toContain('Loading your imported threads');

        fixture.componentRef.setInput('loading', false);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.import__picker-empty')).not.toBeNull();
        expect(fixture.nativeElement.textContent).toContain('No imported threads');
    });

    it('renders candidates, marks only the first three recent, and enforces the selection limit', () => {
        const candidates = [chat('1'), chat('2'), chat('3'), chat('4')];
        fixture.componentRef.setInput('candidates', candidates);
        fixture.componentRef.setInput('selectedIds', new Set(['1', '2']));
        fixture.componentRef.setInput('maxSelect', 2);
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;
        const checkboxes = host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]');

        expect(host.querySelectorAll('.import__picker-row').length).toBe(4);
        expect(host.querySelectorAll('.import__picker-badge').length).toBe(3);
        expect(host.textContent).toContain('2 / 2 selected');
        expect(checkboxes[0].checked).toBe(true);
        expect(checkboxes[0].disabled).toBe(false);
        expect(checkboxes[2].disabled).toBe(true);
    });

    it('emits the toggled chat id', () => {
        fixture.componentRef.setInput('candidates', [chat('1')]);
        let toggled = '';
        fixture.componentInstance.toggle.subscribe(id => (toggled = id));
        fixture.detectChanges();

        fixture.nativeElement.querySelector('input').dispatchEvent(new Event('change'));
        expect(toggled).toBe('1');
    });

    function chat(id: string): Chat {
        return {
            id,
            user_id: 'user-1',
            name: `Imported ${id}`,
            created_at: '2026-08-01T12:00:00Z',
            updated_at: '2026-08-01T12:00:00Z',
        };
    }
});
