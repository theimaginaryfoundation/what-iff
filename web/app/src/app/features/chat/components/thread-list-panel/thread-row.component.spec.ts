import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideRouter } from '@angular/router';

import { Chat } from '../../../../core/models/chat.model';
import { Personality } from '../../../../core/models/personality.model';
import { ContextPanelService } from '../../services/context-panel.service';
import { ThreadRowComponent } from './thread-row.component';

function makeChat(overrides: Partial<Chat> = {}): Chat {
    return {
        id: 'chat-id',
        user_id: 'user-1',
        name: 'Thread',
        created_at: '2026-04-01T00:00:00Z',
        updated_at: '2026-04-01T00:00:00Z',
        ...overrides,
    };
}

describe('ThreadRowComponent', () => {
    let fixture: ComponentFixture<ThreadRowComponent>;
    let component: ThreadRowComponent;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ThreadRowComponent],
            providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr())],
        }).compileComponents();

        fixture = TestBed.createComponent(ThreadRowComponent);
        component = fixture.componentInstance;
        fixture.componentRef.setInput('thread', makeChat({ id: 'a', name: 'Alpha' }));
        fixture.detectChanges();
    });

    it('emits select when row main button clicked', () => {
        const values: string[] = [];
        component.select.subscribe(id => values.push(id));

        const button = fixture.nativeElement.querySelector('.thread-row__main') as HTMLButtonElement;
        button.click();

        expect(values).toEqual(['a']);
    });

    it('emits select when non-interactive row area clicked', () => {
        const values: string[] = [];
        component.select.subscribe(id => values.push(id));

        const row = fixture.nativeElement.querySelector('.thread-row') as HTMLTableRowElement;
        row.dispatchEvent(new MouseEvent('click', { bubbles: true }));

        expect(values).toEqual(['a']);
    });

    it('renders enabled Archive and emits archiveThread when clicked', () => {
        const ids: string[] = [];
        component.archiveThread.subscribe(t => ids.push(t.id));

        const archiveButton = fixture.nativeElement.querySelector('.thread-row__archive') as HTMLButtonElement;
        expect(archiveButton.disabled).toBe(false);
        expect(archiveButton.getAttribute('aria-label')).toBe('Archive thread');
        archiveButton.click();
        expect(ids).toEqual(['a']);
    });

    it('renders Restore in archived view and emits restoreThread', () => {
        fixture.componentRef.setInput('isArchivedView', true);
        fixture.detectChanges();

        const ids: string[] = [];
        component.restoreThread.subscribe(t => ids.push(t.id));

        const restoreButton = fixture.nativeElement.querySelector('.thread-row__archive') as HTMLButtonElement;
        expect(restoreButton.textContent?.trim()).toBe('Restore');
        restoreButton.click();
        expect(ids).toEqual(['a']);
    });

    it('emits deleteThread when delete button is clicked', () => {
        const ids: string[] = [];
        component.deleteThread.subscribe(t => ids.push(t.id));

        const deleteButton = fixture.nativeElement.querySelector('.thread-row__delete') as HTMLButtonElement;
        expect(deleteButton.getAttribute('aria-label')).toBe('Delete thread Alpha');
        deleteButton.click();
        expect(ids).toEqual(['a']);
    });

    it('renders delete in archived view', () => {
        fixture.componentRef.setInput('isArchivedView', true);
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.thread-row__delete')).toBeTruthy();
    });

    it('emits rename on commit when name changed', () => {
        const emitted: string[] = [];
        component.rename.subscribe(payload => emitted.push(payload.name));
        component.editing.set(true);
        fixture.detectChanges();

        const input = fixture.nativeElement.querySelector('.thread-row__name-input') as HTMLInputElement;
        input.value = 'Renamed Thread';
        input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
        fixture.detectChanges();

        expect(emitted).toEqual(['Renamed Thread']);
    });

    it('renders a star toggle and emits togglePin when clicked', () => {
        const emitted: Chat[] = [];
        component.togglePin.subscribe(thread => emitted.push(thread));

        const star = fixture.nativeElement.querySelector('.thread-row__star') as HTMLButtonElement;
        expect(star).toBeTruthy();
        expect(star.getAttribute('aria-label')).toBe('Star thread');
        star.click();
        expect(emitted.length).toBe(1);
    });

    it('shows unread badge when unread_count is positive', () => {
        fixture.componentRef.setInput('thread', makeChat({ id: 'a', name: 'Alpha', unread_count: 4 }));
        fixture.detectChanges();

        const badge = fixture.nativeElement.querySelector('.thread-row__badge') as HTMLElement;
        expect(badge?.textContent?.trim()).toBe('4');
    });

    it('renders the unread badge as its own button outside the open-thread button', () => {
        fixture.componentRef.setInput('thread', makeChat({ id: 'a', name: 'Alpha', unread_count: 4 }));
        fixture.detectChanges();

        const badge = fixture.nativeElement.querySelector('.thread-row__badge') as HTMLButtonElement;
        expect(badge.tagName).toBe('BUTTON');
        expect(badge.closest('.thread-row__main')).toBeNull();
        expect(badge.getAttribute('aria-label')).toBe('4 unread replies, open thread');
    });

    it('opens the thread when the unread badge is clicked', () => {
        const selected: string[] = [];
        component.select.subscribe(id => selected.push(id));
        fixture.componentRef.setInput('thread', makeChat({ id: 'a', name: 'Alpha', unread_count: 4 }));
        fixture.detectChanges();

        (fixture.nativeElement.querySelector('.thread-row__badge') as HTMLButtonElement).click();

        expect(selected).toEqual(['a']);
    });

    it('renders an unchecked select checkbox and emits toggleSelect without navigating', () => {
        const toggled: string[] = [];
        const selected: string[] = [];
        component.toggleSelect.subscribe(id => toggled.push(id));
        component.select.subscribe(id => selected.push(id));

        const checkbox = fixture.nativeElement.querySelector('.thread-row__select') as HTMLInputElement;
        expect(checkbox.checked).toBe(false);
        checkbox.click();

        expect(toggled).toEqual(['a']);
        expect(selected).toEqual([]);
    });

    it('reflects the checked input on the select checkbox', () => {
        fixture.componentRef.setInput('checked', true);
        fixture.detectChanges();

        const checkbox = fixture.nativeElement.querySelector('.thread-row__select') as HTMLInputElement;
        expect(checkbox.checked).toBe(true);
    });

    it('renders personality avatar + accent from resolved personality input', () => {
        const personality: Personality = {
            id: 'p1',
            name: 'Aurex',
            system_prompt: 'prompt',
            auto_pin_memories: false,
            expressions_enabled: true,
            image_style: 'auto', cover_image_id: null,
            cover_image_url: null,
            accent_color: '#123456',
            thumbnail_circle: null,
            created_at: '2026-04-01T00:00:00Z',
            updated_at: '2026-04-01T00:00:00Z',
            stats: { chat_count: 0, last_used_at: null },
        };
        fixture.componentRef.setInput('personality', personality);
        fixture.detectChanges();

        const name = fixture.nativeElement.querySelector('.thread-row__personality-name') as HTMLElement;
        const avatarFallback = fixture.nativeElement.querySelector('.thread-row__personality-avatar-fallback') as HTMLElement;
        const personalityContent = fixture.nativeElement.querySelector('.thread-row__personality-content') as HTMLElement;

        expect(name.textContent?.trim()).toBe('Aurex');
        expect(avatarFallback.textContent?.trim()).toBe('A');
        expect(personalityContent.style.getPropertyValue('--thread-persona-accent').trim()).toBe('#123456');
    });

    it('toggles the thread as a composer context chip without opening it', () => {
        const selected: string[] = [];
        component.select.subscribe(id => selected.push(id));
        const contextPanel = TestBed.inject(ContextPanelService);
        const button = fixture.nativeElement.querySelector('.thread-row__context') as HTMLButtonElement;

        expect(button.getAttribute('aria-label')).toBe('Attach thread Alpha to your next message');
        expect(button.getAttribute('aria-pressed')).toBe('false');

        button.click();
        fixture.detectChanges();

        expect(contextPanel.composerThreadReferences().map(t => t.id)).toEqual(['a']);
        expect(button.getAttribute('aria-pressed')).toBe('true');
        expect(button.classList).toContain('thread-row__context--on');
        expect(selected).toEqual([]);

        button.click();
        fixture.detectChanges();

        expect(contextPanel.composerThreadReferences()).toEqual([]);
        expect(button.getAttribute('aria-pressed')).toBe('false');
    });
});

describe('ThreadRowComponent automations', () => {
    it('renders each automation in the job cell with its status, link and when-line', async () => {
        await TestBed.configureTestingModule({
            imports: [ThreadRowComponent],
            providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr()), provideRouter([])],
        }).compileComponents();
        const fixture = TestBed.createComponent(ThreadRowComponent);
        fixture.componentRef.setInput('thread', makeChat({ id: 'a', name: 'Alpha' }));
        fixture.componentRef.setInput('showJobColumn', true);
        fixture.componentRef.setInput('automations', [
            { chatId: 'a', name: 'Relay · #general', statusText: 'Live', tone: 'success', link: ['/integrations'], when: 'Last post 5m ago' },
            { chatId: 'a', name: 'Relay · #ops', statusText: 'Broken', tone: 'danger', link: ['/integrations'] },
        ]);
        fixture.detectChanges();

        const cell = fixture.nativeElement.querySelector('.thread-row__job') as HTMLElement;
        const names = Array.from(cell.querySelectorAll('.thread-row__job-name')).map(a => a.textContent?.trim());
        expect(names).toEqual(['Relay · #general', 'Relay · #ops']);
        const tones = Array.from(cell.querySelectorAll('.thread-row__job-status')).map(s => s.getAttribute('data-tone'));
        expect(tones).toEqual(['success', 'danger']);
        expect(cell.querySelector('.thread-row__job-name')!.getAttribute('href')).toBe('/integrations');
        expect(cell.querySelectorAll('.thread-row__job-when').length).toBe(1);
    });
});
