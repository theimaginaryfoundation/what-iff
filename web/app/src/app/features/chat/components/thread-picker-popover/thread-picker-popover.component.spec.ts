import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';

import { Chat } from '../../../../core/models/chat.model';
import { THREAD_PICKER_ROW_LIMIT, ThreadPickerPopoverComponent } from './thread-picker-popover.component';

function chat(id: string, name: string, overrides: Partial<Chat> = {}): Chat {
    return { id, user_id: 'u', name, created_at: '', updated_at: '', ...overrides };
}

describe('ThreadPickerPopoverComponent', () => {
    let fixture: ComponentFixture<ThreadPickerPopoverComponent>;
    const options = [
        chat('active', 'Active chat'),
        chat('t-1', 'Travel Planning', { is_favorite: true }),
        chat('t-2', 'Cooking'),
        chat('t-3', 'Old one', { archived: true }),
    ];
    const root = () => fixture.nativeElement as HTMLElement;
    const names = () => [...root().querySelectorAll('.composer__thread-name')].map(n => n.textContent?.trim());
    const rows = () => [...root().querySelectorAll('.composer__thread-row')] as HTMLButtonElement[];

    async function create(inputs: Record<string, unknown> = {}) {
        await TestBed.configureTestingModule({
            imports: [ThreadPickerPopoverComponent],
            providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr())],
        }).compileComponents();
        fixture = TestBed.createComponent(ThreadPickerPopoverComponent);
        fixture.componentRef.setInput('threads', options);
        fixture.componentRef.setInput('activeChatId', 'active');
        for (const [key, value] of Object.entries(inputs)) {
            fixture.componentRef.setInput(key, value);
        }
        fixture.autoDetectChanges();
        await fixture.whenStable();
    }

    it('lists other non-archived threads and filters by search and starred', async () => {
        await create();
        expect(names()).toEqual(['Travel Planning', 'Cooking']);

        (root().querySelector('.composer__thread-starred-filter') as HTMLButtonElement).click();
        await fixture.whenStable();
        expect(names()).toEqual(['Travel Planning']);

        (root().querySelector('.composer__thread-starred-filter') as HTMLButtonElement).click();
        const search = root().querySelector('.composer__thread-search') as HTMLInputElement;
        search.value = 'cook';
        search.dispatchEvent(new Event('input'));
        await fixture.whenStable();
        expect(names()).toEqual(['Cooking']);
    });

    it('focuses the search field on open', async () => {
        await create();
        expect(document.activeElement).toBe(root().querySelector('.composer__thread-search'));
    });

    it('uses toggle buttons with aria-pressed instead of listbox semantics', async () => {
        await create({ selected: [options[2]] });

        expect(root().querySelector('[role="listbox"]')).toBeNull();
        expect(root().querySelector('[role="option"]')).toBeNull();
        expect(rows().map(r => r.getAttribute('aria-pressed'))).toEqual(['false', 'true']);
        expect(root().querySelector('.composer__thread-count')?.textContent).toContain('1 selected');
    });

    it('renders personality avatar, age and a star for starred threads', async () => {
        await create({
            personalities: [{ id: 'p-1', name: 'Lola Tarsier', accent_color: '#10B981' }],
            threads: [
                chat('t-1', 'Travel Planning', {
                    is_favorite: true, personality_id: 'p-1',
                    last_message_time: new Date(Date.now() - 3 * 24 * 3_600_000).toISOString(),
                }),
                chat('t-2', 'Cooking', { personality_name: 'Mooey' }),
            ],
        });
        const [first, second] = rows();

        expect(first.querySelector('.composer__thread-avatar')?.textContent?.trim()).toBe('LT');
        expect(first.querySelector('.composer__thread-age')?.textContent).toContain('3 days ago');
        expect(first.querySelector('.composer__thread-star')).not.toBeNull();
        expect(second.querySelector('.composer__thread-avatar')?.textContent?.trim()).toBe('MO');
        expect(second.querySelector('.composer__thread-star')).toBeNull();
    });

    it('emits toggled for a row, and never when disabled', async () => {
        await create();
        const toggled = vi.fn();
        fixture.componentInstance.toggled.subscribe(toggled);

        rows()[0].click();
        expect(toggled).toHaveBeenCalledWith(options[1]);

        fixture.componentRef.setInput('disabled', true);
        await fixture.whenStable();
        fixture.componentInstance.pick(options[2]);
        expect(toggled).toHaveBeenCalledTimes(1);
    });

    it('keeps Done enabled with nothing selected', async () => {
        await create();
        const done = vi.fn();
        fixture.componentInstance.done.subscribe(done);
        const button = root().querySelector('.composer__thread-done') as HTMLButtonElement;

        expect(button.disabled).toBe(false);
        button.click();
        expect(done).toHaveBeenCalled();
    });

    it('Cancel and Escape emit the selection snapshot from when it opened', async () => {
        await create({ selected: [options[2]] });
        const cancelled = vi.fn();
        fixture.componentInstance.cancelled.subscribe(cancelled);

        fixture.componentRef.setInput('selected', [options[2], options[1]]);
        await fixture.whenStable();
        (root().querySelector('.composer__thread-cancel') as HTMLButtonElement).click();
        expect(cancelled).toHaveBeenLastCalledWith([options[2]]);

        root().querySelector('.composer__thread-search')!
            .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
        expect(cancelled).toHaveBeenCalledTimes(2);
        expect(cancelled).toHaveBeenLastCalledWith([options[2]]);
    });

    it('caps rendered rows to the most recent threads and hints that search finds more', async () => {
        const many = Array.from({ length: THREAD_PICKER_ROW_LIMIT + 10 }, (_, i) =>
            chat(`t-${i}`, `Thread ${i}`, { updated_at: new Date(Date.UTC(2026, 0, 1, 0, i)).toISOString() }),
        );
        await create({ threads: many });

        expect(rows()).toHaveLength(THREAD_PICKER_ROW_LIMIT);
        expect(names()[0]).toBe(`Thread ${THREAD_PICKER_ROW_LIMIT + 9}`);
        expect(root().querySelector('.composer__thread-limit')?.textContent)
            .toContain(`Showing ${THREAD_PICKER_ROW_LIMIT} of ${THREAD_PICKER_ROW_LIMIT + 10}`);

        const search = root().querySelector('.composer__thread-search') as HTMLInputElement;
        search.value = 'Thread 3';
        search.dispatchEvent(new Event('input'));
        await fixture.whenStable();

        expect(names()).toContain('Thread 3');
        expect(root().querySelector('.composer__thread-limit')).toBeNull();
    });
});
