import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { ThreadRefChipsComponent } from './thread-ref-chips.component';

describe('ThreadRefChipsComponent', () => {
    let fixture: ComponentFixture<ThreadRefChipsComponent>;
    const threads = (names: string[]) => names.map(name => ({
        id: `t-${name}`, user_id: 'u-1', name: `Thread ${name}`, created_at: '', updated_at: '',
    }));

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ThreadRefChipsComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();
        fixture = TestBed.createComponent(ThreadRefChipsComponent);
    });

    it('renders chips with overflow counts in a labelled group and emits remove/clear', () => {
        fixture.componentRef.setInput('threads', threads(['A', 'B', 'C', 'D', 'E', 'F', 'G']));
        fixture.detectChanges();
        const root: HTMLElement = fixture.nativeElement;
        const group = root.querySelector('.composer__thread-refs');

        expect(group?.getAttribute('role')).toBe('group');
        expect(group?.getAttribute('aria-label')).toBe('Threads attached to this message');
        expect(root.querySelector('.composer__thread-refs-label')?.textContent).toContain('7 threads added');
        expect(root.querySelectorAll('.composer__thread-chip').length).toBe(7);
        expect(root.querySelectorAll('.composer__thread-chip--hide-mobile').length).toBe(4);
        expect(root.querySelectorAll('.composer__thread-chip--hide-desktop').length).toBe(2);
        expect(root.querySelector('.composer__thread-more--mobile')?.textContent).toContain('+4');
        expect(root.querySelector('.composer__thread-more--desktop')?.textContent).toContain('+2');

        const removed = vi.fn();
        const cleared = vi.fn();
        fixture.componentInstance.removed.subscribe(removed);
        fixture.componentInstance.cleared.subscribe(cleared);
        (root.querySelector('.composer__thread-chip-remove') as HTMLButtonElement).click();
        (root.querySelector('.composer__thread-refs-clear') as HTMLButtonElement).click();

        expect(removed).toHaveBeenCalledWith('t-A');
        expect(cleared).toHaveBeenCalled();
    });

    it('uses a singular label and renders nothing when empty', () => {
        fixture.componentRef.setInput('threads', threads(['A']));
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.composer__thread-refs-label')?.textContent).toContain('1 thread added');

        fixture.componentRef.setInput('threads', []);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.composer__thread-refs')).toBeNull();
    });
});
