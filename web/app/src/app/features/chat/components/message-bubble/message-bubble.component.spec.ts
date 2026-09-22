import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideMarkdown } from 'ngx-markdown';

import { MessageBubbleComponent } from './message-bubble.component';
import { ChatMessage } from '../../../../core/models/message.model';
import { CHAT_PENDING_ASSISTANT_MESSAGE_ID } from '../../chat.constants';

describe('MessageBubbleComponent', () => {
    let fixture: ComponentFixture<MessageBubbleComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [MessageBubbleComponent],
            providers: [provideZonelessChangeDetection(), provideMarkdown()],
        }).compileComponents();

        fixture = TestBed.createComponent(MessageBubbleComponent);
        fixture.componentRef.setInput('message', message('User'));
        fixture.componentRef.setInput('displayContent', 'Hello');
        fixture.detectChanges();
    });

    it('shows model/mood hint for assistant messages when present', () => {
        fixture.componentRef.setInput('message', {
            ...message('Assistant'),
            generation_model: 'claude-sonnet',
            generation_mood_name: 'Focus',
        });
        fixture.componentRef.setInput('displayContent', 'Hi');
        fixture.detectChanges();

        const hint = fixture.nativeElement.querySelector('.bubble__hint');
        expect(hint?.textContent?.trim()).toBe('[claude-sonnet:Focus]');
        expect(fixture.nativeElement.querySelector('.bubble__meta')?.classList).toContain('bubble__meta--assistant');
    });

    it('labels the speaker and emits copy events', () => {
        const spy = vi.fn().mockName('copy');
        fixture.componentInstance.copy.subscribe(spy);

        const copyBtn = fixture.nativeElement.querySelector('.bubble__copy') as HTMLButtonElement;
        copyBtn.click();

        expect(fixture.nativeElement.querySelector('article').getAttribute('aria-label')).toBe('User message');
        expect(fixture.nativeElement.querySelector('article').classList).toContain('bubble--user');
        expect(spy).toHaveBeenCalled();
    });

    it('shows a Context button only for assistant messages with a breakdown, and emits it', () => {
        // No breakdown → no button.
        fixture.componentRef.setInput('message', { ...message('Assistant') });
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__context')).toBeNull();

        fixture.componentRef.setInput('message', {
            ...message('Assistant'),
            context_breakdown: {
                segments: [{ kind: 'history_turn', segments: 1, tokens: 100, cacheable: false }],
                total_tokens: 100,
                budget_tokens: 30000,
                captured_at: '2026-08-17T12:00:00Z',
            },
        });
        fixture.detectChanges();

        const spy = vi.fn().mockName('showContext');
        fixture.componentInstance.showContext.subscribe(spy);
        const btn = fixture.nativeElement.querySelector('.bubble__context') as HTMLButtonElement;
        expect(btn).toBeTruthy();
        btn.click();
        expect(spy).toHaveBeenCalled();
    });

    it('places Context before the model and personality hint', () => {
        fixture.componentRef.setInput('message', {
            ...message('Assistant'),
            generation_model: 'claude-sonnet',
            generation_mood_name: 'Focus',
            context_breakdown: {
                segments: [{ kind: 'history_turn', segments: 1, tokens: 100, cacheable: false }],
                total_tokens: 100,
                budget_tokens: 30000,
                captured_at: '2026-08-17T12:00:00Z',
            },
        });
        fixture.detectChanges();

        const labels = Array.from(fixture.nativeElement.querySelector('.bubble__meta-end')?.children ?? [], (element: Element) => element.textContent?.trim());
        expect(labels).toEqual(['Context', '[claude-sonnet:Focus]']);
    });

    it('shows skill name pills for user messages with rituals', () => {
        fixture.componentRef.setInput('message', {
            ...message('User'),
            rituals: [
                { id: 'r1', name: 'Morning brief', description: '', content: '', hotkeys: '', personality_id: null, created_at: '', updated_at: '' },
            ],
        });
        fixture.componentRef.setInput('displayContent', 'Hi');
        fixture.detectChanges();

        const pills = fixture.nativeElement.querySelectorAll('.bubble__skill-pill');
        expect(pills.length).toBe(1);
        expect(pills[0].textContent?.trim()).toBe('Morning brief');
    });

    it('renders typing dots for the pending assistant placeholder without copy chrome', () => {
        fixture.componentRef.setInput('message', pendingAssistantMessage());
        fixture.componentRef.setInput('displayContent', '');
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('article').getAttribute('aria-label')).toBe('Assistant is composing a reply');
        expect(fixture.nativeElement.querySelector('.bubble__pending-dots')).toBeTruthy();
        expect(fixture.nativeElement.querySelector('.bubble__copy')).toBeNull();
    });

    it('renders streamed placeholder content once available', () => {
        fixture.componentRef.setInput('message', pendingAssistantMessage());
        fixture.componentRef.setInput('displayContent', 'Partial streamed text');
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.bubble__pending-dots')).toBeNull();
        expect(fixture.componentInstance.showPendingDots()).toBe(false);
        expect(fixture.nativeElement.querySelector('app-message-content')).toBeTruthy();
    });
});

describe('MessageBubbleComponent branching', () => {
    let fixture: ComponentFixture<MessageBubbleComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [MessageBubbleComponent],
            providers: [provideZonelessChangeDetection(), provideMarkdown()],
        }).compileComponents();
        fixture = TestBed.createComponent(MessageBubbleComponent);
        fixture.componentRef.setInput('displayContent', 'Hello');
    });

    it('offers "What if…" on user messages and "Branch" on replies, emitting the message', () => {
        const spy = vi.fn();
        fixture.componentInstance.branch.subscribe(spy);

        fixture.componentRef.setInput('message', message('User'));
        fixture.detectChanges();
        const userBtn = fixture.nativeElement.querySelector('.bubble__branch') as HTMLButtonElement;
        expect(userBtn.textContent).toContain('What if…');
        userBtn.click();
        expect(spy).toHaveBeenCalledWith(expect.objectContaining({ id: 'm1', origin: 'User' }));

        fixture.componentRef.setInput('message', message('Assistant'));
        fixture.detectChanges();
        expect((fixture.nativeElement.querySelector('.bubble__branch') as HTMLElement).textContent).toContain('Branch');
    });

    it('hides the branch action when disabled or for optimistic/placeholder rows', () => {
        fixture.componentRef.setInput('message', message('User'));
        fixture.componentRef.setInput('branchingEnabled', false);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__branch')).toBeNull();

        fixture.componentRef.setInput('branchingEnabled', true);
        fixture.componentRef.setInput('message', { ...message('User'), id: 'temp-123' });
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__branch')).toBeNull();

        fixture.componentRef.setInput('message', { ...message('User'), id: 'error-9' });
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__branch')).toBeNull();

        fixture.componentRef.setInput('message', pendingAssistantMessage());
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__branch')).toBeNull();
    });

    it('shows a branch marker listing branches and opens one', () => {
        const spy = vi.fn();
        fixture.componentInstance.openBranch.subscribe(spy);
        fixture.componentRef.setInput('message', message('Assistant'));
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__branches')).toBeNull();

        const branches = [
            { id: 'b1', name: 'What if: Lisbon', forked_from_message_id: 'm1', created_at: '2026-01-02T00:00:00Z' },
            { id: 'b2', name: 'What if: Porto', forked_from_message_id: 'm1', created_at: '2026-01-01T00:00:00Z' },
        ];
        fixture.componentRef.setInput('branches', branches);
        fixture.detectChanges();

        const marker = fixture.nativeElement.querySelector('.bubble__branches') as HTMLElement;
        expect(marker.querySelector('summary')?.textContent).toContain('2 branches');
        const items = marker.querySelectorAll('.bubble__branches-item');
        expect(items.length).toBe(2);
        (items[1] as HTMLButtonElement).click();
        expect(spy).toHaveBeenCalledWith(branches[1]);

        fixture.componentRef.setInput('branches', [branches[0]]);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__branches summary')?.textContent).toContain('1 branch');
    });
});

function message(origin: ChatMessage['origin']): ChatMessage {
    return {
        id: 'm1',
        chat_id: 'c1',
        message: 'Hello',
        origin,
        sent_at: '2024-01-01T00:00:00Z',
    };
}

function pendingAssistantMessage(): ChatMessage {
    return {
        id: CHAT_PENDING_ASSISTANT_MESSAGE_ID,
        chat_id: 'c1',
        message: '',
        origin: 'Assistant',
        sent_at: '1970-01-01T00:00:00.000Z',
        generation_personality: 'Kai',
        generation_expression_key: 'thinking',
    };
}
